package tests

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/hedge"
	"optionsbot/internal/history"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// hedgeNear compares coin figures to a tenth of a satoshi's worth of float noise.
func hedgeNear(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// issueFills: a short of 32,310 USD at 76,650, 10,000 USD of it bought back
// with +0.0013 realised, fees 0.0009, Deribit's funding reading −0.0001.
func issueFills(at time.Time) []orders.PerpFill {
	const size, avg, realised = 10000.0, 76650.0, 0.0013
	return []orders.PerpFill{
		{At: at, Side: hedge.SideSell, FilledUSD: 32310, Price: avg, FeeCoin: 0.0005},
		{At: at.Add(time.Hour), Side: hedge.SideBuy, FilledUSD: size, Price: 1 / (1/avg + realised/size), FeeCoin: 0.0004, FundingCoin: -0.0001},
	}
}

// Scenario: the worst case includes the hedge.
func TestAPISummary_WorstCaseIncludesTheHedge(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	v.Market.Spot = 76650
	const size, avg = -32310.0, 76650.0
	plain := api.BuildSummary(v, nil, now).WorstCase
	wc := api.BuildHedgedSummary(v, nil, api.FuturesHedge{SizeUSD: size, AvgPrice: avg}, now).WorstCase

	if wc.Hedge == nil || wc.Hedge.OptionsDown.LossCoin != plain.Down.LossCoin || wc.Hedge.OptionsUp.LossUSD != plain.Up.LossUSD {
		t.Fatalf("the options-only figures stay beside it: %+v", wc.Hedge)
	}
	if !strings.Contains(wc.Model, "perpetual") {
		t.Errorf("the model line names the perpetual: %q", wc.Model)
	}
	moved := wc.Down.Spot
	gain := size * (1/avg - 1/moved)
	if gain <= 0 {
		t.Fatalf("a short perpetual gains on a fall: %v", gain)
	}
	if !hedgeNear(wc.Down.LossCoin, plain.Down.LossCoin-gain) || !hedgeNear(wc.Down.LossUSD, plain.Down.LossUSD-gain*moved) {
		t.Errorf("−3σ %+v, want the options' %+v less the gain %v", wc.Down, plain.Down, gain)
	}
	if up := size * (1/avg - 1/wc.Up.Spot); up >= 0 || !hedgeNear(wc.Up.LossCoin, plain.Up.LossCoin-up) {
		t.Errorf("+3σ %+v: a short perpetual loses on a rise (%v)", wc.Up, up)
	}
	if !hedgeNear(*wc.Down.PctOfEquity, wc.Down.LossCoin/2*100) {
		t.Errorf("the share of equity follows the hedged loss: %v", *wc.Down.PctOfEquity)
	}
	if len(wc.Scenarios) != len(plain.Scenarios) {
		t.Fatalf("scenarios %d", len(wc.Scenarios))
	}
	for i, sc := range wc.Scenarios {
		p := plain.Scenarios[i]
		g := size * (1/avg - 1/sc.Spot)
		if !hedgeNear(sc.ResultCoin, p.ResultCoin+g) || !hedgeNear(sc.ResultUSD, p.ResultUSD+g*sc.Spot) || !hedgeNear(*sc.PctOfEquity, sc.ResultCoin/2*100) {
			t.Errorf("scenario %v: %+v, options %+v, perpetual %v", sc.Sigmas, sc, p, g)
		}
	}
}

// Scenario: income books the hedge.
func TestAPIIncome_BooksTheHedge(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	fills := issueFills(now.Add(-48 * time.Hour))
	view := summaryView(now)
	closed := orders.Trade{Kind: "close", At: now.Add(-time.Hour), PnL: 0.01, PnLUSD: 1000, Premium: 0.02, Fees: 0.001}
	sum := api.BuildHedgedSummary(view, []orders.Trade{closed}, api.FuturesHedge{Fills: fills}, now)

	m := sum.Income.ThisMonth
	if m.HedgeFills != 2 || !hedgeNear(m.HedgeRealisedCoin, 0.0013) || !hedgeNear(m.HedgeFeesCoin, 0.0009) || !hedgeNear(m.HedgeFundingCoin, -0.0001) {
		t.Fatalf("hedge source %+v", m)
	}
	if !hedgeNear(m.HedgeCoin, 0.0003) {
		t.Errorf("hedge_coin %v, want +0.0003", m.HedgeCoin)
	}
	if !hedgeNear(m.RealisedCoin, 0.0103) || m.Closes != 1 {
		t.Errorf("the month total includes it: %+v", m)
	}
	if m.HedgeUSD == 0 || !hedgeNear(m.RealisedUSD, 1000+m.HedgeUSD) {
		t.Errorf("USD %v, hedge %v", m.RealisedUSD, m.HedgeUSD)
	}
	if !strings.Contains(sum.Income.Basis, "futures hedge") {
		t.Errorf("basis %q", sum.Income.Basis)
	}
}

// With no hedge nothing changes, not even the JSON.
func TestAPISummary_NoHedgeLeavesTheOutputUnchanged(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	trades := []orders.Trade{{Kind: "close", At: now.Add(-time.Hour), PnL: 0.01, PnLUSD: 1000}}
	want, _ := json.Marshal(api.BuildSummary(v, trades, now))
	for name, h := range map[string]api.FuturesHedge{
		"none":            {},
		"flat perpetual":  {SizeUSD: 0.2, AvgPrice: 76650, FundingCoin: 0.1},
		"no average read": {SizeUSD: -32310},
	} {
		got, _ := json.Marshal(api.BuildHedgedSummary(v, trades, h, now))
		if string(got) != string(want) {
			t.Errorf("%s: output changed\n got %s\nwant %s", name, got, want)
		}
	}
	if strings.Contains(string(want), `"hedge`) {
		t.Errorf("no hedge keys without a hedge: %s", want)
	}
}

func TestAPIWorstCase_PerpetualNeedsAnAvailableModel(t *testing.T) {
	now := time.Now().UTC()
	v := summaryView(now)
	wc := api.WorstCaseOf(v.Strangles, 100000, 0, 2, now) // no DVOL
	if got := api.WorstCaseWithPerp(wc, -32310, 76650, 2); got.Available || got.Hedge != nil {
		t.Errorf("a model that could not run stays so: %+v", got)
	}
	noEquity := api.WorstCaseWithPerp(api.WorstCaseOf(v.Strangles, 100000, 50, 0, now), -32310, 76650, 0)
	if noEquity.Down.PctOfEquity != nil || noEquity.Scenarios[0].PctOfEquity != nil {
		t.Errorf("no equity, no %%: %+v", noEquity.Down)
	}
}

func TestHedgeFlows_RealisedFundingAndEdges(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	const spot = 76000.0
	cases := []struct {
		name                 string
		h                    api.FuturesHedge
		flows                int
		realised, fund, fees float64
		wantFundingFlow      bool
	}{
		{name: "no fills, no perpetual"},
		{name: "open position funding without a fill", h: api.FuturesHedge{SizeUSD: -100, FundingCoin: -0.0002},
			flows: 1, fund: -0.0002, wantFundingFlow: true},
		{name: "the open reading books what the last line did not", h: api.FuturesHedge{Fills: issueFills(now), SizeUSD: -22310, FundingCoin: -0.00025},
			flows: 3, realised: 0.0013, fund: -0.00025, fees: 0.0009, wantFundingFlow: true},
		{name: "a stale reading on a closed perpetual is not booked", h: api.FuturesHedge{Fills: issueFills(now), FundingCoin: -0.00025},
			flows: 2, realised: 0.0013, fund: -0.0001, fees: 0.0009},
		{name: "a fill without a price falls back on the spot", h: api.FuturesHedge{Fills: []orders.PerpFill{
			{At: now, Side: hedge.SideSell, FilledUSD: 1000, Spot: 80000},
			{At: now, Side: hedge.SideBuy, FilledUSD: 1000, Spot: 76000},
		}}, flows: 2, realised: 1000 * (1.0/76000 - 1.0/80000)},
		{name: "funding starts again after the perpetual went flat", h: api.FuturesHedge{Fills: []orders.PerpFill{
			{At: now, Side: hedge.SideSell, FilledUSD: 1000, Price: 80000},
			{At: now, Side: hedge.SideBuy, FilledUSD: 1000, Price: 80000, FundingCoin: 0.0003},
			{At: now, Side: hedge.SideSell, FilledUSD: 1000, Price: 80000, FundingCoin: 0},
			{At: now, Side: hedge.SideBuy, FilledUSD: 1000, Price: 80000, FundingCoin: 0.0001},
		}}, flows: 4, fund: 0.0004},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			flows := c.h.Flows(spot, now)
			var realised, fund, fees float64
			for _, f := range flows {
				realised += f.RealisedCoin
				fund += f.FundingCoin
				fees += f.FeesCoin
			}
			if len(flows) != c.flows || !hedgeNear(realised, c.realised) || !hedgeNear(fund, c.fund) || !hedgeNear(fees, c.fees) {
				t.Errorf("flows %+v", flows)
			}
			if c.wantFundingFlow {
				if last := flows[len(flows)-1]; !last.At.Equal(now) || last.Spot != spot || last.Fills != 0 {
					t.Errorf("the open funding is now, at spot, and no fill: %+v", last)
				}
			}
		})
	}
}

