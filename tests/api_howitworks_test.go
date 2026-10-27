package tests

import (
	"math"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// #322: the "How it works" page's strangle in /api/summary.

func howView(now time.Time) strategy.View {
	v := summaryView(now)
	v.DeltaExitThreshold, v.RepairCooldownHours = 0.30, 72
	v.Strangles[0].Legs[0].PremiumReceived = 0.012 // call
	v.Strangles[0].Legs[1].PremiumReceived = 0.018 // put
	return v
}

// Scenario: live numbers — the page's strikes, premiums, expiry and today's
// price come from the bot's nearest strangle.
func TestHowItWorks_LiveNumbers(t *testing.T) {
	now := time.Now().UTC()
	v := howView(now)
	_, body := getJSON(t, api.New(staticView{v}, summaryLogger()).Handler(), "/api/summary")
	if body["delta_exit_threshold"].(float64) != 0.30 || body["repair_cooldown_hours"].(float64) != 72 {
		t.Errorf("config %v %v", body["delta_exit_threshold"], body["repair_cooldown_hours"])
	}
	st, ok := body["strangle"].(map[string]any)
	if !ok {
		t.Fatalf("strangle %v", body["strangle"])
	}
	put, call := st["put"].(map[string]any), st["call"].(map[string]any)
	if put["strike"].(float64) != 95000 || call["strike"].(float64) != 105000 ||
		put["premium_coin"].(float64) != 0.018 || call["premium_coin"].(float64) != 0.012 ||
		math.Abs(st["premium_coin"].(float64)-0.03) > 1e-12 || st["spot"].(float64) != 100000 || st["expiry"] == nil {
		t.Errorf("strangle %v", st)
	}
	if st["exit_lines"] != true || put["exit_line"] == nil || call["exit_line"] == nil {
		t.Errorf("exit lines %v", st)
	}
}

func TestHowItWorks_NearestTwoLeggedStrangle(t *testing.T) {
	now := time.Now().UTC()
	v := howView(now)
	near := now.Add(10 * 24 * time.Hour)
	far := v.Strangles[0]
	oneLegged := strategy.StrangleView{ID: "one", Legs: []strategy.LegView{
		{OptionType: "put", Side: orders.DirectionSell, Strike: 90000, Expiry: now.Add(2 * 24 * time.Hour), Qty: 1},
	}}
	nearer := strategy.StrangleView{ID: "near", Legs: []strategy.LegView{
		{OptionType: "call", Side: orders.DirectionSell, Strike: 110000, Expiry: near, Qty: 2, PremiumReceived: 0.01},
		{OptionType: "put", Side: orders.DirectionSell, Strike: 90000, Expiry: near, Qty: 2, PremiumReceived: 0.01},
	}}
	got := api.HowItWorksOf([]strategy.StrangleView{far, oneLegged, nearer}, 100000, 50, 0.3, now)
	if got == nil || got.StrangleID != "near" || !got.Expiry.Equal(near) || got.Put.Qty != 2 {
		t.Fatalf("picked %+v", got)
	}
	if api.HowItWorksOf([]strategy.StrangleView{oneLegged}, 100000, 50, 0.3, now) != nil {
		t.Error("a one-legged strangle is not a strangle to explain")
	}
}

// Scenario: nothing held — strangle is null (the page shows an example).
func TestHowItWorks_NothingHeld(t *testing.T) {
	_, body := getJSON(t, api.New(staticView{strategy.View{Underlying: "BTC"}}, summaryLogger()).Handler(), "/api/summary")
	if v, ok := body["strangle"]; !ok || v != nil {
		t.Errorf("strangle %v", v)
	}
}

// The exit line is the price where the leg's BS |Δ| equals the threshold.
func TestExitLinePrice_DeltaEqualsThreshold(t *testing.T) {
	delta := func(optType string, s, k, years, sigma float64) float64 {
		sq := sigma * math.Sqrt(years)
		d1 := (math.Log(s/k) + 0.5*sigma*sigma*years) / sq
		n := 0.5 * math.Erfc(-d1/math.Sqrt2)
		if optType == "put" {
			return n - 1
		}
		return n
	}
	for _, c := range []struct {
		typ               string
		strike, years, iv float64
		threshold         float64
		below, above      bool // exit line below / above the strike
	}{
		{"call", 105000, 30.0 / 365, 0.5, 0.30, true, false},
		{"put", 95000, 30.0 / 365, 0.5, 0.30, false, true},
		{"call", 4000, 7.0 / 365, 0.8, 0.45, true, false},
		{"put", 3000, 60.0 / 365, 0.7, 0.20, false, true},
	} {
		s := api.ExitLinePrice(c.typ, c.strike, c.years, c.iv, c.threshold)
		if d := math.Abs(delta(c.typ, s, c.strike, c.years, c.iv)); math.Abs(d-c.threshold) > 1e-9 {
			t.Errorf("%s %v: |Δ| at %.2f = %.6f, want %.2f", c.typ, c.strike, s, d, c.threshold)
		}
		// An exit line under 0.5 sits before the strike: below a call's, above a put's.
		if c.below && s >= c.strike || c.above && s <= c.strike {
			t.Errorf("%s %v: exit line %.2f on the wrong side of the strike", c.typ, c.strike, s)
		}
	}
	// Pinned value: call 105,000, 30 days, IV 50 %, |Δ| 0.30.
	if s := api.ExitLinePrice("call", 105000, 30.0/365, 0.5, 0.30); math.Abs(s-96401) > 5 {
		t.Errorf("pinned call exit line %.2f", s)
	}
}

func TestHowItWorks_ExitLinesNeedData(t *testing.T) {
	now := time.Now().UTC()
	v := howView(now)
	for i := range v.Strangles[0].Legs {
		v.Strangles[0].Legs[i].Greeks.IV = 0
	}
	if hw := api.HowItWorksOf(v.Strangles, 100000, 50, 0.3, now); !hw.ExitLines || hw.Put.ExitLine == nil {
		t.Errorf("DVOL fallback: %+v", hw)
	}
	cases := map[string]*api.HowItWorks{
		"no IV":        api.HowItWorksOf(v.Strangles, 100000, 0, 0.3, now),
		"no threshold": api.HowItWorksOf(howView(now).Strangles, 100000, 50, 0, now),
		"expired":      api.HowItWorksOf(howView(now).Strangles, 100000, 50, 0.3, now.Add(31*24*time.Hour)),
	}
	for name, hw := range cases {
		if hw == nil || hw.ExitLines || hw.ExitLinesReason == "" || hw.Put.ExitLine != nil || hw.Call.ExitLine != nil {
			t.Errorf("%s: %+v", name, hw)
		}
	}
}

// Break-evens of a coin-settled strangle: the payoff there is exactly the payment.
func TestBreakEvens_CoinSettledPayoff(t *testing.T) {
	low, high := api.BreakEvens(95000, 1, 105000, 1, 0.03)
	if low == nil || high == nil {
		t.Fatal("break-evens missing")
	}
	if put := (95000 - *low) / *low; math.Abs(put-0.03) > 1e-12 {
		t.Errorf("put pays %.6f coin at %.2f, want 0.03", put, *low)
	}
	if call := (*high - 105000) / *high; math.Abs(call-0.03) > 1e-12 {
		t.Errorf("call pays %.6f coin at %.2f, want 0.03", call, *high)
	}
	if math.Abs(*low-92233.0097) > 1e-3 || math.Abs(*high-108247.4227) > 1e-3 {
		t.Errorf("break-evens %.4f %.4f", *low, *high)
	}
	// A call smaller than the payment can never cost all of it: no upper break-even.
	if _, high := api.BreakEvens(95000, 1, 105000, 0.02, 0.03); high != nil {
		t.Errorf("high %v", *high)
	}
}
