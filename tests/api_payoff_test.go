package tests

import (
	"encoding/json"
	"math"
	"os"
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

// hedgedView: two strangles whose coin deltas are a and b, the second one
// expiring later, no mark in either delta.
func hedgedView(now time.Time, a, b float64) strategy.View {
	st := func(id string, dte int, d float64) strategy.StrangleView {
		s := payoffStrangle(id, dte, now.Add(time.Duration(dte)*24*time.Hour), now, payoffSpot)
		// The put (Legs[1]) sets the net: coin delta = −(Δ − mark) × qty over both legs.
		s.Legs[0].Greeks.Delta = s.Legs[0].Mark + 0.2
		s.Legs[1].Greeks.Delta = s.Legs[1].Mark - 0.2 - d
		return s
	}
	return payoffView(st("a", 25, a), st("b", 50, b))
}

// perpHeld is a perpetual held at the index, nothing unrealised, in a band of 0.05.
func perpHeld(usd float64) strategy.HedgeView {
	return strategy.HedgeView{PerpUSD: usd, PerpDeltaCoin: usd / payoffSpot, PerpAvg: payoffSpot, BandCoin: 0.05}
}

// Scenario: a strangle chart includes its hedge share.
func TestPayoff_AStrangleChartIncludesItsHedgeShare(t *testing.T) {
	now := time.Now().UTC()
	const optionsDelta, aDelta = 0.3861, 0.18723
	rep := api.PayoffWithHedge(hedgedView(now, aDelta, optionsDelta-aDelta), perpHeld(-29400), now)
	sp := rep.Strangles[0]
	if sp.StrangleID != "a" || sp.HedgeShareUSD == nil || math.Round(*sp.HedgeShareUSD) != -14257 {
		t.Fatalf("%s: share %v, want 14,257 USD short", sp.StrangleID, sp.HedgeShareUSD)
	}
	share := -*sp.HedgeShareUSD
	for _, pt := range sp.Points {
		want := share * (1/pt.Price - 1/payoffSpot)
		if pt.TodayHedgedPnL == nil || math.Abs(*pt.TodayHedgedPnL-*pt.TodayPnL-want) > 1e-12 {
			t.Fatalf("today hedged at %v: %v, options %v, perpetual %v", pt.Price, pt.TodayHedgedPnL, *pt.TodayPnL, want)
		}
		if pt.ExpiryHedgedPnL == nil || math.Abs(*pt.ExpiryHedgedPnL-pt.ExpiryPnL-want) > 1e-12 {
			t.Fatalf("expiry hedged at %v: %v, options %v, perpetual %v", pt.Price, pt.ExpiryHedgedPnL, pt.ExpiryPnL, want)
		}
	}
	if d := *sp.HedgeShareDeltaCoin; math.Abs(d-(-share/payoffSpot)) > 1e-9 || math.Abs(*sp.NetDeltaWithHedgeCoin-(aDelta+d)) > 1e-9 || math.Abs(*sp.CoinDelta-aDelta) > 1e-12 {
		t.Errorf("delta share %v, coin delta %v, net %v", d, *sp.CoinDelta, *sp.NetDeltaWithHedgeCoin)
	}
	// The hedged curve at expiry crosses zero where the break-evens say.
	if len(sp.HedgedBreakEvens) == 0 {
		t.Fatal("no hedged break-even")
	}
	// The break-evens and the lines stay the options' own.
	plain := api.PayoffOf(hedgedView(now, aDelta, optionsDelta-aDelta), now).Strangles[0]
	if *sp.BreakEvenLow != *plain.BreakEvenLow || *sp.Legs[0].ExitLine != *plain.Legs[0].ExitLine || *sp.Legs[0].StopLevel != *plain.Legs[0].StopLevel {
		t.Error("break-evens, exit lines and stop levels are the options' own")
	}
	// The moves: a short share gains on a fall and loses on a rise.
	if len(sp.HedgeMoves) != len(api.PayoffHedgeMoves) {
		t.Fatalf("moves %+v", sp.HedgeMoves)
	}
	for _, m := range sp.HedgeMoves {
		want := share * (1/m.Spot - 1/payoffSpot)
		if (m.MovePct < 0) != (want > 0) || math.Abs(m.HedgedCoin-m.OptionsCoin-want) > 1e-12 || math.Abs(m.HedgedUSD-m.HedgedCoin*m.Spot) > 1e-6 {
			t.Errorf("move %+v, perpetual %v", m, want)
		}
	}
}

// Metric: the hedged P&L at spot is the strangle's unrealised plus its share
// of the perpetual's unrealised. On testnet the ledger's index-priced average
// counts, never the exchange's fill ~3.5 % away (#664).
func TestPayoff_HedgedAtSpotEqualsBookPlusItsShare(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name    string
		pricing string
		avg     float64 // the one the curve must use
	}{{"exchange", "exchange", 84000}, {"index", "index", 83000}} {
		t.Run(tc.name, func(t *testing.T) {
			h := perpHeld(-20000)
			h.PerpAvg, h.LedgerAvg = 84000, 83000
			h.Benchmark.Pricing = tc.pricing
			rep := api.PayoffWithHedge(hedgedView(now, 0.3, 0.1), h, now)
			var sum float64
			for _, sp := range rep.Strangles {
				frac := *sp.CoinDelta / 0.4
				want := sp.UnrealisedCoin + frac*(-20000)*(1/tc.avg-1/payoffSpot)
				if got := *pointAt(t, sp, payoffSpot).TodayHedgedPnL; math.Abs(got-want) > 1e-12 {
					t.Errorf("%s: hedged at spot %v, want %v", sp.StrangleID, got, want)
				}
				if math.Abs(*sp.UnrealisedWithHedgeCoin-want) > 1e-12 || math.Abs(*sp.UnrealisedWithHedgeUSD-want*payoffSpot) > 1e-6 {
					t.Errorf("%s: now %v / %v USD", sp.StrangleID, *sp.UnrealisedWithHedgeCoin, *sp.UnrealisedWithHedgeUSD)
				}
				sum += *sp.UnrealisedWithHedgeCoin - sp.UnrealisedCoin
			}
			if whole := -20000 * (1/tc.avg - 1/payoffSpot); math.Abs(sum-whole) > 1e-12 {
				t.Errorf("shares of the perpetual's result %v, whole %v", sum, whole)
			}
		})
	}
}

