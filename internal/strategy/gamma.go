package strategy

import (
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"optionsbot/internal/gex"
	"optionsbot/internal/orders"
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
	Action GammaAction
	Shed   GammaAction
	// Trend/TrendDir is the direction the rules use: the SMA trend, or the
	// GEX bias under gex_direction: bias (bias up → "bull", down → "bear").
	Trend    string
	TrendDir int // +1 bull, -1 bear, 0 neutral (Trend as a number)
	// Direction is the source of Trend: DirectionSMA or DirectionBias. Both
	// are always computed and recorded, so the two can be compared.
	Direction string
	SMATrend  string // bull | bear | neutral (detectTrend)
	SMADir    int
	Bias      string // up | down | neutral (StableBiasDir)
	BiasDir   int
	// BiasFlip is the median flip of the last gex_bias_flip_window_minutes
	// the bias was also held against (0 = live flip only).
	BiasFlip float64
	// BiasWarmupUntil is when the flip window will cover its span, while it
	// does not yet (zero once it does, or outside gex_direction: bias).
	// Until then the median is only the last few flips — after a restart, a
	// flip jump fills it at once — so the bias sheds nothing; WarmupHeld is
	// the shed it held back (GammaActionNone when none).
	BiasWarmupUntil time.Time
	WarmupHeld      GammaAction

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

// Direction sources (config gex_direction).
const (
	DirectionSMA  = "sma"  // swing levels and SMAs of daily closes (detectTrend)
	DirectionBias = "bias" // the reference script's ACCELERATION BIAS with the flip buffer (BiasDir)
)

// BiasDir is the reference script's direction in a negative regime
// (infer_bias: spot above the flip → ACCELERATION BIAS UP, below → DOWN),
// with the shed's buffer around the flip: +1 when spot is above
// flip × (1 + buffer), −1 below flip × (1 − buffer), 0 in between, in a
// non-negative regime or without a flip. Within the buffer is daily noise.
func BiasDir(regime string, flipFound bool, spot, flip, bufferPct float64) int {
	if !flipFound || flip <= 0 || spot <= 0 || !strings.HasPrefix(regime, "NEGATIVE") {
		return 0
	}
	switch {
	case spot > flip*(1+bufferPct/100):
		return 1
	case spot < flip*(1-bufferPct/100):
		return -1
	}
	return 0
}

// StableBiasDir is BiasDir held against two flips: the live one and
// medianFlip, the median of the last window's snapshots. When they disagree
// the flip itself is moving, not spot, and the bias is neutral. In the last
// hours before a daily expiry the script's lowest-crossing flip can jump far
// from spot for minutes (BTC 2026-10-12 83,422 → 72,000 for 20 min,
// 2026-10-14 → 79,500 for 3 min; both read as "bias up" and shed every call
// with spot flat). Replayed on 10 days of per-minute snapshots, a 60-minute
// median removed those sheds (and ETH's three) and kept the real drop of
// 2026-10-12. medianFlip ≤ 0 → the live flip alone.
func StableBiasDir(regime string, flipFound bool, spot, flip, medianFlip, bufferPct float64) int {
	live := BiasDir(regime, flipFound, spot, flip, bufferPct)
	if medianFlip <= 0 || BiasDir(regime, flipFound, spot, medianFlip, bufferPct) == live {
		return live
	}
	return 0
}

// ResolveBiasAction sheds the side the bias runs toward: puts on bias down
// (a drop accelerating through the flip), calls on bias up (a rally).
func ResolveBiasAction(bias int) GammaAction {
	switch bias {
	case -1:
		return GammaActionClosePuts
	case 1:
		return GammaActionCloseCalls
	}
	return GammaActionNone
}

func biasWord(bias int) string {
	switch {
	case bias > 0:
		return "up"
	case bias < 0:
		return "down"
	}
	return "neutral"
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
	swingPivotN   int                 // days required on each side to confirm a swing pivot
	dailyCloses   []orders.DailyClose // one entry per completed UTC day
	lastTickDate  time.Time           // UTC day of the last processed tick
	lastTickPrice float64             // most recent price (current day's running close)
	live          orders.DailyClose   // today's candle so far (candles.go), committed on the day roll
	gexSrc        GEXSource           // nil until wired; Evaluate then reports no regime
	lastAction    GammaAction         // last action announced, so changes are logged once
	bufferSD      float64             // flip buffer in daily standard deviations (0 = none)
	direction     string              // DirectionSMA (default) or DirectionBias
	flipWindow    time.Duration       // StableBiasDir's median window (0 = live flip only)
	flips         []flipPoint         // each snapshot's flip within flipWindow, oldest first
	flipsSince    time.Time           // first flip of the unbroken record (startup, or after a gap longer than the window)
	dvol          func() float64
	sma9BandSD    float64 // SMA9 band in daily standard deviations (SetSMA9Band; 0 = none)
	sma9Side      int     // side of SMA9 the price was last on (SMA9Side), 0 before the first price

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

// SetDirection picks the direction source: DirectionBias, or the SMA trend
// for anything else.
func (g *GammaMonitor) SetDirection(d string) { g.direction = d }

func (g *GammaMonitor) usesBias() bool { return g.direction == DirectionBias }

// SetBiasFlipWindow holds the bias against the median flip of the last d as
// well as the live one (StableBiasDir); 0 = the live flip alone.
func (g *GammaMonitor) SetBiasFlipWindow(d time.Duration) { g.flipWindow = d }

type flipPoint struct {
	at   time.Time
	flip float64
}

// recordFlip keeps snap's flip once per snapshot and drops those older than
// the window.
func (g *GammaMonitor) recordFlip(snap *gex.Snapshot) {
	if g.flipWindow <= 0 || !snap.GammaFlipFound || snap.GammaFlip <= 0 {
		return
	}
	n := len(g.flips)
	if n > 0 && !snap.ComputedAt.After(g.flips[n-1].at) {
		return
	}
	cut := snap.ComputedAt.Add(-g.flipWindow)
	if n == 0 || !g.flips[n-1].at.After(cut) { // nothing within the window: the record starts over
		g.flipsSince = snap.ComputedAt
	}
	g.flips = append(g.flips, flipPoint{at: snap.ComputedAt, flip: snap.GammaFlip})
	i := 0
	for i < len(g.flips)-1 && !g.flips[i].at.After(cut) {
		i++
	}
	g.flips = g.flips[i:]
}

// medianFlip is the median of the recorded flips (0 without any).
func (g *GammaMonitor) medianFlip() float64 {
	if len(g.flips) == 0 {
		return 0
	}
	v := make([]float64, len(g.flips))
	for i, p := range g.flips {
		v[i] = p.flip
	}
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// biasWarmupUntil is when the flip record will span the whole window, while
// it does not yet; zero once it does, without a window or outside
// DirectionBias. After the 2026-10-14 16:57 restart the window held ten
// minutes of a pre-expiry flip jump (79,500), its median was the jump itself,
// and BTC's calls were shed on it.
func (g *GammaMonitor) biasWarmupUntil(snap *gex.Snapshot) time.Time {
	if !g.usesBias() || g.flipWindow <= 0 {
		return time.Time{}
	}
	if g.flipsSince.IsZero() { // no flip yet: warm-up runs from this snapshot
		return snap.ComputedAt.Add(g.flipWindow)
	}
	if until := g.flipsSince.Add(g.flipWindow); snap.ComputedAt.Before(until) {
		return until
	}
	return time.Time{}
}

// bias is StableBiasDir on snap with the window's median flip.
func (g *GammaMonitor) bias(snap *gex.Snapshot, bufferPct float64) (dir int, medianFlip float64) {
	medianFlip = g.medianFlip()
	return StableBiasDir(snap.Regime, snap.GammaFlipFound, snap.Spot, snap.GammaFlip, medianFlip, bufferPct), medianFlip
}

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

// SetSMA9Band gives SMA9 a band of bandSD daily standard deviations (DVOL ÷
// √365, the live DVOL from SetFlipBuffer) for the SMA trend: the price
// changes side of SMA9 only once it is past the band (SMA9Side). 0 = none.
func (g *GammaMonitor) SetSMA9Band(bandSD float64) { g.sma9BandSD = bandSD }

// sma9BandPct is the SMA9 band now, in % of SMA9 (0 without a DVOL reading).
func (g *GammaMonitor) sma9BandPct() float64 {
	if g.dvol == nil {
		return 0
	}
	return FlipBufferPct(g.sma9BandSD, g.dvol())
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
func (g *GammaMonitor) SeedDailyCloses(closes []orders.DailyClose) {
	g.dailyCloses = make([]orders.DailyClose, len(closes))
	copy(g.dailyCloses, closes)
	if len(closes) > 0 {
		last := closes[len(closes)-1]
		g.lastTickPrice = last.Close
		g.lastTickDate = last.Date.Truncate(24 * time.Hour)
		g.live = candleFrom(last, g.lastTickDate)
	}
}

// PushPrice records the latest underlying price. On UTC day rollover it commits
// the previous day's last price into dailyCloses, which drives swing and trend logic.
func (g *GammaMonitor) PushPrice(price float64) { g.PushPriceAt(time.Now(), price) }

// PushPriceAt is PushPrice at now (cmd/stress -replay runs the recorded past).
func (g *GammaMonitor) PushPriceAt(now time.Time, price float64) {
	today := now.UTC().Truncate(24 * time.Hour)

	if g.lastTickDate.IsZero() {
		// First call — initialise without recording a close yet.
		g.lastTickDate = today
		g.lastTickPrice = price
		g.live = orders.DailyClose{Date: today, Open: price, High: price, Low: price, Close: price}
	} else if today.After(g.lastTickDate) {
		// Day rolled over: commit the previous day's last price as a daily
		// close, with the candle its prices made (only Close drives the trend).
		g.dailyCloses = append(g.dailyCloses, committedCandle(g.live, g.lastTickDate, g.lastTickPrice))
		g.live = orders.DailyClose{Date: today, Open: price, High: price, Low: price, Close: price}
		// Keep enough history for lookback + pivot neighbours + 1 buffer day.
		cutoff := today.AddDate(0, 0, -(g.lookbackDays + g.swingPivotN + 1))
		for len(g.dailyCloses) > 1 && g.dailyCloses[0].Date.Before(cutoff) {
			g.dailyCloses = g.dailyCloses[1:]
		}
		g.lastTickDate = today
	}
	g.lastTickPrice = price
	g.live = extendCandle(g.live, price)
	g.sma9Side = SMA9Side(price, smaOf(g.dailyCloses, 9), g.sma9BandPct(), g.sma9Side)
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

	levels := g.SMALevels()
	smaDir := levels.TrendDir
	dec := GammaDecision{
		Regime:         snap.Regime,
		RegimeScore:    snap.RegimeScore,
		GammaFlip:      snap.GammaFlip,
		GammaFlipFound: snap.GammaFlipFound,
		Direction:      DirectionSMA,
		SMATrend:       trendWord(smaDir),
		SMADir:         smaDir,
		SwingHigh:      levels.SwingHigh,
		SwingLow:       levels.SwingLow,
		SMA9:           levels.SMA9,
		SMA21:          levels.SMA21,
	}

	if g.dvol != nil {
		dec.FlipBufferPct = FlipBufferPct(g.bufferSD, g.dvol())
	}
	g.recordFlip(snap)
	dec.BiasDir, dec.BiasFlip = g.bias(snap, dec.FlipBufferPct)
	dec.Bias = biasWord(dec.BiasDir)
	if g.usesBias() {
		dec.Direction, dec.TrendDir = DirectionBias, dec.BiasDir
		dec.Action = ResolveBiasAction(dec.BiasDir)
	} else {
		dec.TrendDir = smaDir
		dec.Action = ResolveGammaAction(snap.Regime, snap.GammaFlipFound, snap.Spot, snap.GammaFlip, smaDir, dec.FlipBufferPct)
	}
	dec.Trend = trendWord(dec.TrendDir)
	dec.Spot, dec.SnapshotAt = snap.Spot, snap.ComputedAt
	g.observe(snap, dec.Action)
	dec.NonNegativeSince = g.nonNegSince
	dec.BiasWarmupUntil = g.biasWarmupUntil(snap)
	if dec.Action != GammaActionNone && g.signal == dec.Action && g.signalRun >= g.shedConfirm {
		if dec.BiasWarmupUntil.IsZero() {
			dec.Shed = dec.Action
		} else {
			dec.WarmupHeld = dec.Action // entries and repairs still skip the side (Action)
		}
	}

	// Announce a regime action when it changes, not on every cycle it persists.
	changed := dec.Action != g.lastAction
	g.lastAction = dec.Action
	if changed && dec.Action == GammaActionNone {
		slog.Info("gex_regime_cleared", "event", "gex_regime_cleared", "regime", snap.Regime, "trend", dec.Trend,
			"direction", dec.Direction, "sma_trend", dec.SMATrend, "gex_bias", dec.Bias,
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
			"direction", dec.Direction,
			"sma_trend", dec.SMATrend,
			"gex_bias", dec.Bias,
			"bias_flip", dec.BiasFlip,
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

// Trend returns the direction the rules use now (bull | bear | neutral): the
// SMA trend, or the GEX bias under DirectionBias.
func (g *GammaMonitor) Trend() string {
	if g.usesBias() {
		return trendWord(g.biasNow())
	}
	return g.trendLabel()
}

// SMATrend is the SMA trend whatever the direction source (recorded for comparison).
func (g *GammaMonitor) SMATrend() string { return g.trendLabel() }

// Bias is the GEX bias now (up | down | neutral) whatever the direction source.
func (g *GammaMonitor) Bias() string { return biasWord(g.biasNow()) }

func (g *GammaMonitor) biasNow() int {
	snap := g.gexSnapshot()
	if snap == nil {
		return 0
	}
	dir, _ := g.bias(snap, g.FlipBuffer())
	return dir
}

// FlipBuffer is the shed buffer now, in % of spot (FlipBufferPct at the live
// DVOL): a leg is shed below flip × (1 − buffer) and repaired above flip × (1 + buffer).
func (g *GammaMonitor) FlipBuffer() float64 {
	if g.dvol == nil {
		return 0
	}
	return FlipBufferPct(g.bufferSD, g.dvol())
}

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

// SMALevels are the levels behind the SMA trend (detectTrend): the SMAs of
// the daily closes, the last confirmed swing high and low with their days,
// the price the trend is read at (the live spot) and the condition that set
// it. The monitor shows them so a person can see how far a trend change is.
type SMALevels struct {
	SMA9          float64 `json:"sma9"`
	SMA21         float64 `json:"sma21"`
	SwingHigh     float64 `json:"swing_high"`                // 0 = none confirmed
	SwingHighDate string  `json:"swing_high_date,omitempty"` // UTC day, YYYY-MM-DD
	SwingLow      float64 `json:"swing_low"`
	SwingLowDate  string  `json:"swing_low_date,omitempty"`
	Price         float64 `json:"price"`
	Trend         string  `json:"trend"` // bull | bear | neutral
	Reason        string  `json:"reason"`
	// SMA9BandPct is the band around SMA9 (% of SMA9) the price must cross
	// to change side (SMA9Side); 0 = none.
	SMA9BandPct float64 `json:"sma9_band_pct"`
	TrendDir    int     `json:"-"`
	// SMA9Side is the side of SMA9 the trend read the price on (SMA9Side).
	SMA9Side int `json:"-"`
}

// SMA trend reasons (SMATrendOf): which condition set the trend.
const (
	TrendReasonNoSwing   = "no confirmed swing yet"
	TrendReasonBreakout  = "price > swing high"
	TrendReasonBullSMA   = "SMA9 > SMA21 and price > SMA9"
	TrendReasonBreakdown = "price < swing low"
	TrendReasonBearSMA   = "SMA9 < SMA21 and price < SMA9"
	TrendReasonInside    = "inside the swings, SMAs and price not aligned"
)

// SMATrendOf is the SMA trend rule, pure: bull (+1) when price broke above
// the last swing high, or SMA9 > SMA21 with price above SMA9; bear (−1) on
// the mirror conditions; neutral (0) otherwise or without a confirmed swing.
// The reason names the condition that fired. A level of 0 is unknown.
func SMATrendOf(price, swingHigh, swingLow, sma9, sma21 float64) (int, string) {
	return SMATrendOfSide(price, SMA9Side(price, sma9, 0, 0), swingHigh, swingLow, sma9, sma21)
}

// SMA9Side is the side of SMA9 the price is on, with a band of bandPct % of
// SMA9 around it: +1 above sma9 × (1 + band), −1 below sma9 × (1 − band);
// inside the band the side it was on (prev), or plainly above/below SMA9
// when it was on none (prev 0, e.g. startup). 0 without an SMA9, or with the
// price exactly on it and no prev. Pure. Without the band, spot hugging SMA9
// flipped the trend bear ↔ neutral every 40-second cycle (ETH 31 changes on
// 2026-10-20) and the regime-side rule let a put through in a one-cycle
// blip (#243). 0.10 σ cut the flips 83–92 % on a week of minute snapshots,
// stopped every leak and still followed each lasting change within 8–12 min.
func SMA9Side(price, sma9, bandPct float64, prev int) int {
	if sma9 <= 0 {
		return 0
	}
	band := max(bandPct, 0) / 100
	switch {
	case price > sma9*(1+band):
		return 1
	case price < sma9*(1-band):
		return -1
	case prev != 0:
		return prev
	case price > sma9:
		return 1
	case price < sma9:
		return -1
	}
	return 0
}

// SMATrendOfSide is SMATrendOf with the price's side of SMA9 given (side,
// from SMA9Side) instead of read from price itself: a price within the SMA9
// band keeps the side it was on.
func SMATrendOfSide(price float64, side int, swingHigh, swingLow, sma9, sma21 float64) (int, string) {
	if swingHigh == 0 && swingLow == 0 {
		return 0, TrendReasonNoSwing
	}
	// Price must be above SMA9 (not just SMA21) for a bull signal. When price
	// is between the two SMAs (sma21 < price < sma9) that is a neutral
	// consolidation zone, not a confirmed trend. Using "price > sma21" alone
	// triggered bull even mid-consolidation, causing gamma_close to kill
	// freshly-opened call legs every eval cycle.
	switch {
	case swingHigh > 0 && price > swingHigh:
		return 1, TrendReasonBreakout
	case sma21 > 0 && sma9 > sma21 && side > 0:
		return 1, TrendReasonBullSMA
	case swingLow > 0 && price < swingLow:
		return -1, TrendReasonBreakdown
	case sma21 > 0 && sma9 < sma21 && side < 0:
		return -1, TrendReasonBearSMA
	}
	return 0, TrendReasonInside
}

// SMALevels returns the SMA trend's levels now, with the SMA9 band and the
// side of SMA9 the last price (PushPriceAt) left. Reads only.
func (g *GammaMonitor) SMALevels() SMALevels {
	return SMALevelsBand(g.dailyCloses, g.lastTickPrice, g.swingPivotN, g.sma9BandPct(), g.sma9Side)
}

// SMALevelsOf is the SMA trend's levels, pure: the SMAs and swings of the
// completed daily closes (oldest first), the trend read at price (the live
// spot) by SMATrendOf, and its reason. The bot (detectTrend) and cmd/stress
// both call it, so the stress trend cannot drift from the bot's.
func SMALevelsOf(closes []orders.DailyClose, price float64, pivotN int) SMALevels {
	return SMALevelsBand(closes, price, pivotN, 0, 0)
}

// SMALevelsBand is SMALevelsOf with a band of bandPct % around SMA9: the
// price's side of SMA9 is SMA9Side(price, SMA9, bandPct, prevSide), returned
// in SMA9Side for the next call. Pure: the bot's GammaMonitor and cmd/stress
// carry the side from one price to the next.
func SMALevelsBand(closes []orders.DailyClose, price float64, pivotN int, bandPct float64, prevSide int) SMALevels {
	l := SMALevels{SMA9: smaOf(closes, 9), SMA21: smaOf(closes, 21), Price: price, SMA9BandPct: bandPct}
	l.SMA9Side = SMA9Side(price, l.SMA9, bandPct, prevSide)
	var at time.Time
	if l.SwingHigh, at = lastPivot(closes, pivotN, func(other, p float64) bool { return other >= p }); !at.IsZero() {
		l.SwingHighDate = at.UTC().Format(time.DateOnly)
	}
	if l.SwingLow, at = lastPivot(closes, pivotN, func(other, p float64) bool { return other <= p }); !at.IsZero() {
		l.SwingLowDate = at.UTC().Format(time.DateOnly)
	}
	l.TrendDir, l.Reason = SMATrendOfSide(l.Price, l.SMA9Side, l.SwingHigh, l.SwingLow, l.SMA9, l.SMA21)
	l.Trend = trendWord(l.TrendDir)
	return l
}

// detectTrend is the SMA trend now (SMATrendOf on the current levels).
func (g *GammaMonitor) detectTrend() int { return g.SMALevels().TrendDir }

// smaOf returns the simple moving average of the last days closes (of all of
// them when there are fewer).
func smaOf(closes []orders.DailyClose, days int) float64 {
	n := len(closes)
	if n == 0 {
		return 0
	}
	window := min(days, n)
	sum := 0.0
	for _, p := range closes[n-window:] {
		sum += p.Close
	}
	return sum / float64(window)
}

// lastPivot finds the newest close with pivotN closes on each side, none of
// which disqualifies it (disqualifies(other, p) is true): a swing high when
// every neighbour is lower, a swing low when every neighbour is higher.
// Returns 0 if none is found.
func lastPivot(h []orders.DailyClose, n int, disqualifies func(other, p float64) bool) (float64, time.Time) {
	for i := len(h) - n - 1; i >= n; i-- {
		p := h[i].Close
		pivot := true
		for j := i - n; j <= i+n; j++ {
			if j != i && disqualifies(h[j].Close, p) {
				pivot = false
				break
			}
		}
		if pivot {
			return p, h[i].Date
		}
	}
	return 0, time.Time{}
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

// shadowAction is what the direction source not in use would shed now.
func (d GammaDecision) shadowAction() GammaAction {
	if d.Direction == DirectionBias {
		return ResolveGammaAction(d.Regime, d.GammaFlipFound, d.Spot, d.GammaFlip, d.SMADir, d.FlipBufferPct)
	}
	return ResolveBiasAction(d.BiasDir)
}
