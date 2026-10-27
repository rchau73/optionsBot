package tests

import (
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #391: the payoff chart of each strangle, /api/strangles/payoff.

const payoffSpot = 82400.0

// modelLeg is a short leg whose mark is the model's value at spot (no fees),
// so the book's unrealised P&L is the model's.
func modelLeg(optType string, strike, qty, premium, iv, delta float64, expiry, now time.Time, spot float64) strategy.LegView {
	years := expiry.Sub(now).Hours() / 24 / 365
	mark := api.BlackScholesUSD(optType, spot, strike, years, iv) / spot
	return strategy.LegView{
		PositionID: optType + "-" + expiry.Format("02Jan"), Instrument: "BTC-" + optType, OptionType: optType,
		Side: orders.DirectionSell, Strike: strike, Expiry: expiry, Qty: qty, Mark: mark,
		PremiumReceived: premium, UnrealisedPnL: premium - mark*qty, StopLossMark: premium * 3 / qty,
		Greeks: orders.Greeks{IV: iv, Delta: delta},
	}
}

// payoffStrangle is a BTC strangle on the dte slot: put 75,000, call 90,000.
func payoffStrangle(id string, dte int, expiry, now time.Time, spot float64) strategy.StrangleView {
	return strategy.StrangleView{ID: id, Slot: orders.SlotRef{DTE: dte, Delta: 0.16}, Legs: []strategy.LegView{
		modelLeg("call", 90000, 1, 0.05, 0.5, 0.2, expiry, now, spot),
		modelLeg("put", 75000, 1, 0.05, 0.5, -0.15, expiry, now, spot),
	}}
}

func payoffView(strangles ...strategy.StrangleView) strategy.View {
	v := strategy.View{Underlying: "BTC", DeltaExitThreshold: 0.30, StopLossMultiple: 2, Strangles: strangles}
	v.Market.Spot, v.Market.DVOL = payoffSpot, 50
	return v
}

// pointAt is the curve's point at price p (the spot, a strike or a break-even
// is always one of them).
func pointAt(t *testing.T, sp api.StranglePayoff, p float64) api.PayoffPoint {
	t.Helper()
	for _, pt := range sp.Points {
		if math.Abs(pt.Price-p) < 1e-6 {
			return pt
		}
	}
	t.Fatalf("no point at %v", p)
	return api.PayoffPoint{}
}

// maxGap is the largest |today − expiry| over the curve.
func maxGap(sp api.StranglePayoff) float64 {
	var gap float64
	for _, pt := range sp.Points {
		gap = math.Max(gap, math.Abs(*pt.TodayPnL-pt.ExpiryPnL))
	}
	return gap
}

// Scenario: one chart per strangle, by coin and DTE.
func TestPayoff_OneChartPerStrangleByDTE(t *testing.T) {
	now := time.Now().UTC()
	v := payoffView(
		payoffStrangle("fifty", 50, now.Add(50*24*time.Hour), now, payoffSpot),
		payoffStrangle("twentyfive", 25, now.Add(25*24*time.Hour), now, payoffSpot))
	_, body := getJSON(t, api.New(staticView{v}, summaryLogger()).Handler(), "/api/strangles/payoff")
	sts, ok := body["strangles"].([]any)
	if !ok || len(sts) != 2 {
		t.Fatalf("strangles %v", body["strangles"])
	}
	if body["model"] != api.PayoffModel || body["underlying"] != "BTC" {
		t.Errorf("model %v underlying %v", body["model"], body["underlying"])
	}
	for i, want := range []string{"twentyfive", "fifty"} {
		st := sts[i].(map[string]any)
		if st["strangle_id"] != want {
			t.Errorf("chart %d is %v, want %s (ordered by DTE)", i, st["strangle_id"], want)
		}
		pts := st["points"].([]any)
		if len(pts) < api.PayoffGridPoints || pts[0].(map[string]any)["today_pnl"] == nil {
			t.Errorf("%s: %d points, first %v", want, len(pts), pts[0])
		}
		if st["break_even_low"] == nil || st["break_even_high"] == nil || st["today"] != true {
			t.Errorf("%s: break-evens %v %v today %v", want, st["break_even_low"], st["break_even_high"], st["today"])
		}
		for _, l := range st["legs"].([]any) {
			leg := l.(map[string]any)
			if leg["exit_line"] == nil || leg["stop_level"] == nil {
				t.Errorf("%s %s: exit %v stop %v", want, leg["option_type"], leg["exit_line"], leg["stop_level"])
			}
		}
	}
	// The axis covers ±3 daily σ beyond the outer break-evens, spot and strikes.
	sp := api.PayoffOf(v, now).Strangles[0]
	first, last := sp.Points[0].Price, sp.Points[len(sp.Points)-1].Price
	if first >= *sp.BreakEvenLow || last <= *sp.BreakEvenHigh || first >= 75000 || last <= 90000 {
		t.Errorf("axis %v–%v, break-evens %v–%v", first, last, *sp.BreakEvenLow, *sp.BreakEvenHigh)
	}
}

// The today curve at spot equals the book's unrealised P&L — also when the
// mark is not the model's (the curve is anchored on the book).
func TestPayoff_TodayAtSpotEqualsBook(t *testing.T) {
	now := time.Now().UTC()
	st := payoffStrangle("s", 25, now.Add(25*24*time.Hour), now, payoffSpot)
	st.Legs[0].Mark += 0.002 // the exchange's mark above the model
	st.Legs[0].UnrealisedPnL = st.Legs[0].PremiumReceived - st.Legs[0].Mark - 0.0001
	sp := api.PayoffOf(payoffView(st), now).Strangles[0]
	book := st.Legs[0].UnrealisedPnL + st.Legs[1].UnrealisedPnL
	if got := *pointAt(t, sp, payoffSpot).TodayPnL; math.Abs(got-book) > 1e-12 {
		t.Errorf("today at spot %v, book %v", got, book)
	}
	if math.Abs(sp.UnrealisedCoin-book) > 1e-12 || math.Abs(sp.UnrealisedUSD-book*payoffSpot) > 1e-6 {
		t.Errorf("unrealised %v / %v USD, book %v", sp.UnrealisedCoin, sp.UnrealisedUSD, book)
	}
}

// Scenario: the earned premium fills as time passes.
func TestPayoff_EarnedPremiumFillsAsTimePasses(t *testing.T) {
	now := time.Now().UTC()
	st := payoffStrangle("s", 25, now.Add(25*24*time.Hour), now, payoffSpot)
	st.Legs[0].PremiumReceived, st.Legs[1].PremiumReceived = 0.0047, 0.0047
	st.Legs[0].UnrealisedPnL, st.Legs[1].UnrealisedPnL = 0.0029, 0.0029
	sp := api.PayoffOf(payoffView(st), now).Strangles[0]
	if math.Abs(sp.PremiumCoin-0.0094) > 1e-12 || sp.EarnedPct == nil || math.Round(*sp.EarnedPct) != 62 {
		t.Errorf("premium %v earned %v", sp.PremiumCoin, sp.EarnedPct)
	}
	if *pointAt(t, sp, payoffSpot).TodayPnL <= 0 {
		t.Error("a strangle with +0.0058 unrealised sits in the gain area")
	}

	// A later day with spot unchanged: the today curve is closer to the expiry curve.
	expiry := now.Add(25 * 24 * time.Hour)
	gapAt := func(at time.Time) float64 {
		v := payoffView(payoffStrangle("s", 25, expiry, at, payoffSpot))
		return maxGap(api.PayoffOf(v, at).Strangles[0])
	}
	early, later, last := gapAt(now), gapAt(now.Add(15*24*time.Hour)), gapAt(expiry.Add(-time.Minute))
	if !(later < early) || !(last < later) {
		t.Errorf("gap to the expiry curve %v → %v → %v: should shrink", early, later, last)
	}
	if last > 1e-3 {
		t.Errorf("one minute before expiry the curves differ by %v coin", last)
	}
}

// The break-evens make the expiry payoff zero.
func TestPayoff_BreakEvensMakeExpiryPayoffZero(t *testing.T) {
	now := time.Now().UTC()
	sp := api.PayoffOf(payoffView(payoffStrangle("s", 25, now.Add(25*24*time.Hour), now, payoffSpot)), now).Strangles[0]
	for _, be := range []*float64{sp.BreakEvenLow, sp.BreakEvenHigh} {
		if be == nil {
			t.Fatal("missing break-even")
		}
		if got := pointAt(t, sp, *be).ExpiryPnL; math.Abs(got) > 1e-12 {
			t.Errorf("expiry payoff at break-even %v = %v", *be, got)
		}
	}
	// Between the strikes the strangle keeps all the premium.
	if got := pointAt(t, sp, payoffSpot).ExpiryPnL; math.Abs(got-sp.PremiumCoin) > 1e-12 {
		t.Errorf("expiry payoff at spot %v, premium %v", got, sp.PremiumCoin)
	}
}

// The exit lines are How it works' (ExitLinePrice), the stop levels where the
// leg's mark reaches stop_loss_mark on the same model.
func TestPayoff_ExitLinesMatchHowItWorksAndStopLevels(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(25 * 24 * time.Hour)
	v := payoffView(payoffStrangle("s", 25, expiry, now, payoffSpot))
	sp := api.PayoffOf(v, now).Strangles[0]
	hw := api.HowItWorksOf(v.Strangles, payoffSpot, v.Market.DVOL, v.DeltaExitThreshold, now)
	put, call := sp.Legs[0], sp.Legs[1]
	if put.OptionType != "put" || call.OptionType != "call" {
		t.Fatalf("legs %v %v: put first", put.OptionType, call.OptionType)
	}
	if put.ExitLine == nil || call.ExitLine == nil || *put.ExitLine != *hw.Put.ExitLine || *call.ExitLine != *hw.Call.ExitLine {
		t.Errorf("exit lines %v %v, How it works %v %v", put.ExitLine, call.ExitLine, *hw.Put.ExitLine, *hw.Call.ExitLine)
	}
	years := expiry.Sub(now).Hours() / 24 / 365
	for _, l := range []api.PayoffLeg{put, call} {
		if l.StopLevel == nil {
			t.Fatalf("%s: no stop level", l.OptionType)
		}
		mark := api.BlackScholesUSD(l.OptionType, *l.StopLevel, l.Strike, years, l.IV) / *l.StopLevel
		if math.Abs(mark-l.StopLossMark) > 1e-6 {
			t.Errorf("%s: model mark at the stop level %v = %v, stop mark %v", l.OptionType, *l.StopLevel, mark, l.StopLossMark)
		}
	}
	if !(*put.StopLevel < *put.ExitLine && *call.StopLevel > *call.ExitLine) {
		t.Errorf("stops %v/%v should lie beyond the exit lines %v/%v", *put.StopLevel, *call.StopLevel, *put.ExitLine, *call.ExitLine)
	}
}

// A missing IV gives a reason, not zeros.
func TestPayoff_MissingIVGivesReason(t *testing.T) {
	now := time.Now().UTC()
	st := payoffStrangle("s", 25, now.Add(25*24*time.Hour), now, payoffSpot)
	st.Legs[0].Greeks.IV = 0
	v := payoffView(st)
	_, body := getJSON(t, api.New(staticView{v}, summaryLogger()).Handler(), "/api/strangles/payoff")
	js := body["strangles"].([]any)[0].(map[string]any)
	if js["today"] != false || !strings.Contains(js["today_reason"].(string), "no implied volatility for BTC-call") {
		t.Errorf("today %v reason %v", js["today"], js["today_reason"])
	}
	for _, p := range js["points"].([]any) {
		if p.(map[string]any)["today_pnl"] != nil {
			t.Fatalf("today_pnl %v without an IV: must be null", p)
		}
	}
	for _, l := range js["legs"].([]any) {
		if leg := l.(map[string]any); leg["exit_line"] != nil || leg["stop_level"] != nil {
			t.Errorf("lines without an IV: %v", leg)
		}
	}
	sp := api.PayoffOf(v, now).Strangles[0]
	if pointAt(t, sp, payoffSpot).ExpiryPnL != sp.PremiumCoin {
		t.Error("the expiry curve does not need an IV")
	}
}

// Scenario: a leg in loss — spot beyond the put's break-even.
func TestPayoff_LegInLoss(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(25 * 24 * time.Hour)
	spot := 68000.0
	st := payoffStrangle("s", 25, expiry, now, spot)
	v := payoffView(st)
	v.Market.Spot = spot
	sp := api.PayoffOf(v, now).Strangles[0]
	if spot >= *sp.BreakEvenLow {
		t.Fatalf("fixture: spot %v not beyond the put's break-even %v", spot, *sp.BreakEvenLow)
	}
	today := *pointAt(t, sp, spot).TodayPnL
	if today >= 0 || math.Abs(today-sp.UnrealisedCoin) > 1e-12 || sp.UnrealisedUSD >= 0 {
		t.Errorf("today at spot %v, unrealised %v / %v USD: a loss", today, sp.UnrealisedCoin, sp.UnrealisedUSD)
	}
	if sp.EarnedPct == nil || *sp.EarnedPct >= 0 {
		t.Errorf("earned %v", sp.EarnedPct)
	}
}

// Scenario: delta exposure — the strangle's net delta in coin and USD per
// 1 %, each leg's, and the bot's for comparison.
func TestPayoff_DeltaExposure(t *testing.T) {
	now := time.Now().UTC()
	v := payoffView(payoffStrangle("s", 25, now.Add(25*24*time.Hour), now, payoffSpot))
	v.Greeks.NetDelta = -0.4
	rep := api.PayoffOf(v, now)
	sp := rep.Strangles[0]
	// Short call Δ 0.2 → −0.2 coin; short put Δ −0.15 → +0.15 coin.
	if math.Abs(sp.NetDeltaCoin-(-0.05)) > 1e-12 || math.Abs(sp.NetDeltaUSDPer1Pct-(-0.05*payoffSpot/100)) > 1e-9 {
		t.Errorf("net delta %v coin, %v USD per 1 %%", sp.NetDeltaCoin, sp.NetDeltaUSDPer1Pct)
	}
	if sp.Legs[0].PositionDelta != 0.15 || sp.Legs[1].PositionDelta != -0.2 || math.Abs(sp.Legs[1].PositionDeltaUSD1-(-164.8)) > 1e-9 {
		t.Errorf("leg deltas %+v", sp.Legs)
	}
	if rep.BotNetDelta != -0.4 || math.Abs(rep.BotNetDeltaUSDPer1Pct-(-329.6)) > 1e-9 {
		t.Errorf("bot net delta %v / %v", rep.BotNetDelta, rep.BotNetDeltaUSDPer1Pct)
	}
}

// One-legged strangle: the missing side and why, from held_legs.
func TestPayoff_OneLeggedNamesTheMissingSide(t *testing.T) {
	now := time.Now().UTC()
	st := payoffStrangle("s", 25, now.Add(25*24*time.Hour), now, payoffSpot)
	st.Legs = st.Legs[1:] // the put only
	v := payoffView(st)
	v.HeldLegs = []strategy.HeldLegView{{StrangleID: "s", Missing: "call", Rule: "stopped", Reason: "stopped out; cooldown until 30 Oct"}}
	sp := api.PayoffOf(v, now).Strangles[0]
	if len(sp.Missing) != 1 || sp.Missing[0].OptionType != "call" || sp.Missing[0].Rule != "stopped" || sp.Missing[0].Reason != "stopped out; cooldown until 30 Oct" {
		t.Errorf("missing %+v", sp.Missing)
	}
	if sp.BreakEvenLow == nil || sp.BreakEvenHigh != nil {
		t.Errorf("put only: break-evens %v %v", sp.BreakEvenLow, sp.BreakEvenHigh)
	}
	// No hold reported: unknown, with the reason.
	v.HeldLegs = nil
	if m := api.PayoffOf(v, now).Strangles[0].Missing; len(m) != 1 || !strings.HasPrefix(m[0].Reason, "unknown:") {
		t.Errorf("missing without a hold %+v", m)
	}
}

// Nothing held: an empty list, never null.
func TestPayoff_NothingHeld(t *testing.T) {
	_, body := getJSON(t, api.New(staticView{strategy.View{Underlying: "BTC"}}, summaryLogger()).Handler(), "/api/strangles/payoff")
	if sts, ok := body["strangles"].([]any); !ok || len(sts) != 0 {
		t.Errorf("strangles %v", body["strangles"])
	}
}