func TestAPIIncome_HedgeMonthsAndStats(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	inc := api.IncomeOf(nil, now, api.SummaryMonths)
	flows := []api.HedgeFlow{
		{At: now.AddDate(-1, 0, 0), RealisedCoin: 9}, // before the months shown, as a close would be
		{At: now.AddDate(0, -1, 0), Spot: 70000, Fills: 1, FeesCoin: 0.001},
		{At: now, Spot: 76000, FundingCoin: 0.002}, // a month with funding only is a month with a result
	}
	got := api.IncomeWithHistory(api.IncomeWithHedge(inc, flows), nil, now, "testnet")
	if inc.Months[5].RealisedCoin != 0 {
		t.Error("the input is not changed")
	}
	if !hedgeNear(got.Months[4].RealisedCoin, -0.001) || !hedgeNear(got.Months[4].RealisedUSD, -70) || got.Months[4].HedgeFills != 1 {
		t.Errorf("October %+v", got.Months[4])
	}
	if !hedgeNear(got.ThisMonth.RealisedCoin, 0.002) {
		t.Errorf("this month %+v", got.ThisMonth)
	}
	s := got.Summary
	if s.ActiveMonths != 2 || s.UpMonths != 1 || s.DownMonths != 1 || !hedgeNear(s.Total.Coin, 0.001) || s.Source != api.IncomeHedgedSource {
		t.Errorf("stats %+v", s)
	}
	if s.WorstMonth == nil || s.WorstMonth.Month != "2026-10" {
		t.Errorf("worst month %+v", s.WorstMonth)
	}
}

