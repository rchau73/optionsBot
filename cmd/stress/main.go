// Command stress replays the live book through a scripted market shock and
// reports, day by day, what the bot's rules would do. It starts from the
// bot's own snapshot (positions, account, DVOL history and daily closes —
// see scripts/stress_inputs.sh), optionally runs calm days with DVOL sliding
// to its lows (-calm), then a two-week crash or rally (-shock), or normal
// random paths. Decisions use the bot's real pure rules (EvaluateLeg with the
// delta exit, ResolveGammaAction with the flip buffer, risk.Evaluate,
// RepairBlockReason, SelectStrike, MMKeepQty, Slot/EntryShare); prices,
// margin, volume and fills are modelled (Black–Scholes on a DVOL smile, fees
// 0.03 % per contract) — lines marked MODEL. Research tooling: it never
// connects to Deribit and places no orders. Flags default to today's bot;
// -condor and its sub-flags test the squeeze-protection condor proposal that
// was rejected (kept for re-tests). See README "Stress test".
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/config"
	"optionsbot/internal/history"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/risk"
	"optionsbot/internal/strategy"
)

const (
	rolloutDTE                     = 15
	drift, roiTP, stopMult, minPre = 0.10, 0.50, 2.0, 0.001
	feePerBTC                      = 0.0003 // Deribit: 0.03 % of the underlying per contract
	cooldown                       = 72 * time.Hour
	// Proposed condor rules (squeeze-protection research request).
	squeezeDays    = 10   // consecutive low-band DVOL closes
	wingDelta      = 0.05 // wing at least this far out (|Δ| ≤ 0.05) …
	wingBudgetFrac = 0.30 // … and all wings ≤ 30 % of the credit still to earn
)

// underlying is the coin of the simulated book (-currency); lot is its
// minimum trade (Deribit: 0.1 BTC, 1 ETH).
var (
	underlying = "BTC"
	lot        = 0.1
)

// strikeStep is a Deribit-like strike spacing near spot: about 1 % of it,
// rounded to 1, 2.5 or 5 × a power of ten (BTC ~85k → 1,000; ETH ~2.7k → 25).
func strikeStep(spot float64) float64 {
	raw := spot / 100
	pow := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2.5, 5, 10} {
		if raw <= m*pow*1.5 {
			return m * pow
		}
	}
	return 10 * pow
}

type slot struct {
	dte   int
	delta float64
}

var slots = []slot{{25, 0.16}, {45, 0.16}, {60, 0.18}}

type leg = orders.Position

type strangle struct {
	slot              slot
	call, put         *leg
	callWing, putWing *leg
	stopped           map[string]time.Time
	shed              map[string]time.Time // GEX-shed legs: when
}

func markShed(s *strangle, typ string, at time.Time) {
	if s.shed == nil {
		s.shed = map[string]time.Time{}
	}
	s.shed[typ] = at
}

// botTrendOf is the bot's SMA rule (detectTrend without swing breakouts).
func botTrendOf(closes []float64) int {
	n := len(closes)
	if n < 21 {
		return 0
	}
	sma := func(k int) float64 {
		t := 0.0
		for _, c := range closes[n-k:] {
			t += c
		}
		return t / float64(k)
	}
	s9, s21, c := sma(9), sma(21), closes[n-1]
	switch {
	case s9 > s21 && c > s9:
		return 1
	case s9 < s21 && c < s9:
		return -1
	}
	return 0
}

type day struct {
	ret, dvol float64
	negative  bool
	volRatio  float64 // MODEL: 7-day / 20-day volume (BTC perp + options)
}

// MODEL: 14 calm days — tiny moves, DVOL 36 → 27, volume drying up.
var squeeze = []day{
	{+0.4, 34, false, 1.00}, {-0.3, 33, false, 0.97}, {+0.2, 33, false, 0.93}, {-0.5, 32, false, 0.90}, {+0.3, 31, false, 0.86},
	{+0.1, 31, false, 0.83}, {-0.2, 30, false, 0.80}, {+0.4, 30, false, 0.78}, {-0.1, 29, false, 0.76}, {+0.2, 29, false, 0.75},
	{-0.3, 28, false, 0.74}, {+0.1, 28, false, 0.73}, {0.0, 28, false, 0.72}, {+0.2, 27, false, 0.72},
}

// MODEL: the same two-week shocks as the earlier stress test (volume surges).
var shocks = map[string][]day{
	// MODEL: the squeeze signal was a false alarm — two more quiet weeks.
	"quiet": {
		{+0.5, 27, false, 0.72}, {-0.6, 27, false, 0.73}, {+0.3, 26, false, 0.74}, {-0.4, 26, false, 0.75}, {+0.8, 27, false, 0.78},
		{-0.5, 27, false, 0.80}, {+0.2, 26, false, 0.80}, {-0.7, 27, false, 0.82}, {+0.6, 28, false, 0.85}, {-0.3, 28, false, 0.86},
		{+0.4, 27, false, 0.88}, {-0.2, 27, false, 0.90}, {+0.5, 28, false, 0.92}, {-0.4, 28, false, 0.94},
	},
	"crash40": {
		{-8, 55, true, 2.5}, {-12, 80, true, 3.5}, {-10, 95, true, 3.8}, {-9, 105, true, 3.5}, {-10, 110, true, 3.2},
		{+5, 100, true, 2.6}, {-3, 95, true, 2.2}, {+2, 88, true, 1.9}, {+1, 82, true, 1.7}, {-2, 78, true, 1.6},
		{+3, 74, false, 1.4}, {0, 70, false, 1.3}, {+1, 67, false, 1.2}, {-1, 64, false, 1.1},
	},
	"rally40": {
		{+7, 45, false, 2.2}, {+9, 60, true, 3.0}, {+8, 70, true, 3.2}, {+7, 75, true, 3.0}, {+5, 78, true, 2.8},
		{-4, 74, true, 2.3}, {+2, 70, true, 2.0}, {-2, 66, true, 1.8}, {+1, 63, false, 1.6}, {+1, 60, false, 1.4},
		{-1, 58, false, 1.3}, {+2, 56, false, 1.2}, {0, 55, false, 1.1}, {+1, 54, false, 1.1},
	},
	// MODEL: two weeks of violent back-and-forth, no trend — where a
	// defensive roll can lose by rolling right before a reversal.
	"chop": {
		{+5, 45, false, 1.5}, {-5, 50, false, 1.6}, {+6, 52, true, 1.7}, {-6, 55, true, 1.8}, {+4, 52, false, 1.5},
		{-4, 50, false, 1.4}, {+5, 52, true, 1.6}, {-5, 53, true, 1.6}, {+3, 50, false, 1.4}, {-3, 48, false, 1.3},
		{+4, 48, false, 1.3}, {-4, 47, false, 1.2}, {+2, 45, false, 1.1}, {-2, 44, false, 1.1},
	},
	"crash": {
		{-4, 44, true, 1.6}, {-6, 56, true, 2.2}, {-3, 60, true, 2.3}, {+2, 57, true, 2.0}, {-5, 64, true, 2.1}, {-4, 70, true, 2.0}, {-2, 72, true, 1.8},
		{+3, 66, true, 1.6}, {-3, 69, true, 1.6}, {-4, 74, true, 1.7}, {-1, 72, true, 1.5}, {+2, 68, true, 1.4}, {-2, 65, false, 1.3}, {-1, 63, false, 1.2},
	},
	"rally": {
		{+4, 40, false, 1.5}, {+6, 48, true, 2.0}, {+3, 52, true, 2.1}, {-2, 50, true, 1.8}, {+5, 55, true, 1.9}, {+4, 60, true, 1.9}, {+2, 61, true, 1.7},
		{-3, 57, true, 1.5}, {+3, 59, true, 1.5}, {+4, 63, true, 1.6}, {+1, 62, false, 1.4}, {-2, 58, false, 1.3}, {+2, 56, false, 1.2}, {+1, 54, false, 1.1},
	},
}

