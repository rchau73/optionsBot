package tests

import (
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// summaryView: short 1 BTC call and 1 BTC put near the money, 30 days out,
// spot 100,000, DVOL 50, equity 2 BTC.
func summaryView(now time.Time) strategy.View {
	exp := now.Add(30 * 24 * time.Hour)
	return strategy.View{
		AsOf: now, StrategyID: "short-strangle", Underlying: "BTC",
		Market:  orders.MarketSnapshot{Spot: 100000, DVOL: 50},
		Account: strategy.AccountView{Equity: 2},
		Strangles: []strategy.StrangleView{{ID: "st-1", Slot: orders.SlotRef{DTE: 30, Delta: 0.3}, Legs: []strategy.LegView{
			{Instrument: "BTC-X-105000-C", OptionType: "call", Side: orders.DirectionSell, Strike: 105000, Expiry: exp, Qty: 1, Greeks: orders.Greeks{IV: 0.5}},
			{Instrument: "BTC-X-95000-P", OptionType: "put", Side: orders.DirectionSell, Strike: 95000, Expiry: exp, Qty: 1, Greeks: orders.Greeks{IV: 0.5}},
		}}},
		PnL: []strategy.PnLView{{Unrealised: 0.01, Total: 0.01}},
	}
}

func summaryLogger(trades ...orders.Trade) *orders.Logger {
	l := orders.NewWriterLogger(io.Discard, 0.05)
	l.Restore(orders.Replay{Trades: trades})
	return l
}

// Scenario: the four answers.
func TestAPISummary_TheFourAnswers(t *testing.T) {
	now := time.Now().UTC()
	journal := summaryLogger(
		orders.Trade{Seq: 1, At: now.AddDate(0, -2, 0), Kind: "close", OptionType: "call", Reason: "roi_target", PnL: 0.01, PnLUSD: 1000},
		orders.Trade{Seq: 2, At: now, Kind: "open", OptionType: "put", Reason: orders.TriggerRepair},
	)
	code, body := getJSON(t, api.New(staticView{summaryView(now)}, journal).Handler(), "/api/summary")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	holding := body["holding"].(map[string]any)
	if holding["strangles"].(float64) != 1 || holding["legs"].(float64) != 2 || holding["coin"] != "BTC" ||
		holding["notional_coin"].(float64) != 2 || holding["notional_usd"].(float64) != 200000 || holding["nearest_expiry"] == nil {
		t.Errorf("holding %v", holding)
	}
	income := body["income"].(map[string]any)
	months := income["months"].([]any)
	if len(months) != 6 || income["coin"] != "BTC" || income["this_month"] == nil {
		t.Fatalf("income %v", income)
	}
	if m := months[3].(map[string]any); m["realised_usd"].(float64) != 1000 || m["realised_coin"].(float64) != 0.01 {
		t.Errorf("two months ago: %v", m)
	}
	if income["unrealised_usd"].(float64) != 1000 {
		t.Errorf("unrealised %v", income["unrealised_usd"])
	}
	wc := body["worst_case"].(map[string]any)
	for _, side := range []string{"down", "up"} {
		s := wc[side].(map[string]any)
		for _, k := range []string{"loss_coin", "loss_usd", "pct_of_equity", "spot"} {
			if _, ok := s[k]; !ok {
				t.Errorf("worst_case.%s.%s missing", side, k)
			}
		}
	}
	if wc["coin"] != "BTC" || wc["available"] != true {
		t.Errorf("worst case %v", wc)
	}
	la := body["last_action"].(map[string]any)
	if la["kind"] != "open" || la["rule"] != "repair" || !strings.Contains(la["reason"].(string), "complete its strangle") {
		t.Errorf("last action %v", la)
	}
}

// Scenario (#320): range bar — the nearest strangle's put and call strikes.
func TestAPISummary_NearestStranglesStrikes(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	near := now.Add(7 * 24 * time.Hour)
	// A second strangle on a nearer expiry, two puts there (the highest is
	// the band's floor) and a long call that is not part of the band.
	v.Strangles = append(v.Strangles, strategy.StrangleView{ID: "st-2", Legs: []strategy.LegView{
		{OptionType: "call", Side: orders.DirectionSell, Strike: 110000, Expiry: near, Qty: 1},
		{OptionType: "call", Side: orders.DirectionBuy, Strike: 101000, Expiry: near, Qty: 1},
		{OptionType: "put", Side: orders.DirectionSell, Strike: 90000, Expiry: near, Qty: 1},
		{OptionType: "put", Side: orders.DirectionSell, Strike: 92000, Expiry: near, Qty: 1},
	}})
	n := api.BuildSummary(v, nil, now).Holding.Nearest
	if n == nil || !n.Expiry.Equal(near) || n.PutStrike == nil || *n.PutStrike != 92000 || n.CallStrike == nil || *n.CallStrike != 110000 {
		t.Fatalf("nearest %+v", n)
	}

	// One-legged: the missing side is null.
	v = summaryView(now)
	v.Strangles[0].Legs = v.Strangles[0].Legs[:1] // call only
	n = api.BuildSummary(v, nil, now).Holding.Nearest
	if n == nil || n.PutStrike != nil || n.CallStrike == nil || *n.CallStrike != 105000 {
		t.Fatalf("one-legged nearest %+v", n)
	}

	// Nothing held: null.
	v.Strangles = nil
	if n := api.BuildSummary(v, nil, now).Holding.Nearest; n != nil {
		t.Fatalf("flat book nearest %+v", n)
	}
}

// The last action carries the coin's spot at the action (the phone card's
// "the coin's move since"), omitted when the journal has none.
func TestAPISummary_LastActionSpot(t *testing.T) {
	now := time.Now().UTC()
	la := api.BuildSummary(summaryView(now), []orders.Trade{{Seq: 1, At: now, Kind: "close", OptionType: "put", Reason: "roi_target", Spot: 98000}}, now).LastAction
	if la == nil || la.Spot == nil || *la.Spot != 98000 {
		t.Fatalf("last action %+v", la)
	}
	la = api.BuildSummary(summaryView(now), []orders.Trade{{Seq: 1, At: now, Kind: "open", OptionType: "put", Reason: "entry"}}, now).LastAction
	if la == nil || la.Spot != nil {
		t.Fatalf("last action without spot %+v", la)
	}
}

// Scenario: worst case is a model, in money.
func TestAPISummary_WorstCaseIsAModelInMoney(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	wc := api.WorstCaseOf(v.Strangles, 100000, 50, 2, now)
	if !wc.Available || !strings.Contains(wc.Model, "model") {
		t.Fatalf("worst case %+v", wc)
	}
	move := 3 * 0.50 / math.Sqrt(365) // −3 daily σ of DVOL 50 ≈ 7.85 %
	down := 100000 * (1 - move)
	t0, t1 := 30.0/365, 29.0/365
	want := api.BlackScholesUSD("call", down, 105000, t1, 0.5) - api.BlackScholesUSD("call", 100000, 105000, t0, 0.5) +
		api.BlackScholesUSD("put", down, 95000, t1, 0.5) - api.BlackScholesUSD("put", 100000, 95000, t0, 0.5)
	if math.Abs(wc.Down.Spot-down) > 1e-6 || math.Abs(wc.Down.LossUSD-want) > 1e-6 {
		t.Fatalf("down: spot %.2f loss %.2f, want %.2f / %.2f", wc.Down.Spot, wc.Down.LossUSD, down, want)
	}
	// A ±7.85 % day on a short strangle 5 % out loses money both ways (the
	// short call's gain offsets part of the put's loss): ~1,000 USD down.
	if wc.Down.LossUSD < 500 || wc.Down.LossUSD > 3000 || wc.Up.LossUSD <= 0 {
		t.Errorf("down %.2f, up %.2f USD", wc.Down.LossUSD, wc.Up.LossUSD)
	}
	if math.Abs(wc.Down.LossCoin-want/down) > 1e-12 || math.Abs(*wc.Down.PctOfEquity-want/down/2*100) > 1e-9 {
		t.Errorf("coin %.6f pct %.4f", wc.Down.LossCoin, *wc.Down.PctOfEquity)
	}
	if math.Abs(wc.MovePct-move*100) > 1e-9 {
		t.Errorf("move %.4f %%", wc.MovePct)
	}
}

// #318: five one-day moves, signed results; ±3σ are the down/up pair, 0σ is
// one day of theta (a gain for a short book).
func TestAPISummary_WorstCaseScenarios(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	wc := api.WorstCaseOf(v.Strangles, 100000, 50, 2, now)
	if len(wc.Scenarios) != 5 {
		t.Fatalf("scenarios %+v", wc.Scenarios)
	}
	sigma := 0.50 / math.Sqrt(365)
	for i, want := range []float64{-3, -2, 0, 2, 3} {
		sc := wc.Scenarios[i]
		if sc.Sigmas != want || math.Abs(sc.MovePct-want*sigma*100) > 1e-9 || math.Abs(sc.Spot-100000*(1+want*sigma)) > 1e-6 {
			t.Errorf("row %d: %+v", i, sc)
		}
		if sc.PctOfEquity == nil || math.Abs(*sc.PctOfEquity-sc.ResultCoin/2*100) > 1e-9 {
			t.Errorf("row %d pct %v", i, sc.PctOfEquity)
		}
	}
	first, last := wc.Scenarios[0], wc.Scenarios[4]
	if first.ResultUSD != -wc.Down.LossUSD || first.ResultCoin != -wc.Down.LossCoin || *first.PctOfEquity != -*wc.Down.PctOfEquity {
		t.Errorf("−3σ %+v vs down %+v", first, wc.Down)
	}
	if last.ResultUSD != -wc.Up.LossUSD || last.ResultCoin != -wc.Up.LossCoin {
		t.Errorf("+3σ %+v vs up %+v", last, wc.Up)
	}
	// Quiet day: one day of time decay on 2 short options, 30 days out.
	quiet := wc.Scenarios[2]
	t0, t1 := 30.0/365, 29.0/365
	theta := api.BlackScholesUSD("call", 100000, 105000, t0, 0.5) - api.BlackScholesUSD("call", 100000, 105000, t1, 0.5) +
		api.BlackScholesUSD("put", 100000, 95000, t0, 0.5) - api.BlackScholesUSD("put", 100000, 95000, t1, 0.5)
	if quiet.ResultUSD <= 0 || math.Abs(quiet.ResultUSD-theta) > 1e-6 || quiet.Spot != 100000 {
		t.Errorf("quiet day %+v, want theta %.2f USD", quiet, theta)
	}
	// Big days cost more than the quiet day pays.
	if first.ResultUSD >= 0 || last.ResultUSD >= 0 || wc.Scenarios[1].ResultUSD >= quiet.ResultUSD {
		t.Errorf("scenarios %+v", wc.Scenarios)
	}
	// Without equity the % is null, never 0.
	for _, sc := range api.WorstCaseOf(v.Strangles, 100000, 50, 0, now).Scenarios {
		if sc.PctOfEquity != nil {
			t.Errorf("pct without equity: %v", *sc.PctOfEquity)
		}
	}
	// Unknown DVOL: no rows (the page shows no zero results).
	if sc := api.WorstCaseOf(v.Strangles, 100000, 0, 2, now).Scenarios; sc == nil || len(sc) != 0 {
		t.Errorf("no DVOL: %v", sc)
	}
}

func TestAPISummary_EnvironmentAndStopMultiple(t *testing.T) {
	v := summaryView(time.Now().UTC())
	v.Environment, v.StopLossMultiple = "testnet", 2
	_, body := getJSON(t, api.New(staticView{v}, summaryLogger()).Handler(), "/api/summary")
	if body["environment"] != "testnet" || body["stop_loss_multiple"].(float64) != 2 {
		t.Errorf("summary %v %v", body["environment"], body["stop_loss_multiple"])
	}
	if n := len(body["worst_case"].(map[string]any)["scenarios"].([]any)); n != 5 {
		t.Errorf("scenarios %d", n)
	}
}

func TestAPISummary_WorstCaseLongLegGainsWhatAShortLoses(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	short := api.WorstCaseOf(v.Strangles, 100000, 50, 0, now)
	for i := range v.Strangles[0].Legs {
		v.Strangles[0].Legs[i].Side = orders.DirectionBuy
	}
	long := api.WorstCaseOf(v.Strangles, 100000, 50, 0, now)
	if math.Abs(short.Down.LossUSD+long.Down.LossUSD) > 1e-9 || short.Down.PctOfEquity != nil {
		t.Errorf("short %.2f long %.2f pct %v", short.Down.LossUSD, long.Down.LossUSD, short.Down.PctOfEquity)
	}
}

func TestAPISummary_WorstCaseFallsBackToDVOLAndNeedsData(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	withIV := api.WorstCaseOf(v.Strangles, 100000, 50, 2, now)
	for i := range v.Strangles[0].Legs {
		v.Strangles[0].Legs[i].Greeks.IV = 0 // no ticker IV: DVOL 50 % is used
	}
	if got := api.WorstCaseOf(v.Strangles, 100000, 50, 2, now); math.Abs(got.Down.LossUSD-withIV.Down.LossUSD) > 1e-9 {
		t.Errorf("DVOL fallback %.2f vs %.2f", got.Down.LossUSD, withIV.Down.LossUSD)
	}
	if wc := api.WorstCaseOf(v.Strangles, 100000, 0, 2, now); wc.Available || wc.Reason == "" {
		t.Errorf("no DVOL: %+v", wc)
	}
	if wc := api.WorstCaseOf(v.Strangles, 0, 50, 2, now); wc.Available || wc.Reason == "" {
		t.Errorf("no spot: %+v", wc)
	}
	if wc := api.WorstCaseOf(nil, 100000, 50, 2, now); !wc.Available || wc.Down.LossUSD != 0 {
		t.Errorf("empty book: %+v", wc)
	}
}

func TestBlackScholesUSD_KnownValuesAndParity(t *testing.T) {
	if c := api.BlackScholesUSD("call", 100, 100, 1, 0.2); math.Abs(c-7.9656) > 1e-3 {
		t.Errorf("ATM call %.4f, want 7.9656", c)
	}
	c, p := api.BlackScholesUSD("call", 100, 90, 0.5, 0.6), api.BlackScholesUSD("put", 100, 90, 0.5, 0.6)
	if math.Abs(c-p-10) > 1e-9 { // zero rate: C − P = S − K
		t.Errorf("parity: C %.4f P %.4f", c, p)
	}
	if v := api.BlackScholesUSD("put", 80, 90, 0, 0.6); v != 10 {
		t.Errorf("expired put %.2f, want intrinsic 10", v)
	}
}

func TestAPISummary_IncomePerCalendarMonth(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	trades := []orders.Trade{
		{Kind: "close", At: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), PnL: 0.02, PnLUSD: 2000},
		{Kind: "close", At: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), PnL: -0.005, PnLUSD: -500},
		{Kind: "open", At: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), PnL: 9},                   // opens never count
		{Kind: "close", At: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), PnL: 0.01, PnLUSD: 1000},   // first of the 6
		{Kind: "close", At: time.Date(2026, 4, 30, 23, 0, 0, 0, time.UTC), PnL: 0.01, PnLUSD: 1000}, // too old
	}
	inc := api.IncomeOf(trades, now, 6)
	var names []string
	for _, m := range inc.Months {
		names = append(names, m.Month)
	}
	if strings.Join(names, ",") != "2026-05,2026-06,2026-07,2026-08,2026-09,2026-10" {
		t.Fatalf("months %v", names)
	}
	if inc.ThisMonth.Month != "2026-10" || math.Abs(inc.ThisMonth.RealisedCoin-0.015) > 1e-12 || inc.ThisMonth.RealisedUSD != 1500 || inc.ThisMonth.Closes != 2 {
		t.Errorf("this month %+v", inc.ThisMonth)
	}
	if inc.Months[0].RealisedUSD != 1000 || inc.Months[0].Closes != 1 || inc.Months[1].Closes != 0 {
		t.Errorf("months %+v", inc.Months)
	}
}