func TestAPIIncomeStart_CountsTheHedgeButNotAsAClose(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	at := now.Add(-48 * time.Hour)
	balance := 2.0
	points := []history.Point{{T: at, Spot: 76000, MarginBalance: &balance, Unit: "BTC"}, {T: now, Spot: 76000, MarginBalance: &balance, Unit: "BTC"}}
	trades := api.HedgeTrades([]api.HedgeFlow{{At: at.Add(time.Hour), RealisedCoin: 0.002}})
	st := api.IncomeStartOf([]string{"2026-11"}, trades, points, time.Time{}, now)
	if !st.Known || !hedgeNear(st.ResultCoin, 0.002) || st.Closes != 0 || !hedgeNear(*st.ResultPct, 0.1) {
		t.Errorf("start %+v", st.StartResult)
	}
}

// The journal is the history: hedge lines survive a restart, and only a line
// that moved the perpetual counts.
func TestJournal_PerpFillsReplayAndRestore(t *testing.T) {
	var b strings.Builder
	l := orders.NewWriterLogger(&b, 0)
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	l.LogHedge(orders.HedgeRecord{Timestamp: at, Change: orders.HedgeFill, Side: "sell", AmountUSD: 100, FilledUSD: 100, Price: 76650, FeeCoin: 0.0001,
		FundingCoin: -0.0002, Market: orders.MarketSnapshot{Spot: 76600}})
	l.LogHedge(orders.HedgeRecord{Timestamp: at, Change: orders.HedgeFill, Side: "sell", AmountUSD: 100}) // an IOC with no match
	l.LogHedge(orders.HedgeRecord{Timestamp: at, Change: orders.HedgeRateLimited, AmountUSD: 100, FilledUSD: 100})
	l.LogHedge(orders.HedgeRecord{Timestamp: at, Change: orders.HedgeClosed, Side: "buy", FilledUSD: 100, Price: 76000})
	want := orders.PerpFill{At: at, Side: "sell", FilledUSD: 100, Price: 76650, FeeCoin: 0.0001, FundingCoin: -0.0002, Spot: 76600}
	if live := l.PerpFills(); len(live) != 2 || live[0] != want {
		t.Fatalf("live fills %+v", live)
	}

	rp, err := orders.ReplayJournal(strings.NewReader(b.String()+`{"event":"hedge","change":"hedge_fill","filled_usd":"torn"}`+"\n"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rp.PerpFills) != 2 || !rp.PerpFills[0].At.Equal(at) || rp.PerpFills[0].FundingCoin != want.FundingCoin || rp.PerpFills[1].Side != "buy" {
		t.Fatalf("replayed fills %+v", rp.PerpFills)
	}
	restored := orders.NewWriterLogger(io.Discard, 0)
	restored.Restore(rp)
	if len(restored.PerpFills()) != 2 {
		t.Errorf("restored %+v", restored.PerpFills())
	}
}

// openHedge is a controller with a perpetual open.
type openHedge struct {
	fakeHedgeController
	view strategy.HedgeView
}

func (o *openHedge) HedgeState() strategy.HedgeView { return o.view }

// The routes read the journal's fills and the published perpetual.
func TestAPIRoutes_SummaryAndIncomeCountTheHedge(t *testing.T) {
	now := time.Now().UTC()
	l := orders.NewWriterLogger(io.Discard, 0)
	for _, f := range issueFills(now.Add(-time.Minute)) {
		l.LogHedge(orders.HedgeRecord{Timestamp: f.At, Change: orders.HedgeFill, Side: f.Side, FilledUSD: f.FilledUSD, Price: f.Price, FeeCoin: f.FeeCoin, FundingCoin: f.FundingCoin})
	}
	c := &openHedge{view: strategy.HedgeView{PerpUSD: -22310, PerpAvg: 76650, FundingCoin: -0.0001}}
	h := api.New(staticView{summaryView(now)}, l, api.WithFuturesHedge(c, "", true)).Handler()

	var sum api.Summary
	if rec := hedgeCall(h, http.MethodGet, "/api/summary", "", ""); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &sum) != nil {
		t.Fatalf("summary %d %s", rec.Code, rec.Body)
	}
	if sum.WorstCase.Hedge == nil || sum.WorstCase.Hedge.SizeUSD != -22310 || !hedgeNear(sum.Income.ThisMonth.HedgeCoin, 0.0003) {
		t.Errorf("summary worst case %+v, this month %+v", sum.WorstCase.Hedge, sum.Income.ThisMonth)
	}
	var inc api.IncomeSummary
	if rec := hedgeCall(h, http.MethodGet, "/api/income", "", ""); rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &inc) != nil {
		t.Fatalf("income %d %s", rec.Code, rec.Body)
	}
	if !hedgeNear(inc.ThisMonth.HedgeCoin, 0.0003) || !hedgeNear(inc.Summary.Total.Coin, 0.0003) {
		t.Errorf("income %+v", inc.ThisMonth)
	}
}