func normCDF(x float64) float64 { return 0.5 * math.Erfc(-x/math.Sqrt2) }

// MODEL: Black-Scholes on spot, inverse price in coin, IV = DVOL with a skew
// calibrated to today's chain.
func iv(k, s, dvol float64) float64 {
	base := dvol / 100
	if k < s {
		return base * (1 + 0.8*math.Log(s/k))
	}
	return base * (1 + 0.37*math.Log(k/s))
}

func price(typ string, k, s, t, sig float64) (float64, float64) {
	if t <= 0 {
		if typ == "call" {
			return math.Max(s-k, 0) / s, 0
		}
		return math.Max(k-s, 0) / s, 0
	}
	sq := sig * math.Sqrt(t)
	d1 := (math.Log(s/k) + 0.5*sig*sig*t) / sq
	d2 := d1 - sq
	if typ == "call" {
		return (s*normCDF(d1) - k*normCDF(d2)) / s, normCDF(d1)
	}
	return (k*normCDF(-d2) - s*normCDF(-d1)) / s, normCDF(d1) - 1
}

func years(exp, now time.Time) float64 { return exp.Sub(now).Hours() / 24 / 365 }

type market struct {
	now        time.Time
	spot, dvol float64
}

func (m market) mark(l *leg) (float64, float64) {
	return price(l.OptionType, l.Strike, m.spot, years(l.Expiry, m.now), iv(l.Strike, m.spot, m.dvol))
}

// MODEL: half-spread paid when trading at market, widening with DVOL.
func halfSpread(dvol float64) float64 {
	switch {
	case dvol < 50:
		return 0.04
	case dvol < 65:
		return 0.08
	}
	return 0.12
}

func sign(l *leg) float64 { // value of the book: shorts owe, longs own
	if l.Side == orders.DirectionBuy {
		return 1
	}
	return -1
}

// MODEL: portfolio margin ≈ worst loss over spot ±16 % × vol −20 %/+30 %
// shocks, scaled so day 0 equals the real account IM; MM = IM × real ratio.
func rawIM(legs []*leg, m market) float64 {
	worst := 0.0
	for _, sm := range []float64{0.84, 0.88, 0.92, 0.96, 1, 1.04, 1.08, 1.12, 1.16} {
		for _, vm := range []float64{0.8, 1, 1.3} {
			s2, loss := m.spot*sm, 0.0
			for _, l := range legs {
				p0, _ := m.mark(l)
				p1, _ := price(l.OptionType, l.Strike, s2, years(l.Expiry, m.now), iv(l.Strike, s2, m.dvol*vm))
				loss -= sign(l) * l.Qty * (p1 - p0)
			}
			worst = math.Max(worst, loss)
		}
	}
	return worst
}

type book struct {
	strangles                []*strangle
	mb0, u0                  float64
	realised, fees, wingCost float64
	imScale, mmRatio         float64
	appliedLimit             float64
	stops, gex, rolls, wings int
}

func (st *strangle) all() []*leg {
	var out []*leg
	for _, l := range []*leg{st.call, st.put, st.callWing, st.putWing} {
		if l != nil && l.Qty > 1e-9 {
			out = append(out, l)
		}
	}
	return out
}

func (b *book) legs() []*leg {
	var out []*leg
	for _, st := range b.strangles {
		out = append(out, st.all()...)
	}
	return out
}

// unrealised: shorts = premium − value; longs = value − cost (PremiumReceived
// holds the cost paid for a long, as a positive number).
func unrealised(legs []*leg) float64 {
	u := 0.0
	for _, l := range legs {
		if l.Side == orders.DirectionBuy {
			u += l.CurrentMid*l.Qty - l.PremiumReceived
		} else {
			u += l.PremiumReceived - l.CurrentMid*l.Qty
		}
	}
	return u
}

func (b *book) marginBalance() float64 { return b.mb0 - b.u0 + b.realised + unrealised(b.legs()) }

func (b *book) usage(m market) risk.Usage {
	im := rawIM(b.legs(), m) * b.imScale
	return risk.Usage{IM: im, MM: im * b.mmRatio, MarginBalance: b.marginBalance(), Unit: underlying}
}

// trade closes qty of l at market: buy back a short at the ask, sell a long at the bid.
func (b *book) trade(l *leg, qty float64, m market) float64 {
	qty = math.Min(qty, l.Qty)
	mid, _ := m.mark(l)
	h := halfSpread(m.dvol)
	fee := feePerBTC * qty
	basis := l.PremiumReceived * qty / l.Qty
	var pnl float64
	if l.Side == orders.DirectionBuy {
		pnl = mid*(1-h)*qty - basis - fee
	} else {
		pnl = basis - mid*(1+h)*qty - fee
	}
	b.realised += pnl
	b.fees += fee
	l.PremiumReceived -= basis
	l.Qty -= qty
	return pnl
}

func (b *book) closeAll(l *leg, m market) float64 {
	if l == nil || l.Qty < 1e-9 {
		return 0
	}
	return b.trade(l, l.Qty, m)
}

func (b *book) prune() {
	var keep []*strangle
	for _, st := range b.strangles {
		for _, p := range []**leg{&st.call, &st.put, &st.callWing, &st.putWing} {
			if *p != nil && (*p).Qty < 1e-9 {
				*p = nil
			}
		}
		if st.call == nil && st.callWing != nil { // a wing alone protects nothing: sell it
			st.callWing = nil
		}
		if st.put == nil && st.putWing != nil {
			st.putWing = nil
		}
		if st.call != nil || st.put != nil {
			keep = append(keep, st)
		}
	}
	b.strangles = keep
}

