package strategy

import (
	"log/slog"
	"math"
	"strings"
	"time"

	"optionsbot/internal/gex"
)

// GammaAction describes what the gamma monitor wants to do.
type GammaAction int

const (
	GammaActionNone       GammaAction = iota
	GammaActionClosePuts              // bear market: close put legs
	GammaActionCloseCalls             // bull market: close call legs
)

// GammaDecision carries both the action and contextual GEX data for logging.
//
// Action is the live signal: entry and repair skip a leg type it sheds.
// Shed is the same action once it has held for the confirm count of GEX
// snapshots (SetShedConfirm); only Shed closes legs, so a one-snapshot flip
// jump does not buy back the book.
type GammaDecision struct {
	Action         GammaAction
	Shed           GammaAction
	Trend          string
	TrendDir       int // +1 bull, -1 bear, 0 neutral (Trend as a number)
	Regime         string
	RegimeScore    float64
	GammaFlip      float64
	GammaFlipFound bool
	SwingHigh      float64
	SwingLow       float64
	SMA9           float64
	SMA21          float64
	// FlipBufferPct is how far below the flip spot must be (%) before a leg
	// is shed: gamma_flip_buffer_sd daily standard deviations (0 = none).
	FlipBufferPct float64
	// Spot and SnapshotAt are the GEX snapshot's spot and time (zero
	// without a snapshot): a shed leg's re-sell counts snapshots, not cycles.
	Spot       float64
	SnapshotAt time.Time
	// NonNegativeSince is when the live regime last stopped being negative
	// (zero while it is negative or before the first snapshot).
	NonNegativeSince time.Time
}

// Sheds reports whether the live signal sheds legs of optType.
func (d GammaDecision) Sheds(optType string) bool {
	return (optType == "put" && d.Action == GammaActionClosePuts) ||
		(optType == "call" && d.Action == GammaActionCloseCalls)
}

// GammaMonitor combines the market-wide GEX regime (from the GEX manager) with
// a short-term price trend to decide whether to keep or shed a strangle leg.
//
// Decision logic:
//   - GEX POSITIVE/PINNING: market makers dampen moves → full strangle is safe → ActionNone
//   - GEX NEGATIVE/ACCELERATION + bear trend → calls dominate risk side → ActionClosePuts (put side is at risk as price falls through it)
//   - GEX NEGATIVE/ACCELERATION + bull trend → ActionCloseCalls
//   - GEX NEGATIVE + no clear trend → ActionNone (wait for confirmation)
//
// The existing crude net-portfolio-gamma check is replaced by the market-wide
// GEX regime score, which uses real open interest across the full chain.
type GammaMonitor struct {
	lookbackDays  int
	swingPivotN   int          // days required on each side to confirm a swing pivot
	dailyCloses   []pricePoint // one entry per completed UTC day
	lastTickDate  time.Time    // UTC day of the last processed tick
	lastTickPrice float64      // most recent price (current day's running close)
	gexSrc        GEXSource    // nil until wired; Evaluate then reports no regime
	lastAction    GammaAction  // last action announced, so changes are logged once
	bufferSD      float64      // flip buffer in daily standard deviations (0 = none)
	dvol          func() float64

	shedConfirm int         // snapshots in a row the signal must hold before Shed (≤ 1 = at once)
	signal      GammaAction // live signal on the last counted snapshot
	signalRun   int         // consecutive snapshots it has held
	lastSnapAt  time.Time   // last snapshot counted
	nonNegSince time.Time   // see GammaDecision.NonNegativeSince
}

// SetShedConfirm makes a leg shed only once the live signal has held for n
// consecutive GEX snapshots (one per minute). The script's flip (lowest
// crossing) jumps several percent when open interest shifts — ETH about
// twice a day, 40 % of the jumps reverting within the hour — and shedding on
// the first snapshot then re-selling bought the book back at the spread.
func (g *GammaMonitor) SetShedConfirm(n int) { g.shedConfirm = n }

// SetFlipBuffer makes a leg shed only once spot is bufferSD daily standard
// deviations below the flip, the deviation taken from dvol (the asset's
// implied volatility index, %) at that moment.
func (g *GammaMonitor) SetFlipBuffer(bufferSD float64, dvol func() float64) {
	g.bufferSD, g.dvol = bufferSD, dvol
}

