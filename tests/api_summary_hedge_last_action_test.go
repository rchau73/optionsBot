package tests

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/hedge"
	"optionsbot/internal/orders"
)

// Last action names the newest move, hedge included (#794).

// lastOptionTrade is an option buy-back at at.
func lastOptionTrade(at time.Time) []orders.Trade {
	return []orders.Trade{{Seq: 1, At: at, Kind: "close", Instrument: "BTC-27NOV26-90000-P", OptionType: "put", Qty: 1,
		Reason: "roi_target", PnL: 0.002, PnLUSD: 190, Spot: 95000}}
}

// Scenario: the hedge traded after the last option trade.
func TestAPISummary_HedgeTradedAfterTheLastOptionTrade(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	fills := []orders.PerpFill{{At: now.Add(-time.Minute), Change: orders.HedgeFill, Instrument: "BTC-PERPETUAL",
		Side: hedge.SideSell, FilledUSD: 300, Price: 95100, Spot: 95000}}
	la := api.BuildHedgedSummary(summaryView(now), lastOptionTrade(now.Add(-time.Hour)), api.FuturesHedge{Fills: fills}, now).LastAction

	if la == nil || la.Kind != "hedge" {
		t.Fatalf("the hedge fill is the newest move: %+v", la)
	}
	want, _ := api.ExplainHedge(orders.HedgeFill)
	if !la.At.Equal(fills[0].At) || la.Side != "sell" || la.SizeUSD != 300 || la.Reason != want || la.Rule != orders.HedgeFill || la.Instrument != "BTC-PERPETUAL" {
		t.Errorf("time, side, size and the catalogue's reason: %+v", la)
	}
	if la.PnLCoin != nil || la.PnLUSD != nil || la.OptionType != "" || la.Qty != 0 {
		t.Errorf("a fill is no close and no option: %+v", la)
	}
	if la.Spot == nil || *la.Spot != 95000 {
		t.Errorf("the coin's price at the fill: %v", la.Spot)
	}
}

// Scenario: the option trade is the newest (a tie goes to the option trade).
func TestAPISummary_OptionTradeIsTheNewest(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	trades := lastOptionTrade(now.Add(-time.Minute))
	plain := api.BuildSummary(summaryView(now), trades, now).LastAction
	for _, at := range []time.Time{now.Add(-time.Hour), now.Add(-time.Minute)} {
		fills := []orders.PerpFill{{At: at, Change: orders.HedgeFill, Side: hedge.SideBuy, FilledUSD: 100, Price: 95000}}
		la := api.BuildHedgedSummary(summaryView(now), trades, api.FuturesHedge{Fills: fills}, now).LastAction
		if !reflect.DeepEqual(la, plain) {
			t.Errorf("hedge at %s: want the option trade as today %+v, got %+v", at, plain, la)
		}
	}
}

// Scenario: a hedge close with a loss.
func TestAPISummary_HedgeCloseWithALoss(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	fills := []orders.PerpFill{
		{At: now.Add(-2 * time.Hour), Change: orders.HedgeFill, Side: hedge.SideSell, FilledUSD: 300, Price: 95000},
		{At: now.Add(-time.Minute), Change: orders.HedgeClosed, Side: hedge.SideBuy, FilledUSD: 300, Price: 96000, RealisedCoin: -0.00004},
	}
	la := api.BuildHedgedSummary(summaryView(now), lastOptionTrade(now.Add(-time.Hour)), api.FuturesHedge{Fills: fills}, now).LastAction
	if la == nil || la.Kind != "hedge" || la.Rule != orders.HedgeClosed || la.Side != "buy" {
		t.Fatalf("the close is the newest move: %+v", la)
	}
	if la.PnLCoin == nil || *la.PnLCoin != -0.00004 || la.PnLUSD == nil || !hedgeNear(*la.PnLUSD, -0.00004*96000) {
		t.Errorf("the loss net of fees, signed, in coin and USD: %v %v", la.PnLCoin, la.PnLUSD)
	}
	if want, _ := api.ExplainHedge(orders.HedgeClosed); la.Reason != want {
		t.Errorf("reason %q, want %q", la.Reason, want)
	}
}

// Scenario: no hedge — /api/summary unchanged, and still "no trade yet" with nothing traded.
func TestAPISummary_NoHedgeLastActionUnchanged(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for name, trades := range map[string][]orders.Trade{"an option trade": lastOptionTrade(now.Add(-time.Hour)), "no trade": nil} {
		plain, _ := json.Marshal(api.BuildSummary(summaryView(now), trades, now).LastAction)
		hedged, _ := json.Marshal(api.BuildHedgedSummary(summaryView(now), trades, api.FuturesHedge{}, now).LastAction)
		if string(plain) != string(hedged) {
			t.Errorf("%s: %s, want %s", name, hedged, plain)
		}
		if strings.Contains(string(hedged), "size_usd") || strings.Contains(string(hedged), `"side"`) {
			t.Errorf("%s: no hedge field without a hedge: %s", name, hedged)
		}
	}
	if la := api.BuildHedgedSummary(summaryView(now), nil, api.FuturesHedge{}, now).LastAction; la != nil {
		t.Errorf("no trade yet: %+v", la)
	}
}