func chain(m market) []*marketdata.Instrument {
	var out []*marketdata.Instrument
	first := m.now.Truncate(24 * time.Hour)
	for first.Weekday() != time.Friday {
		first = first.AddDate(0, 0, 1)
	}
	for w := 0; w < 56; w++ {
		exp := first.AddDate(0, 0, 7*w).Add(8 * time.Hour)
		if !exp.After(m.now) {
			continue
		}
		if realExpiries && !listed(exp, m.now) {
			continue
		}
		step := strikeStep(m.spot)
		for k := math.Round(m.spot*0.35/step) * step; k <= m.spot*2.8; k += step {
			for _, typ := range []string{"call", "put"} {
				p, d := price(typ, k, m.spot, years(exp, m.now), iv(k, m.spot, m.dvol))
				out = append(out, &marketdata.Instrument{
					Name:       fmt.Sprintf("%s-%s-%.0f-%s", underlying, strings.ToUpper(exp.Format("2Jan06")), k, strings.ToUpper(typ[:1])),
					Underlying: underlying, Strike: k, Expiry: exp, OptionType: typ, MinTradeAmount: lot,
					Mid: p, Bid: p * 0.96, Ask: p * 1.04, Greeks: marketdata.Greeks{Delta: d}, UpdatedAt: m.now,
				})
			}
		}
	}
	return out
}

var realExpiries bool

// listed: Deribit lists the next ~4 Fridays, the last Friday of the next ~4
// months, and the last Friday of each quarter-end month up to a year out.
func listed(exp, now time.Time) bool {
	days := exp.Sub(now).Hours() / 24
	lastFriday := exp.AddDate(0, 0, 7).Month() != exp.Month()
	switch {
	case days <= 29:
		return true
	case days <= 125:
		return lastFriday
	case days <= 370:
		return lastFriday && exp.Month()%3 == 0
	}
	return false
}

func newLeg(typ string, k float64, exp time.Time, qty float64, side string, m market) *leg {
	mid, d := price(typ, k, m.spot, years(exp, m.now), iv(k, m.spot, m.dvol))
	fill := mid // sells fill at mid (the bot quotes the ask; testnet rarely fills)
	if side == orders.DirectionBuy {
		fill = mid * (1 + halfSpread(m.dvol)) // wings are bought at the ask
	}
	return &leg{
		ID: fmt.Sprintf("%s-%.0f-%s", exp.Format("Jan02"), k, typ), Instrument: fmt.Sprintf("%.0f%s", k, typ), Underlying: underlying,
		Strike: k, Expiry: exp, OptionType: typ, Side: side, Qty: qty, EntryPrice: fill, EntryTime: m.now,
		PremiumReceived: fill * qty, CurrentMid: mid, CurrentGreeks: orders.Greeks{Delta: d}, MarkLive: true,
	}
}

func short(l *leg) string {
	tag := map[string]string{"call": "kC", "put": "kP"}[l.OptionType]
	if l.Side == orders.DirectionBuy {
		tag += "(long)"
	}
	return fmt.Sprintf("%s %.0f%s×%.1f", l.Expiry.Format("Jan02"), l.Strike/1000, tag, l.Qty)
}

// trend MODEL: SMA9 vs SMA21 with price on the same side.
func trend(closes []float64) int {
	n := len(closes)
	sma := func(k int) float64 {
		s := 0.0
		for _, c := range closes[n-k:] {
			s += c
		}
		return s / float64(k)
	}
	s9, s21, c := sma(9), sma(21), closes[n-1]
	switch {
	case s9 < s21 && c < s21:
		return -1
	case s9 > s21 && c > s21:
		return 1
	}
	return 0
}

// realisedVol is the annualised stdev of the last k daily log returns.
func realisedVol(closes []float64, k int) float64 {
	n := len(closes)
	var rs []float64
	for i := n - k; i < n; i++ {
		rs = append(rs, math.Log(closes[i]/closes[i-1]))
	}
	mean := 0.0
	for _, r := range rs {
		mean += r
	}
	mean /= float64(len(rs))
	v := 0.0
	for _, r := range rs {
		v += (r - mean) * (r - mean)
	}
	return math.Sqrt(v/float64(len(rs)-1)) * math.Sqrt(365)
}

// wingStrike: the first strike beyond the short by one expected move, and at
// |Δ| ≤ wingDelta — whichever is further out.
var wingDeltaMax, wingNeedEM = wingDelta, true

func wingStrike(short *leg, m market, insts []*marketdata.Instrument) (*marketdata.Instrument, bool) {
	em := m.spot * (m.dvol / 100) * math.Sqrt(years(short.Expiry, m.now))
	if !wingNeedEM {
		em = 1 // any strike beyond the short
	}
	var best *marketdata.Instrument
	for _, in := range insts {
		if !in.Expiry.Equal(short.Expiry) || in.OptionType != short.OptionType {
			continue
		}
		far := (short.OptionType == "call" && in.Strike >= short.Strike+em) || (short.OptionType == "put" && in.Strike <= short.Strike-em)
		if !far || math.Abs(in.Greeks.Delta) > wingDeltaMax {
			continue
		}
		if best == nil || math.Abs(in.Strike-short.Strike) < math.Abs(best.Strike-short.Strike) {
			best = in
		}
	}
	return best, best != nil && best.Mid >= 0.0001
}