func TestAPISummary_EmptyBot(t *testing.T) {
	code, body := getJSON(t, api.New(staticView{strategy.View{Underlying: "ETH"}}, summaryLogger()).Handler(), "/api/summary")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if body["last_action"] != nil || body["holding"].(map[string]any)["nearest_expiry"] != nil {
		t.Errorf("empty bot: %v", body)
	}
	if wc := body["worst_case"].(map[string]any); wc["available"] != false || wc["reason"] == "" {
		t.Errorf("worst case without data: %v", wc)
	}
	if n := len(body["income"].(map[string]any)["months"].([]any)); n != 6 {
		t.Errorf("months %d", n)
	}
}

// Scenario: plain words (one case per journal reason).
func TestAPISummary_PlainWords(t *testing.T) {
	cases := []struct {
		trade orders.Trade
		want  string
	}{
		{orders.Trade{Kind: "close", OptionType: "put", Reason: "stop_loss", Detail: "stop-loss: loss 2.05× the premium ≥ 2.00× (mark 0.03 vs entry 0.01)"},
			"Bought back the put: it lost twice its premium (stop-loss)"},
		{orders.Trade{Kind: "close", OptionType: "put", Reason: "stop_loss"}, "Bought back the put: it lost twice its premium (stop-loss)"},
		{orders.Trade{Kind: "close", OptionType: "call", Reason: "stop_loss", Detail: "loss 2.6× the premium ≥ 2.50×"},
			"Bought back the call: it lost 2.5 times its premium (stop-loss)"},
		{orders.Trade{Kind: "close", OptionType: "call", Reason: "dte_rollout", Detail: "time roll: 3 days to expiry ≤ 3 — the whole strangle closes"},
			"Bought back the call: only 3 days to expiry left, so the strangle closes before expiry (time roll)"},
		{orders.Trade{Kind: "close", OptionType: "call", Reason: "delta_exit"}, "(delta exit)"},
		{orders.Trade{Kind: "close", OptionType: "call", Reason: "delta_drift"}, "(delta drift)"},
		{orders.Trade{Kind: "close", OptionType: "put", Reason: "roi_target"}, "Bought back the put: most of its premium was already earned (take-profit)"},
		{orders.Trade{Kind: "close", OptionType: "put", Reason: "gamma_regime"}, "(GEX shed)"},
		{orders.Trade{Kind: "close", OptionType: "put", Reason: "kill_switch"}, "kill switch"},
		{orders.Trade{Kind: "close", OptionType: "put", Reason: orders.TriggerManualClose}, "(manual close)"},
		{orders.Trade{Kind: "close", OptionType: "put", Reason: orders.TriggerRebalanceDownsize}, "above its limit (rebalance)"},
		{orders.Trade{Kind: "close", OptionType: "put", Reason: orders.TriggerMarginMM}, "(margin cut)"},
		{orders.Trade{Kind: "close", OptionType: "call", Reason: orders.TriggerRebalanceLegs}, "(leg balance)"},
		{orders.Trade{Kind: "close", OptionType: "call", Reason: "something_new"}, "Bought back the call (something_new)"},
		{orders.Trade{Kind: "open", OptionType: "call", Reason: orders.TriggerEntry}, "Sold the call to open a new strangle and collect its premium (entry)"},
		{orders.Trade{Kind: "open", OptionType: "put", Reason: orders.TriggerRepair}, "Sold the put again to complete its strangle (repair)"},
		{orders.Trade{Kind: "open", OptionType: "put", Reason: orders.TriggerRebalanceUpsize}, "(rebalance)"},
		{orders.Trade{Kind: "open", Reason: "reconciled"}, "Sold the option (reconciled)"},
	}
	for _, c := range cases {
		if got := api.PlainReason(c.trade); !strings.Contains(got, c.want) {
			t.Errorf("%s %s: %q, want %q", c.trade.Kind, c.trade.Reason, got, c.want)
		}
	}
}

func TestAPISummary_LastActionIsTheLatestTrade(t *testing.T) {
	now := time.Now().UTC()
	journal := summaryLogger(
		orders.Trade{Seq: 1, At: now.Add(-time.Hour), Kind: "open", OptionType: "put", Reason: orders.TriggerEntry},
		orders.Trade{Seq: 2, At: now, Kind: "close", OptionType: "put", Reason: "stop_loss", PnL: -0.02, PnLUSD: -2000,
			Detail: "stop-loss: loss 2.10× the premium ≥ 2.00×"},
	)
	_, body := getJSON(t, api.New(staticView{summaryView(now)}, journal).Handler(), "/api/summary")
	la := body["last_action"].(map[string]any)
	reason, _ := la["reason"].(string)
	if !strings.HasPrefix(reason, "Bought back the put: it lost twice its premium (stop-loss)") || la["pnl_usd"].(float64) != -2000 || la["rule"] != "stop_loss" {
		t.Errorf("last action %v", la)
	}
}