// Scenario: hedge shares sum to the whole perpetual — also when one strangle
// leans against the book, so its share has the other sign.
func TestPayoff_HedgeSharesSumToTheWholePerpetual(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct{ a, b, perp float64 }{{0.3, 0.1, -30000}, {0.5, -0.2, -24000}, {-0.3, -0.1, 30000}} {
		rep := api.PayoffWithHedge(hedgedView(now, tc.a, tc.b), perpHeld(tc.perp), now)
		var usd, delta float64
		for _, sp := range rep.Strangles {
			usd += *sp.HedgeShareUSD
			delta += *sp.HedgeShareDeltaCoin
		}
		if math.Abs(usd-tc.perp) > 1e-6 || math.Abs(delta-tc.perp/payoffSpot) > 1e-9 {
			t.Errorf("%+v: shares %v USD, %v coin", tc, usd, delta)
		}
		if rep.Hedge == nil || !rep.Hedge.Split || math.Abs(rep.Hedge.OptionsDeltaCoin-(tc.a+tc.b)) > 1e-12 {
			t.Errorf("%+v: hedge %+v", tc, rep.Hedge)
		}
	}
}

// noShares fails on any hedged field of a strangle or its points.
func noShares(t *testing.T, rep api.PayoffReport) {
	t.Helper()
	for _, sp := range rep.Strangles {
		if sp.HedgeShareUSD != nil || sp.HedgeShareDeltaCoin != nil || sp.NetDeltaWithHedgeCoin != nil || sp.CoinDelta != nil ||
			sp.UnrealisedWithHedgeCoin != nil || sp.HedgedBreakEvens != nil || sp.HedgeMoves != nil {
			t.Errorf("%s: hedged fields %+v", sp.StrangleID, sp)
		}
		for _, pt := range sp.Points {
			if pt.TodayHedgedPnL != nil || pt.ExpiryHedgedPnL != nil {
				t.Fatalf("%s: hedged point %+v", sp.StrangleID, pt)
			}
		}
	}
}

// Scenario: options' delta near zero — nor when the band or a leg's greeks are unknown.
func TestPayoff_OptionsDeltaNearZeroGivesNoShare(t *testing.T) {
	now := time.Now().UTC()
	band0 := perpHeld(-500)
	band0.BandCoin = 0
	unpriced := hedgedView(now, 0.3, 0.1)
	unpriced.Strangles[1].Legs[0].Greeks = orders.Greeks{}
	for _, tc := range []struct {
		name   string
		v      strategy.View
		h      strategy.HedgeView
		reason string
	}{
		{"inside the band", hedgedView(now, 0.03, 0.01), perpHeld(-500), "inside the hedge band"},
		{"one tick under the band", hedgedView(now, 0.03, 0.0199), perpHeld(-500), "inside the hedge band"},
		{"exactly zero", hedgedView(now, 0.2, -0.2), perpHeld(-500), "inside the hedge band"},
		{"band unknown", hedgedView(now, 0.3, 0.1), band0, "inside the hedge band"},
		{"a leg without greeks", unpriced, perpHeld(-500), "no greeks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep := api.PayoffWithHedge(tc.v, tc.h, now)
			noShares(t, rep)
			if rep.Hedge == nil || rep.Hedge.Split || !strings.Contains(rep.Hedge.NotSplitReason, tc.reason) || rep.Hedge.PerpUSD != -500 {
				t.Errorf("hedge %+v", rep.Hedge)
			}
		})
	}
	// Just past the band the share is stable.
	if rep := api.PayoffWithHedge(hedgedView(now, 0.03, 0.0201), perpHeld(-500), now); !rep.Hedge.Split {
		t.Errorf("|delta| past the band splits: %+v", rep.Hedge)
	}
}