func main() {
	name := flag.String("shock", "crash", "path after the calm days: crash | rally | crash40 | rally40 | chop | quiet | normalN | longN (N = random seed)")
	currency := flag.String("currency", "BTC", "coin of the book in -dir (BTC or ETH; use -volscale for ETH-like moves)")
	condor := flag.Bool("condor", false, "apply the PROPOSED squeeze-protection condor rules")
	letWings := flag.Bool("letwings", false, "with -condor: winged spreads keep their defined risk (no spread stop, no GEX shed) until the DTE roll")
	dir := flag.String("dir", ".", "folder with market.json, positions.json, account.json")
	quiet := flag.Bool("quiet", false, "summary only")
	calm := flag.Int("calm", 0, "calm days before the shock")
	capMult := flag.Float64("cap", 2, "max_leg_size_multiple (0 = no cap)")
	steps := flag.Int("steps", 6, "decisions per day (6 = every 4 hours)")
	breach := flag.Float64("breach", 0.30, "delta exit: close a short leg when its |delta| reaches this, as the bot (delta_exit_threshold; 0 = off)")
	vrpFlag := flag.Float64("vrp", 0.85, "normal scenarios: realised vol as a fraction of implied (DVOL)")
	breachHold := flag.Bool("breachhold", true, "treat a delta breach like a stop: exit early, re-sell only once calm (repair hold)")
	wingD := flag.Float64("wingdelta", 0.05, "with -condor: wing at |Δ| ≤ this (closest such strike)")
	wingEM := flag.Bool("wingem", true, "with -condor: wing also at least one expected move beyond the short (the original far wing)")
	always := flag.Bool("always", false, "with -condor: every entry/repair is a condor, not only while the squeeze trigger is on")
	matched := flag.Bool("matched", false, "with -condor: exits judged on the spread (TP on net credit, drift, stop at a fraction of max loss)")
	sizeAsStrangle := flag.Bool("sizeasstrangle", false, "with -condor: size a condor by its strangle's margin (same lots), so only the wings differ")
	spreadBreach := flag.Bool("spreadbreach", true, "with -matched: the delta exit also applies to winged spreads (false = the wing is the protection)")
	spreadStop := flag.Float64("spreadstop", 0.5, "with -matched: close the spread when its loss reaches this fraction of max loss (≥1 = never)")
	stickyFlip := flag.Bool("stickyflip", false, "MODEL: the flip follows spot with a lag (half-life ~4 days) and the regime is NEGATIVE whenever spot is below it")
	flipStart := flag.Float64("flipstart", 0.6, "with -stickyflip: the flip starts this % above spot")
	volScale := flag.Float64("volscale", 1, "MODEL: scale implied vol (DVOL, its history) and moves — 1 BTC-like, 1.4 ETH-like, 2.2 SOL-like")
	flipBufSD := flag.Float64("flipbufsd", 1, "shed only when spot is more than this many daily standard deviations (DVOL/√365) below the flip; overrides -flipbuf")
	flipBuf := flag.Float64("flipbuf", 0, "shed a leg only when spot is more than this % below the flip")
	shedHold := flag.Float64("shedhold", 0, "hours repair waits before re-selling a GEX-shed leg")
	botTrend := flag.Bool("bottrend", true, "trend as the bot: bull needs SMA9 > SMA21 and price > SMA9 (swing breakouts not modelled)")
	slotsFlag := flag.String("slots", "", "comma-separated slot DTEs (e.g. 45,60,90); empty = today's 25/45/60")
	slotDelta := flag.Float64("slotdelta", 0.16, "with -slots: entry delta of every slot")
	scaled := flag.Bool("scaled", false, "drift and delta-exit thresholds scale with each slot's entry delta (today's ratios: 0.10 and 0.30 at 0.16)")
	distinct := flag.String("distinct", "farther-wait", "expiry per slot: farther-wait (the bot: nearest free expiry up to 1.5× target, else wait) | farther-share | wait | off")
	flat := flag.Bool("flat", false, "start with no positions (same equity)")
	days := flag.Int("days", 28, "length of normal paths")
	warm := flag.Int("warm", 0, "ordinary days (seed 99) before the shock")
	realExp := flag.Bool("realexp", true, "Deribit's expiry calendar (weeklies ~4 weeks, month-ends ~4 months, quarter-ends ~1 year) instead of every Friday")
	flag.Parse()
	underlying = *currency
	if underlying != "BTC" {
		lot = 1
	}
	realExpiries = *realExp
	if *slotsFlag != "" {
		slots = nil
		for _, f := range strings.Split(*slotsFlag, ",") {
			d, dl := 0, *slotDelta
			if i := strings.Index(f, ":"); i > 0 { // "45:0.20" = DTE 45 at delta 0.20
				fmt.Sscanf(f[i+1:], "%g", &dl)
				f = f[:i]
			}
			fmt.Sscanf(f, "%d", &d)
			slots = append(slots, slot{d, dl})
		}
	}
	gen := func(seed int64, n int) []day { // MODEL: ordinary days, realised vol = vrp × DVOL
		rng := rand.New(rand.NewSource(seed))
		var p []day
		dvol := 38.0
		for i := 0; i < n; i++ {
			dvol = math.Max(30, math.Min(50, dvol+rng.NormFloat64()*1.5))
			v := dvol * *volScale
			daily := *vrpFlag * v / math.Sqrt(365)
			p = append(p, day{ret: rng.NormFloat64() * daily, dvol: v, negative: rng.Float64() < 0.2, volRatio: 1})
		}
		return p
	}
	if strings.HasPrefix(*name, "long") {
		var seed int64
		fmt.Sscanf(strings.TrimPrefix(*name, "long"), "%d", &seed)
		shocks[*name] = gen(seed, *days)
	}
	if *volScale != 1 && !strings.HasPrefix(*name, "long") { // scripted shocks: bigger moves, higher vol
		var scaled []day
		for _, d := range shocks[*name] {
			d.ret *= *volScale
			d.dvol *= *volScale
			scaled = append(scaled, d)
		}
		shocks[*name] = scaled
	}
	if *warm > 0 {
		shocks[*name] = append(gen(99, *warm), shocks[*name]...)
	}
	if strings.HasPrefix(*name, "normal") { // MODEL: 28 ordinary days, ~2.3 %/day, DVOL ~40
		var seed int64
		fmt.Sscanf(strings.TrimPrefix(*name, "normal"), "%d", &seed)
		rng := rand.New(rand.NewSource(seed))
		vrp := *vrpFlag
		var p []day
		dvol := 38.0
		for i := 0; i < 28; i++ {
			dvol = math.Max(30, math.Min(50, dvol+rng.NormFloat64()*1.5))
			// realised vol = vrp × implied (DVOL): the variance risk premium
			// short options harvest. 0.85 is typical; 1.0 means no edge.
			daily := vrp * dvol / math.Sqrt(365)
			p = append(p, day{ret: rng.NormFloat64() * daily, dvol: dvol, negative: rng.Float64() < 0.2, volRatio: 1})
		}
		shocks[*name] = p
	}
	path := append(append([]day{}, squeeze[:*calm]...), shocks[*name]...)

	read := func(f string, v any) {
		data, err := os.ReadFile(*dir + "/" + f)
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			panic(err)
		}
	}
	var mk struct {
		Dvol   [][2]float64 `json:"dvol"`
		Closes [][2]float64 `json:"closes"`
		Spot   float64      `json:"spot"`
	}
	read("market.json", &mk)
	var pos struct {
		Strangles []struct {
			Slot struct {
				DTE   int     `json:"dte"`
				Delta float64 `json:"delta"`
			} `json:"slot"`
			Legs []struct {
				OptionType string    `json:"option_type"`
				Strike     float64   `json:"strike"`
				Expiry     time.Time `json:"expiry"`
				Qty        float64   `json:"qty"`
				Entry      float64   `json:"entry_price"`
			} `json:"legs"`
		} `json:"strangles"`
	}
	read("positions.json", &pos)
	var acct struct {
		Snapshot struct {
			Assets []struct {
				Currency      string  `json:"currency"`
				MarginBalance float64 `json:"margin_balance"`
				IM            float64 `json:"initial_margin"`
				MM            float64 `json:"maintenance_margin"`
			} `json:"assets"`
		} `json:"snapshot"`
	}
	read("account.json", &acct)

	start := time.Now().UTC().Truncate(24 * time.Hour)
	m := market{now: start, spot: mk.Spot, dvol: mk.Dvol[len(mk.Dvol)-1][1]}
	s0 := m.spot
	b := &book{appliedLimit: math.NaN()}
	for _, st := range pos.Strangles {
		s := &strangle{slot: slot{st.Slot.DTE, st.Slot.Delta}}
		for _, l := range st.Legs {
			p := &leg{ID: fmt.Sprintf("%s-%.0f", l.Expiry.Format("Jan02"), l.Strike), Underlying: underlying, Strike: l.Strike, Expiry: l.Expiry,
				OptionType: l.OptionType, Side: orders.DirectionSell, Qty: l.Qty, EntryPrice: l.Entry, PremiumReceived: l.Entry * l.Qty, MarkLive: true}
			p.CurrentMid, p.CurrentGreeks.Delta = m.mark(p)
			if l.OptionType == "call" {
				s.call = p
			} else {
				s.put = p
			}
		}
		b.strangles = append(b.strangles, s)
	}
	for _, a := range acct.Snapshot.Assets {
		if a.Currency == underlying {
			b.mb0, b.imScale, b.mmRatio = a.MarginBalance, a.IM/rawIM(b.legs(), m), a.MM/a.IM
		}
	}
	if *flat {
		b.strangles = nil
	}
	b.u0 = unrealised(b.legs())

	dv := marketdata.NewDVOLTracker(252)
	for _, d := range mk.Dvol {
		dv.Record(time.UnixMilli(int64(d[0])), d[1]**volScale)
	}
	m.dvol *= *volScale
	regimes, _ := history.OpenRegimes("")
	regimes.Record(start.AddDate(0, 0, -2).Add(12*time.Hour), "POSITIVE/PINNING") // MODEL: a confirmed regime at the start
	regimes.Record(start.AddDate(0, 0, -1).Add(12*time.Hour), "POSITIVE/PINNING")
	var closes []float64
	for _, c := range mk.Closes {
		closes = append(closes, c[1])
	}
	policy := (&config.Config{}).RiskPolicy(true)
	const flip0 = 83577.0

	wingDeltaMax, wingNeedEM = *wingD, *wingEM
	mode := "TODAY'S BOT"
	if *condor {
		mode = "TODAY'S BOT + PROPOSED SQUEEZE CONDOR"
	}
	if *condor && *letWings {
		mode += " (wings kept, no stop)"
	}
	if *breach > 0 {
		mode += fmt.Sprintf(", defensive roll at |Δ| %.2f", *breach)
	}
	if !*quiet {
		fmt.Printf("### %s — %s, %d decisions/day\n\n", mode, *name, *steps)
		fmt.Println("| Day | Date | " + underlying + " | Δ | DVOL (pct) | Squeeze (low-band days · vol 7/20 · RV 10/20) | GEX live/conf | Limit | Entries | IM / MM % | Equity " + underlying + " | P&L USD | vs holding " + underlying + " / USD | Actions |")
		fmt.Println("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
	}
	eq0USD := b.mb0 * s0
	// Results are judged against simply holding the coin: in coin that is
	// equity − starting equity; in USD the same coin difference at today's
	// price (eq·S − eq0·S0 − eq0·(S − S0) = (eq − eq0)·S).
	worstUSD, worstDay, maxIM, maxMM := 0.0, "", 0.0, 0.0
	worstVsHold, worstVsDay := 0.0, ""
	triggerDays := 0

	var actions []string
	act := func(f string, a ...any) { actions = append(actions, fmt.Sprintf(f, a...)) }
	dayStartSpot, prevDvol := m.spot, m.dvol
	worstMM, breaches, worstEq := 0.0, 0, 0.0
	stickyF, sheds, resells := 0.0, 0, 0
	sumIM, nIM, nEntries := 0.0, 0, 0
	shared, held := 0, 0
	for i := 0; i <= len(path)**steps; i++ {
		d := (i + *steps - 1) / *steps
		dayEnd := i%*steps == 0
		chg, regime, volRatio := 0.0, "POSITIVE/PINNING", 1.0
		if i > 0 {
			p := path[d-1]
			k := (i-1)%*steps + 1
			volRatio = p.volRatio
			m.spot *= math.Pow(1+p.ret/100, 1/float64(*steps))
			m.dvol = prevDvol + (p.dvol-prevDvol)*float64(k)/float64(*steps)
			if dayEnd {
				prevDvol = p.dvol
			}
			if p.negative {
				regime = "NEGATIVE/ACCELERATION"
			}
			chg = (m.spot/dayStartSpot - 1) * 100
		}
		m.now = start.Add(time.Duration(i) * 24 * time.Hour / time.Duration(*steps))
		if dayEnd {
			closes = append(closes, m.spot)
		}
		dv.Record(m.now, m.dvol)
		regimes.Record(m.now, regime)
		flip := flip0 * math.Sqrt(m.spot/s0)
		if *stickyFlip {
			if i == 0 {
				stickyF = m.spot * (1 + *flipStart/100)
			} else {
				stickyF += (m.spot - stickyF) * (1 - math.Pow(0.5, 1/(4*float64(*steps)))) // half-life 4 days
			}
			flip = stickyF
			regime = "POSITIVE/PINNING"
			if m.spot < flip {
				regime = "NEGATIVE/ACCELERATION"
			}
			regimes.Record(m.now, regime)
		}
		for _, l := range b.legs() {
			l.CurrentMid, l.CurrentGreeks.Delta = m.mark(l)
		}

		// Margin policy, as live (daily closes, today excluded).
		ivCloses, ivToday := dv.Daily()
		today := m.now.Truncate(24 * time.Hour)
		byDay := map[time.Time]*risk.Day{}
		get := func(t time.Time) *risk.Day {
			if byDay[t] == nil {
				byDay[t] = &risk.Day{Date: t}
			}
			return byDay[t]
		}
		for _, c := range ivCloses {
			if c.Day.Before(today) {
				x := get(c.Day)
				x.IVPct, x.IVKnown = c.Percentile, c.Known
			}
		}
		for _, r := range regimes.Daily() {
			if r.Day.Before(today) {
				x := get(r.Day)
				x.Negative, x.RegimeKnown = strings.HasPrefix(r.Regime, "NEGATIVE"), true
			}
		}
		var rd []risk.Day
		for _, x := range byDay {
			rd = append(rd, *x)
		}
		sort.Slice(rd, func(i, j int) bool { return rd[i].Date.Before(rd[j].Date) })
		st := risk.Evaluate(policy, rd, risk.Day{Date: today, IVPct: ivToday.Percentile, IVKnown: ivToday.Known,
			Negative: strings.HasPrefix(regime, "NEGATIVE"), RegimeKnown: true})

		// Squeeze trigger (PROPOSED): low band confirmed for ≥ squeezeDays
		// closes, volume and realised vol both falling.
		low := 0
		for i := len(ivCloses) - 1; i >= 0 && ivCloses[i].Day.Before(today); i-- {
			if !ivCloses[i].Known || ivCloses[i].Percentile >= 30 {
				break
			}
			low++
		}
		rvRatio := realisedVol(closes, 10) / realisedVol(closes, 20)
		squeezeOn := st.Band != nil && st.Band.MinIVPct == 0 && low >= squeezeDays && volRatio < 1 && rvRatio < 1
		if squeezeOn {
			triggerDays++
		}
		insts := chain(m)

		// PROPOSED: add wings to live strangles while the squeeze is on.
		if *condor && (squeezeOn || *always) {
			credit := 0.0
			for _, s := range b.strangles {
				for _, l := range []*leg{s.call, s.put} {
					if l != nil {
						credit += l.CurrentMid * l.Qty
					}
				}
			}
			var plan []func()
			cost := 0.0
			for _, s := range b.strangles {
				s := s
				for _, pair := range []struct {
					short *leg
					wing  **leg
				}{{s.call, &s.callWing}, {s.put, &s.putWing}} {
					if pair.short == nil || *pair.wing != nil || pair.short.DTEAt(m.now) <= rolloutDTE {
						continue
					}
					in, ok := wingStrike(pair.short, m, insts)
					if !ok {
						continue
					}
					w := newLeg(in.OptionType, in.Strike, in.Expiry, pair.short.Qty, orders.DirectionBuy, m)
					cost += w.PremiumReceived
					wp, l := pair.wing, w
					plan = append(plan, func() { *wp = l })
				}
			}
			switch {
			case len(plan) == 0:
			case cost <= wingBudgetFrac*credit:
				for _, f := range plan {
					f()
				}
				b.wingCost += cost
				for _, s := range b.strangles {
					for _, w := range []*leg{s.callWing, s.putWing} {
						if w != nil && w.EntryTime.Equal(m.now) {
							b.realised -= feePerBTC * w.Qty
							b.fees += feePerBTC * w.Qty
							b.wings++
							act("BUY wing %s", short(w))
						}
					}
				}
			default:
				act("wings skipped: cost %.4f > %.0f%% of credit %.4f", cost, wingBudgetFrac*100, credit)
			}
		}

		// GEX shedding (real rule). A shed short takes its wing with it.
		tr := trend(closes)
		if *botTrend {
			tr = botTrendOf(closes)
		}
		// The buffer: the rule sees a flip lowered by flipbuf %, so a leg is
		// shed only when spot is that far below the real flip.
		buf := *flipBuf
		if *flipBufSD > 0 {
			buf = *flipBufSD * m.dvol / math.Sqrt(365)
		}
		gexAct := strategy.ResolveGammaAction(regime, true, m.spot, flip, tr, buf)
		for _, s := range b.strangles {
			if *letWings && gexAct == strategy.GammaActionClosePuts && s.putWing != nil {
				continue // defined risk already
			}
			if *letWings && gexAct == strategy.GammaActionCloseCalls && s.callWing != nil {
				continue
			}
			if gexAct == strategy.GammaActionClosePuts && s.put != nil {
				markShed(s, "put", m.now)
				sheds++
				act("GEX shed %s (%+.4f)", short(s.put), b.closeAll(s.put, m)+b.closeAll(s.putWing, m))
				b.gex++
			}
			if gexAct == strategy.GammaActionCloseCalls && s.call != nil {
				markShed(s, "call", m.now)
				sheds++
				act("GEX shed %s (%+.4f)", short(s.call), b.closeAll(s.call, m)+b.closeAll(s.callWing, m))
				b.gex++
			}
		}
		b.prune()

		// Exit rules. With a wing (PROPOSED) the stop is judged on the spread:
		// spread loss ≥ stopMult × its net credit; the short-leg stop is off.
		for _, s := range b.strangles {
			for _, pair := range []struct {
				short *leg
				wing  *leg
			}{{s.call, s.callWing}, {s.put, s.putWing}} {
				l := pair.short
				if l == nil || l.Qty < 1e-9 {
					continue
				}
				stop := stopMult
				if pair.wing != nil && *matched {
					w := pair.wing
					credit := l.PremiumReceived - w.PremiumReceived
					value := l.CurrentMid*l.Qty - w.CurrentMid*w.Qty
					maxLoss := math.Abs(w.Strike-l.Strike)/m.spot*l.Qty - credit
					dte := l.DTEAt(m.now)
					why := ""
					switch {
					case dte <= rolloutDTE:
						// the DTE roll below closes the whole structure
					case *spreadStop < 1 && maxLoss > 0 && value-credit >= *spreadStop*maxLoss:
						why = "SPREAD STOP"
						b.stops++
					case *spreadBreach && *breach > 0 && math.Abs(l.CurrentGreeks.Delta) >= *breach:
						why = "DELTA EXIT spread"
						breaches++
					case credit > 0 && credit-value >= roiTP*credit && dte >= 25:
						why = "spread take-profit"
					case math.Abs(l.CurrentGreeks.Delta) < drift && dte >= 25:
						why = "spread drift"
					}
					if why != "" {
						if why != "spread take-profit" && why != "spread drift" {
							if s.stopped == nil {
								s.stopped = map[string]time.Time{}
							}
							s.stopped[l.OptionType] = m.now
						}
						act("%s %s (%+.4f)", why, short(l), b.closeAll(l, m)+b.closeAll(w, m))
						b.rolls++
						continue
					}
					if dte > rolloutDTE {
						continue
					}
				}
				if pair.wing != nil {
					stop = 1e9
					credit := l.PremiumReceived - pair.wing.PremiumReceived
					value := l.CurrentMid*l.Qty - pair.wing.CurrentMid*pair.wing.Qty
					if !*letWings && credit > 0 && value-credit >= stopMult*credit {
						if s.stopped == nil {
							s.stopped = map[string]time.Time{}
						}
						s.stopped[l.OptionType] = m.now
						act("SPREAD STOP %s (%+.4f)", short(l), b.closeAll(l, m)+b.closeAll(pair.wing, m))
						b.stops++
						continue
					}
				}
				legDrift, legBreach := drift, *breach
				if *scaled {
					legDrift, legBreach = s.slot.delta*0.10/0.16, s.slot.delta*0.30/0.16
				}
				dec := strategy.EvaluateLeg(l, m.now, rolloutDTE, legDrift, roiTP, stop, legBreach)
				switch dec.Action {
				case strategy.ActionDeltaExit:
					act("DELTA exit %s Δ%.2f (%+.4f)", short(l), l.CurrentGreeks.Delta, b.closeAll(l, m)+b.closeAll(pair.wing, m))
					b.rolls++
					breaches++
					if *breachHold { // like a stop: re-sold only once calm
						if s.stopped == nil {
							s.stopped = map[string]time.Time{}
						}
						s.stopped[l.OptionType] = m.now
					}
				case strategy.ActionStopLoss:
					if s.stopped == nil {
						s.stopped = map[string]time.Time{}
					}
					s.stopped[l.OptionType] = m.now
					act("STOP %s (%+.4f)", short(l), b.closeAll(l, m))
					b.stops++
				case strategy.ActionRollNextMonth:
					pnl := 0.0
					for _, x := range s.all() {
						pnl += b.closeAll(x, m)
					}
					act("DTE roll %s expiry (%+.4f)", l.Expiry.Format("Jan02"), pnl)
					b.rolls++
				case strategy.ActionRollSameLeg:
					act("%s %s (%+.4f)", map[string]string{orders.TriggerRolloutDelta: "drift roll", orders.TriggerRolloutROI: "take-profit"}[dec.Reason], short(l), b.closeAll(l, m)+b.closeAll(pair.wing, m))
					b.rolls++
				}
			}
		}
		b.prune()

		// Margin policy.
		u := b.usage(m)
		mmBreach := u.MMPct() >= st.MaxMMPct
		switch {
		case mmBreach:
			for _, l := range b.legs() {
				if l.Side == orders.DirectionBuy {
					continue
				}
				keep := strategy.MMKeepQty(l.Qty, lot, u.MMPct(), st.MaxMMPct)
				if l.Qty-keep > 1e-9 {
					act("MM cut %s→%.1f (%+.4f)", short(l), keep, b.trade(l, l.Qty-keep, m))
				}
			}
			b.prune()
		case !st.Frozen && st.CanRebalance && st.LimitIMPct != b.appliedLimit:
			if !math.IsNaN(b.appliedLimit) {
				act("limit %.0f%%→%.0f%%", b.appliedLimit, st.LimitIMPct)
			}
			b.appliedLimit = st.LimitIMPct
		}

		// Repair (real hold rule for stopped legs). PROPOSED: with the squeeze
		// on, a repaired short gets its wing too.
		if !mmBreach {
			for _, s := range b.strangles {
				present, missing := s.call, "put"
				if s.call == nil {
					present, missing = s.put, "call"
				} else if s.put != nil {
					continue
				}
				if (missing == "put" && gexAct == strategy.GammaActionClosePuts) || (missing == "call" && gexAct == strategy.GammaActionCloseCalls) {
					continue
				}
				shedAt, wasShed := s.shed[missing]
				if wasShed && m.now.Sub(shedAt) < time.Duration(*shedHold*float64(time.Hour)) {
					continue // re-sell hold after a GEX shed
				}
				if wasShed {
					delete(s.shed, missing)
					resells++
				}
				if present.DTEAt(m.now) <= rolloutDTE {
					continue
				}
				if at, ok := s.stopped[missing]; ok {
					if strategy.RepairBlockReason(at, m.now, cooldown, st) != "" {
						continue
					}
					delete(s.stopped, missing)
				}
				in, err := strategy.SelectStrike(insts, present.Expiry, missing, s.slot.delta, 0.03)
				if err != nil || in.Mid < minPre {
					continue
				}
				l := newLeg(missing, in.Strike, in.Expiry, present.Qty, orders.DirectionSell, m)
				b.realised -= feePerBTC * l.Qty
				b.fees += feePerBTC * l.Qty
				desc := "repair: sell " + short(l)
				if missing == "put" {
					s.put = l
				} else {
					s.call = l
				}
				if *condor && (squeezeOn || *always) {
					if w, ok := wingStrike(l, m, insts); ok {
						wl := newLeg(missing, w.Strike, w.Expiry, l.Qty, orders.DirectionBuy, m)
						if missing == "put" {
							s.putWing = wl
						} else {
							s.callWing = wl
						}
						b.wingCost += wl.PremiumReceived
						b.wings++
						desc += " + wing " + short(wl)
					}
				}
				act("%s", desc)
			}
		}

		// Entries (frozen → none); PROPOSED: open as condors while the squeeze is on.
		entries := "open"
		switch {
		case st.Frozen:
			entries = "FROZEN"
		case mmBreach:
			entries = "MM breach"
		default:
			occupied := map[slot]bool{}
			for _, s := range b.strangles {
				occupied[s.slot] = true
			}
			vacant := 0
			for _, sl := range slots {
				if !occupied[sl] {
					vacant++
				}
			}
			for _, sl := range slots {
				if occupied[sl] {
					continue
				}
				u := b.usage(m)
				share := strategy.EntryShare(u.Headroom(st.LimitIMPct), strategy.SlotShare(st.LimitIMPct, u.MarginBalance, len(slots)), vacant)
				exp, ok := strategy.SelectExpiry(insts, m.now, sl.dte, 10, rolloutDTE)
				if *distinct != "off" {
					held := map[time.Time]bool{}
					for _, o := range b.strangles {
						for _, l := range []*leg{o.call, o.put} {
							if l != nil {
								held[l.Expiry] = true
							}
						}
					}
					if ok && held[exp] {
						alt, found := time.Time{}, false
						if *distinct != "wait" { // the nearest later free expiry, up to 1.5× the target DTE
							lo, _ := marketdata.ExpiryWindow(sl.dte, 10, rolloutDTE)
							alt, found = marketdata.NearestExpiry(insts, m.now, lo, sl.dte*3/2, held)
						}
						switch {
						case found:
							exp = alt
						case *distinct == "farther-share":
							// keep the shared expiry
						default:
							ok = false // wait for a free expiry
						}
					}
				}
				if !ok || share <= 0 {
					continue
				}
				ci, e1 := strategy.SelectStrike(insts, exp, "call", sl.delta, 0.03)
				pi, e2 := strategy.SelectStrike(insts, exp, "put", sl.delta, 0.03)
				if e1 != nil || e2 != nil || ci.Mid < minPre || pi.Mid < minPre {
					continue
				}
				s := &strangle{slot: sl}
				if gexAct != strategy.GammaActionCloseCalls {
					s.call = newLeg("call", ci.Strike, exp, lot, orders.DirectionSell, m)
				}
				if gexAct != strategy.GammaActionClosePuts {
					s.put = newLeg("put", pi.Strike, exp, lot, orders.DirectionSell, m)
				}
				if *condor && (squeezeOn || *always) {
					for _, pair := range []struct {
						short *leg
						wing  **leg
					}{{s.call, &s.callWing}, {s.put, &s.putWing}} {
						if pair.short == nil {
							continue
						}
						if w, ok := wingStrike(pair.short, m, insts); ok {
							*pair.wing = newLeg(w.OptionType, w.Strike, w.Expiry, lot, orders.DirectionBuy, m)
						}
					}
				}
				cw, pw := s.callWing, s.putWing
				if *sizeAsStrangle {
					s.callWing, s.putWing = nil, nil
				}
				b.strangles = append(b.strangles, s)
				perLot := b.usage(m).IM - u.IM
				lots := strategy.EntryLots(share, perLot)
				alone := rawIM(s.all(), m) * b.imScale
				s.callWing, s.putWing = cw, pw
				normal := strategy.TargetLots(strategy.SlotShare(st.LimitIMPct, u.MarginBalance, len(slots)), alone)
				if capped, bound := strategy.CapLots(lots, normal, *capMult); bound {
					act("size cap: %d → %d lots (normal %d)", lots, capped, normal)
					lots = capped
				}
				if lots < 1 {
					b.strangles = b.strangles[:len(b.strangles)-1]
					continue
				}
				// Like the live sizeEntry: confirm the post-trade IM/MM fit the
				// limits at the final size, shrinking up to three times.
				fits := false
				for attempt := 0; attempt < 3 && lots >= 1; attempt++ {
					for _, l := range s.all() {
						l.Qty = float64(lots) * lot
					}
					post := b.usage(m)
					if risk.Fits(post, st.LimitIMPct, st.MaxMMPct) {
						fits = true
						break
					}
					over := math.Max(post.IMPct()/st.LimitIMPct, post.MMPct()/st.MaxMMPct)
					lots = min(lots-1, int(math.Floor(float64(lots)/over)))
				}
				if !fits {
					b.strangles = b.strangles[:len(b.strangles)-1]
					continue
				}
				var names []string
				for _, l := range s.all() {
					l.Qty = float64(lots) * lot
					l.PremiumReceived = l.EntryPrice * l.Qty
					b.realised -= feePerBTC * l.Qty
					b.fees += feePerBTC * l.Qty
					if l.Side == orders.DirectionBuy {
						b.wingCost += l.PremiumReceived
						b.wings++
					}
					names = append(names, short(l))
				}
				kind := "strangle"
				if s.callWing != nil || s.putWing != nil {
					kind = "CONDOR"
				}
				act("entry %dd %s: %s", sl.dte, kind, strings.Join(names, " + "))
				nEntries++
			}
		}

		u = b.usage(m)
		maxIM = math.Max(maxIM, u.IMPct())
		maxMM = math.Max(maxMM, u.MMPct())
		worstEq = math.Min(worstEq, (u.MarginBalance/b.mb0-1)*100)
		sumIM += u.IMPct()
		nIM++
		cnt := map[time.Time]int{}
		for _, o := range b.strangles {
			if l := o.call; l != nil {
				cnt[l.Expiry]++
			} else if l := o.put; l != nil {
				cnt[l.Expiry]++
			}
		}
		for _, c := range cnt {
			if c > 1 {
				shared += c
			}
		}
		held += len(b.strangles)
		worstMM = math.Max(worstMM, u.MMPct())
		if !dayEnd {
			continue
		}
		dayStartSpot = m.spot
		eq := u.MarginBalance
		pnl := eq*m.spot - eq0USD
		if pnl < worstUSD {
			worstUSD, worstDay = pnl, m.now.Format("Jan 2")
		}
		vsHold := (eq - b.mb0) * m.spot
		if vsHold < worstVsHold {
			worstVsHold, worstVsDay = vsHold, m.now.Format("Jan 2")
		}
		if len(actions) == 0 {
			actions = []string{"—"}
		}
		gexCell := map[bool]string{true: "NEG", false: "POS"}[strings.HasPrefix(regime, "NEGATIVE")] + "/" + map[bool]string{true: "NEG", false: "POS"}[st.RegimeNegative]
		sq := fmt.Sprintf("%d · %.2f · %.2f", low, volRatio, rvRatio)
		if squeezeOn {
			sq += " **ON**"
		}
		if *quiet {
			actions = nil
		}
		if !*quiet {
			fmt.Printf("| %d | %s | %.0f | %+.1f%% | %.0f (%.0f) | %s | %s | %.0f%% | %s | %.1f / %.1f | %.4f | %+.0f | %+.4f / %+.0f | %s |\n",
				d, m.now.Format("Jan 2"), m.spot, chg, m.dvol, ivToday.Percentile, sq, gexCell, st.LimitIMPct, entries,
				u.IMPct(), u.MMPct(), eq, pnl, eq-b.mb0, vsHold, strings.Join(actions, "; "))
			actions = nil
		}
	}
	eq := b.marginBalance()
	if *breachHold {
		*name += "+hold"
	}
	fmt.Printf("RESULT\t%s\t%.2f\t%.4f\t%.2f\t%.0f\t%.0f\t%d\t%d\t%d\t%.4f\t%.1f\t%.2f\t%d\t%.4f\t%d\t%.4f\t%.0f\n", *name, *breach, eq, (eq/b.mb0-1)*100,
		eq*m.spot-eq0USD, b.mb0*(m.spot-s0), b.stops, breaches, b.rolls, b.realised, worstMM, worstEq, b.wings, b.wingCost, triggerDays,
		eq-b.mb0, (eq-b.mb0)*m.spot)
	fmt.Printf("DTE\t%.2f\t%.4f\t%d\t%.1f\t%.1f\t%.2f\t%d\t%d\n", (eq/b.mb0-1)*100, b.fees, nEntries, sumIM/float64(max(nIM, 1)),
		100*float64(shared)/float64(max(held, 1)), float64(held)/float64(max(nIM, 1)), sheds, resells)
	u := underlying
	usd0, usd1, hold1 := eq0USD, eq*m.spot, b.mb0*m.spot
	fmt.Printf("\n**%s, %s:** **vs holding the coin: %+.4f %s · $%+.0f** (worst %s $%+.0f) · %s: %.4f → %.4f (%+.1f%%) · USD: $%.0f → $%.0f (%+.1f%%), holding → $%.0f (%+.1f%%) · %s %.0f → %.0f (%+.1f%%) · realised %+.4f %s net (fees paid %.4f) · wings bought %d (cost %.4f %s) · stops %d · GEX sheds %d · rolls %d · max IM %.1f%% · max MM %.1f%% · worst day $%+.0f on %s · squeeze trigger on %d days\n",
		mode, *name, eq-b.mb0, u, (eq-b.mb0)*m.spot, worstVsDay, worstVsHold,
		u, b.mb0, eq, (eq/b.mb0-1)*100, usd0, usd1, (usd1/usd0-1)*100, hold1, (hold1/usd0-1)*100,
		u, s0, m.spot, (m.spot/s0-1)*100,
		b.realised, u, b.fees, b.wings, b.wingCost, u, b.stops, b.gex, b.rolls, maxIM, maxMM, worstUSD, worstDay, triggerDays)
}