// FlipBufferPct is bufferSD daily standard deviations in percent of spot:
// bufferSD × DVOL ÷ √365. In stress simulations a buffer of 1σ beat none on
// 10 of 11 paths at BTC-, ETH- and SOL-like volatility, while the best fixed
// percentage moved with volatility (2 % BTC, 3–4 % ETH, 6 % SOL) — dips of a
// fraction of a day's normal move under the flip are noise, and shedding on
// them (then re-selling on the way back) only pays the spread twice. Without
// a DVOL reading it is 0: shedding then works as before, never less.
func FlipBufferPct(bufferSD, dvol float64) float64 {
	if bufferSD <= 0 || dvol <= 0 {
		return 0
	}
	return bufferSD * dvol / math.Sqrt(365)
}

type pricePoint struct {
	timestamp time.Time
	price     float64
}

func NewGammaMonitor(lookbackDays, swingPivotN int) *GammaMonitor {
	return &GammaMonitor{lookbackDays: lookbackDays, swingPivotN: swingPivotN}
}

// SetGEXSource wires the market-wide GEX snapshot provider.
func (g *GammaMonitor) SetGEXSource(src GEXSource) {
	g.gexSrc = src
}

// SeedDailyCloses pre-populates the daily close history on startup so trend and
// swing decisions are available immediately without waiting for days to accumulate.
func (g *GammaMonitor) SeedDailyCloses(closes []pricePoint) {
	g.dailyCloses = make([]pricePoint, len(closes))
	copy(g.dailyCloses, closes)
	if len(closes) > 0 {
		last := closes[len(closes)-1]
		g.lastTickPrice = last.price
		g.lastTickDate = last.timestamp.Truncate(24 * time.Hour)
	}
}

// PushPrice records the latest underlying price. On UTC day rollover it commits
// the previous day's last price into dailyCloses, which drives swing and trend logic.
func (g *GammaMonitor) PushPrice(price float64) {
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)

	if g.lastTickDate.IsZero() {
		// First call — initialise without recording a close yet.
		g.lastTickDate = today
		g.lastTickPrice = price
	} else if today.After(g.lastTickDate) {
		// Day rolled over: commit the previous day's last price as a daily close.
		g.dailyCloses = append(g.dailyCloses, pricePoint{g.lastTickDate, g.lastTickPrice})
		// Keep enough history for lookback + pivot neighbours + 1 buffer day.
		cutoff := today.AddDate(0, 0, -(g.lookbackDays + g.swingPivotN + 1))
		for len(g.dailyCloses) > 1 && g.dailyCloses[0].timestamp.Before(cutoff) {
			g.dailyCloses = g.dailyCloses[1:]
		}
		g.lastTickDate = today
	}
	g.lastTickPrice = price
}

// ResolveGammaAction is the pure decision function for which strangle leg (if any)
// to close, given the GEX regime label, raw spot vs flip, short-term trend and
// the flip buffer (% below the flip spot must be before a leg is shed).
//
// Full strangle (ActionNone) whenever ANY of these are true:
//   - No gamma flip found — no reference point, safe default
//   - spot ≥ flip (raw) — spot is in or above POSITIVE territory; the hysteresis-
//     adjusted regime label may still say NEGATIVE if spot hasn't cleared the full
//     band yet, but the raw position above the flip means market makers are net long
//     gamma near current price and both legs are appropriate
//   - regime == "POSITIVE/PINNING" — hysteresis is holding POSITIVE even though spot
//     dipped just below the flip; protects both legs in the transition band
//
// Single-leg action only when spot is genuinely below the flip — by more than
// bufferPct — AND regime is NEGATIVE/ACCELERATION: fully confirmed negative
// territory, not the band, and not a dip of normal daily noise.
func ResolveGammaAction(regime string, flipFound bool, spot, flip float64, trend int, bufferPct float64) GammaAction {
	if !flipFound || spot >= flip*(1-bufferPct/100) || regime == "POSITIVE/PINNING" {
		return GammaActionNone
	}
	switch trend {
	case -1:
		return GammaActionClosePuts
	case 1:
		return GammaActionCloseCalls
	default:
		return GammaActionNone
	}
}