// A one-legged strangle with no IV has no today curve: its hedged today curve
// and moves are null, the hedged expiry curve is still drawn.
func TestPayoff_NoTodayCurveKeepsTheHedgedExpiryCurve(t *testing.T) {
	now := time.Now().UTC()
	v := hedgedView(now, 0.3, 0.1)
	v.Strangles[0].Legs[0].Greeks.IV = 0
	sp := api.PayoffWithHedge(v, perpHeld(-20000), now).Strangles[0]
	if sp.Today || sp.HedgeMoves != nil || sp.HedgeShareUSD == nil {
		t.Fatalf("today %v moves %v share %v", sp.Today, sp.HedgeMoves, sp.HedgeShareUSD)
	}
	pt := pointAt(t, sp, payoffSpot)
	if pt.TodayHedgedPnL != nil || pt.ExpiryHedgedPnL == nil {
		t.Errorf("today %v expiry %v", pt.TodayHedgedPnL, pt.ExpiryHedgedPnL)
	}
}

// The route counts the hedge when it is wired, and a bot without it still answers.
func TestPayoff_RouteReadsTheHedge(t *testing.T) {
	now := time.Now().UTC()
	v := hedgedView(now, 0.3, 0.1)
	srv := api.New(staticView{v}, summaryLogger(), api.WithFuturesHedge(&openHedge{view: perpHeld(-20000)}, "", false))
	_, body := getJSON(t, srv.Handler(), "/api/strangles/payoff")
	if body["hedge"] == nil {
		t.Fatalf("hedge %v", body["hedge"])
	}
	if st := body["strangles"].([]any)[0].(map[string]any); st["hedge_share_usd"] == nil {
		t.Errorf("share %v", st["hedge_share_usd"])
	}
	_, plain := getJSON(t, api.New(staticView{v}, summaryLogger()).Handler(), "/api/strangles/payoff")
	if plain["hedge"] != nil || plain["strangles"].([]any)[0].(map[string]any)["hedge_share_usd"] != nil {
		t.Errorf("no hedge wired: %v", plain["hedge"])
	}
}

// Scenario: no perpetual, no change — the hedge off, or dust under the
// strategy's flat test; the golden pins the options-only answer to the digit.
func TestPayoff_NoPerpetualNoChange(t *testing.T) {
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	v := payoffView(payoffStrangle("s", 25, now.Add(25*24*time.Hour), now, payoffSpot))
	want, _ := json.Marshal(api.PayoffOf(v, now))
	for _, h := range []strategy.HedgeView{{}, {PerpUSD: 0.4, BandCoin: 0.05}} {
		rep := api.PayoffWithHedge(v, h, now)
		noShares(t, rep)
		got, _ := json.Marshal(rep)
		if rep.Hedge != nil || string(got) != string(want) {
			t.Errorf("hedge %+v changed the answer", h)
		}
	}

	var out any
	if err := json.Unmarshal(want, &out); err != nil {
		t.Fatal(err)
	}
	rep := out.(map[string]any)
	st := rep["strangles"].([]any)[0].(map[string]any)
	pts := st["points"].([]any)
	st["points"] = []any{pts[0], pts[len(pts)/2], pts[len(pts)-1]} // a small file
	got, err := json.MarshalIndent(roundFloats(out), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/payoff_no_perpetual.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(golden)) != strings.TrimSpace(string(got)) {
		t.Errorf("/api/strangles/payoff without a perpetual changed:\n%s", got)
	}
}

// roundFloats keeps six decimals: the last bits differ between CPUs (FMA).
func roundFloats(v any) any {
	switch x := v.(type) {
	case float64:
		return math.Round(x*1e6) / 1e6
	case []any:
		for i := range x {
			x[i] = roundFloats(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = roundFloats(x[k])
		}
	}
	return v
}

// Nothing held: an empty list, never null.
func TestPayoff_NothingHeld(t *testing.T) {
	_, body := getJSON(t, api.New(staticView{strategy.View{Underlying: "BTC"}}, summaryLogger()).Handler(), "/api/strangles/payoff")
	if sts, ok := body["strangles"].([]any); !ok || len(sts) != 0 {
		t.Errorf("strangles %v", body["strangles"])
	}
}