// Evaluate checks the GEX regime and, when in confirmed negative territory,
// applies trend direction to pick which leg to shed.
func (g *GammaMonitor) Evaluate() GammaDecision {
	snap := g.gexSnapshot()
	if snap == nil {
		// No GEX data yet — fall back to silent no-op (don't trade on missing data)
		return GammaDecision{Action: GammaActionNone, Regime: "UNKNOWN"}
	}

	trend := g.detectTrend()
	dec := GammaDecision{
		Regime:         snap.Regime,
		RegimeScore:    snap.RegimeScore,
		GammaFlip:      snap.GammaFlip,
		GammaFlipFound: snap.GammaFlipFound,
		Trend:          g.trendLabel(),
		TrendDir:       trend,
		SwingHigh:      g.lastSwingHigh(),
		SwingLow:       g.lastSwingLow(),
		SMA9:           g.sma(9),
		SMA21:          g.sma(21),
	}

	if g.dvol != nil {
		dec.FlipBufferPct = FlipBufferPct(g.bufferSD, g.dvol())
	}
	dec.Action = ResolveGammaAction(snap.Regime, snap.GammaFlipFound, snap.Spot, snap.GammaFlip, trend, dec.FlipBufferPct)
	dec.Spot, dec.SnapshotAt = snap.Spot, snap.ComputedAt
	g.observe(snap, dec.Action)
	dec.NonNegativeSince = g.nonNegSince
	if dec.Action != GammaActionNone && g.signal == dec.Action && g.signalRun >= g.shedConfirm {
		dec.Shed = dec.Action
	}

	// Announce a regime action when it changes, not on every cycle it persists.
	changed := dec.Action != g.lastAction
	g.lastAction = dec.Action
	if changed && dec.Action == GammaActionNone {
		slog.Info("gex_regime_cleared", "event", "gex_regime_cleared", "regime", snap.Regime, "trend", dec.Trend,
			"gex_spot", snap.Spot, "gamma_flip", snap.GammaFlip, "flip_buffer_pct", dec.FlipBufferPct)
	}
	if changed && dec.Action != GammaActionNone {
		slog.Warn("gex_regime_trigger",
			"event", "gex_regime_trigger",
			"regime", snap.Regime,
			"regime_score", snap.RegimeScore,
			"gamma_flip", snap.GammaFlip,
			"gamma_flip_found", snap.GammaFlipFound,
			"flip_buffer_pct", dec.FlipBufferPct,
			"shed_below", snap.GammaFlip*(1-dec.FlipBufferPct/100),
			"trend", dec.Trend,
			"action", actionLabel(dec.Action),
			"gex_spot", snap.Spot,
			"tick_price", g.lastTickPrice,
			"swing_high", dec.SwingHigh,
			"swing_low", dec.SwingLow,
			"sma9", dec.SMA9,
			"sma21", dec.SMA21,
		)
	}
	return dec
}

// observe counts each GEX snapshot once (Evaluate runs several times per
// snapshot): how long the live signal has held and since when the regime is
// non-negative.
func (g *GammaMonitor) observe(snap *gex.Snapshot, action GammaAction) {
	if !snap.ComputedAt.IsZero() && !snap.ComputedAt.After(g.lastSnapAt) {
		return
	}
	g.lastSnapAt = snap.ComputedAt
	if action != GammaActionNone && action == g.signal {
		g.signalRun++
	} else {
		g.signal, g.signalRun = action, 1
	}
	if strings.HasPrefix(snap.Regime, "NEGATIVE") {
		g.nonNegSince = time.Time{}
	} else if g.nonNegSince.IsZero() {
		g.nonNegSince = snap.ComputedAt
	}
	if action != GammaActionNone && g.signalRun < g.shedConfirm && g.signalRun == 1 {
		slog.Info("gex_shed_pending", "event", "gex_shed_pending", "action", actionLabel(action),
			"confirm_snapshots", g.shedConfirm, "gex_spot", snap.Spot, "gamma_flip", snap.GammaFlip)
	}
}

// Trend returns the current price trend as a human-readable label.
func (g *GammaMonitor) Trend() string { return g.trendLabel() }

// CurrentGEXSnapshot returns the live GEX snapshot for use in order log enrichment.
func (g *GammaMonitor) CurrentGEXSnapshot() *gex.Snapshot { return g.gexSnapshot() }

func (g *GammaMonitor) gexSnapshot() *gex.Snapshot {
	if g.gexSrc == nil {
		return nil
	}
	return g.gexSrc.Snapshot()
}

func (g *GammaMonitor) trendLabel() string {
	switch g.detectTrend() {
	case 1:
		return "bull"
	case -1:
		return "bear"
	default:
		return "neutral"
	}
}

// detectTrend uses daily swing levels to determine direction.
// Bull (+1): current price broke above the last confirmed swing high.
// Bear (-1): current price broke below the last confirmed swing low.
// Neutral (0): price is between swing levels, or not enough history yet.
func (g *GammaMonitor) detectTrend() int {
	swingHigh := g.lastSwingHigh()
	swingLow := g.lastSwingLow()
	sma21 := g.sma(21)
	sma9 := g.sma(9)
	if swingHigh == 0 && swingLow == 0 {
		return 0
	}
	price := g.lastTickPrice

	BullishBreakout := swingHigh > 0 && price > swingHigh
	BearishBreakdown := swingLow > 0 && price < swingLow
	// Price must be above SMA9 (not just SMA21) for a bull signal. When price
	// is between the two SMAs (sma21 < price < sma9) that is a neutral
	// consolidation zone, not a confirmed trend. Using "price > sma21" alone
	// triggered bull even mid-consolidation, causing gamma_close to kill
	// freshly-opened call legs every eval cycle.
	BullishSMA := sma21 > 0 && sma9 > sma21 && price > sma9
	BearishSMA := sma21 > 0 && sma9 < sma21 && price < sma9

	if BullishBreakout || BullishSMA {
		return 1
	}
	if BearishBreakdown || BearishSMA {
		return -1
	}
	return 0
}

// sma returns the simple moving average of the last 21 daily closes.
func (g *GammaMonitor) sma(days int) float64 {
	n := len(g.dailyCloses)
	if n == 0 {
		return 0
	}
	window := days
	if n < window {
		window = n
	}
	sum := 0.0
	for _, p := range g.dailyCloses[n-window:] {
		sum += p.price
	}
	return sum / float64(window)
}

// lastSwingHigh returns the most recent confirmed daily swing high.
// A swing high at index i requires dailyCloses[i].price to exceed all closes
// in the g.swingPivotN days on each side. Returns 0 if none found.
func (g *GammaMonitor) lastSwingHigh() float64 {
	h := g.dailyCloses
	n := g.swingPivotN
	for i := len(h) - n - 1; i >= n; i-- {
		p := h[i].price
		isHigh := true
		for j := i - n; j < i; j++ {
			if h[j].price >= p {
				isHigh = false
				break
			}
		}
		if isHigh {
			for j := i + 1; j <= i+n; j++ {
				if h[j].price >= p {
					isHigh = false
					break
				}
			}
		}
		if isHigh {
			return p
		}
	}
	return 0
}

// lastSwingLow returns the most recent confirmed daily swing low.
// Returns 0 if none found.
func (g *GammaMonitor) lastSwingLow() float64 {
	h := g.dailyCloses
	n := g.swingPivotN
	for i := len(h) - n - 1; i >= n; i-- {
		p := h[i].price
		isLow := true
		for j := i - n; j < i; j++ {
			if h[j].price <= p {
				isLow = false
				break
			}
		}
		if isLow {
			for j := i + 1; j <= i+n; j++ {
				if h[j].price <= p {
					isLow = false
					break
				}
			}
		}
		if isLow {
			return p
		}
	}
	return 0
}

func actionLabel(a GammaAction) string {
	switch a {
	case GammaActionClosePuts:
		return "close_puts"
	case GammaActionCloseCalls:
		return "close_calls"
	default:
		return "none"
	}
}
