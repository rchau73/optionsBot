package tests

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"optionsbot/internal/coverage"
	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/recorder"
	"optionsbot/internal/usdcstudy"
)

// #356: SOL and XRP USDC books recorded hourly beside BTC's, and the
// liquidity report for the SOL/XRP decision (#117).

var studyDay = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

// strikeName writes a strike as Deribit does: "2d5" for 2.5.
func strikeName(k float64) string {
	return strings.Replace(strconv.FormatFloat(k, 'f', -1, 64), ".", "d", 1)
}

// studyRows is a book of prefix ("BTC", "SOL_USDC"…) at `at` around spot:
// two expiries, 32 and 51 days out (one in each slot band), strikes every
// 1 % from 70 % to 130 % of spot, every option at mid 0.02 × scale with a
// spread of 10 % of the mid; putsOneSided leaves the puts' bid empty.
func studyRows(prefix string, at time.Time, spot, scale, oi float64, putsOneSided bool) []gex.SummaryRow {
	var rows []gex.SummaryRow
	for _, days := range []int{32, 51} {
		exp := strings.ToUpper(at.AddDate(0, 0, days).Format("2Jan06"))
		for m := 70; m <= 130; m++ {
			k := math.Round(spot*float64(m)/100*1000) / 1000
			for _, typ := range []string{"C", "P"} {
				mid := 0.02 * scale
				bid, ask := mid*0.95, mid*1.05
				if typ == "P" && putsOneSided {
					bid = 0
				}
				rows = append(rows, gex.SummaryRow{
					InstrumentName:  fmt.Sprintf("%s-%s-%s-%s", prefix, exp, strikeName(k), typ),
					UnderlyingPrice: spot, MarkIV: 50, BidPrice: bid, AskPrice: ask, MidPrice: mid, MarkPrice: mid,
					OpenInterest: oi,
				})
			}
		}
	}
	return rows
}

// countingUSDC answers the USDC book summary with BTC_USDC, SOL_USDC and
// XRP_USDC options and counts the calls.
type countingUSDC struct {
	mu    sync.Mutex
	calls int
}

func (c *countingUSDC) Call(_ context.Context, method string, params any, _ int) (gateway.JSONRPCResponse, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if p, _ := params.(map[string]any); method != "public/get_book_summary_by_currency" || p["currency"] != "USDC" || p["kind"] != "option" {
		return gateway.JSONRPCResponse{}, fmt.Errorf("unexpected call %s %v", method, params)
	}
	now := time.Now()
	var rows []gex.SummaryRow
	rows = append(rows, studyRows("BTC_USDC", now, 100000, 100000, 3, false)...)
	rows = append(rows, studyRows("SOL_USDC", now, 150, 150, 500, false)...)
	rows = append(rows, studyRows("XRP_USDC", now, 2.5, 2.5, 25500, false)...)
	data, err := json.Marshal(rows)
	return gateway.JSONRPCResponse{Result: data}, err
}

func (c *countingUSDC) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// readRawCSV returns every row (header included) of a day file's members.
func readRawCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// Scenario: SOL and XRP books are recorded hourly from mainnet — each
// snapshot writes the BTC, SOL and XRP USDC books from one USDC summary.
func TestUSDCStudy_SOLAndXRPBooksAreRecordedHourlyFromMainnet(t *testing.T) {
	gw := &countingUSDC{}
	usdc := recorder.NewUSDCBooks(gw)
	base := t.TempDir()
	dir := func(u string, own bool) string { return filepath.Join(base, recorder.USDCDataset(u, own)) }
	g := recorder.NewGroup(usdc, time.Hour,
		recorder.New(usdc.Book("BTC"), func() float64 { return 45 }, dir("BTC", true), time.Hour),
		recorder.New(usdc.Book("SOL"), nil, dir("SOL", false), time.Hour),
		recorder.New(usdc.Book("XRP"), nil, dir("XRP", false), time.Hour),
	)
	if got := []string{recorder.USDCDataset("SOL", false), recorder.USDCDataset("XRP", false)}; !reflect.DeepEqual(got, []string{"market_usdc_sol", "market_usdc_xrp"}) {
		t.Fatalf("datasets = %v", got)
	}
	if _, ok := usdc.Book("SOL").(recorder.Fetcher); ok {
		t.Fatal("a book view must not fetch: only the group's one fetch per snapshot")
	}

	for tick := 1; tick <= 2; tick++ {
		n := g.Tick(context.Background(), time.Now())
		if gw.count() != tick {
			t.Fatalf("tick %d: %d USDC summary calls, want one per snapshot", tick, gw.count())
		}
		// 2 expiries × 61 strikes × call/put each.
		if !reflect.DeepEqual(n, []int{244, 244, 244}) {
			t.Fatalf("tick %d: instruments %v, want 244 per book (XRP's 2d5-style strikes parsed)", tick, n)
		}
	}
	// Each book's day file gained one snapshot per tick (both ticks may fall
	// in one second, so count the rows rather than the snapshot times).
	_, at := usdc.Book("BTC").LatestSummary()
	for _, d := range []string{dir("BTC", true), dir("SOL", false), dir("XRP", false)} {
		if rows := readRawCSV(t, filepath.Join(d, at.UTC().Format(time.DateOnly)+".csv.gz")); len(rows) != 1+2*244 {
			t.Fatalf("%s: %d rows, want the header and two snapshots of 244", d, len(rows))
		}
	}
	snaps, _ := recorder.ReadDir(dir("XRP", false), time.Time{}, time.Time{})
	if q := snaps[0].Quotes[0]; q.Underlying != "XRP_USDC" || q.Strike != 1.75 {
		t.Errorf("first XRP quote %s strike %v, want XRP_USDC 1.75", q.Underlying, q.Strike)
	}

	// The single-book LinearBook still fetches for itself (one call each).
	before := gw.count()
	if err := recorder.NewLinearBook(gw, "SOL").FetchSummary(context.Background()); err != nil || gw.count() != before+1 {
		t.Errorf("LinearBook fetch: err %v calls %d", err, gw.count()-before)
	}
}

// Scenario: no DVOL for SOL or XRP — their rows have an empty dvol_index
// and the recorder logs no error.
func TestUSDCStudy_NoDVOLForSOLOrXRP(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	gw := &countingUSDC{}
	usdc := recorder.NewUSDCBooks(gw)
	base := t.TempDir()
	g := recorder.NewGroup(usdc, time.Hour,
		recorder.New(usdc.Book("BTC"), func() float64 { return 45 }, filepath.Join(base, "btc"), time.Hour),
		recorder.New(usdc.Book("SOL"), nil, filepath.Join(base, "sol"), time.Hour),
	)
	if n := g.Tick(context.Background(), time.Now()); n[0] == 0 || n[1] == 0 {
		t.Fatalf("instruments %v", n)
	}
	_, at := usdc.Book("SOL").LatestSummary()
	day := at.UTC().Format(time.DateOnly) + ".csv.gz"
	col := len(recorder.Header) - 1
	if recorder.Header[col] != "dvol_index" {
		t.Fatal("dvol_index is the last column")
	}
	for name, want := range map[string]string{"sol": "", "btc": "45"} {
		recs := readRawCSV(t, filepath.Join(base, name, day))
		for _, r := range recs[1:] {
			if r[col] != want {
				t.Fatalf("%s dvol_index %q, want %q", name, r[col], want)
			}
		}
	}
	for _, level := range []string{`"level":"ERROR"`, `"level":"WARN"`} {
		if strings.Contains(logs.String(), level) {
			t.Errorf("recorder logged %s:\n%s", level, logs.String())
		}
	}
	snaps, err := recorder.ReadDir(filepath.Join(base, "sol"), time.Time{}, time.Time{})
	if err != nil || len(snaps) != 1 || snaps[0].DVOL != 0 {
		t.Errorf("SOL read back: %v %v, want one snapshot with no DVOL", snaps, err)
	}
}

// A decimal strike name parses (Deribit: XRP_USDC-27NOV26-2d5-C is 2.5).
func TestParseOptionName_DecimalStrike(t *testing.T) {
	und, _, strike, typ, err := marketdata.ParseOptionName("XRP_USDC-27NOV26-2d5-C")
	if err != nil || und != "XRP_USDC" || strike != 2.5 || typ != "call" {
		t.Errorf("→ %s %v %s %v", und, strike, typ, err)
	}
	if _, _, _, _, err := marketdata.ParseOptionName("XRP_USDC-27NOV26-2d5d1-C"); err == nil {
		t.Error("two decimal marks must not parse")
	}
}

// The pure rules on a hand-made book: spreads, one-sided share, the 0.16 Δ
// strangle's OI, premium and the liquidity cap floored to the contract.
func TestUSDCStudy_BuildBandFigures(t *testing.T) {
	at := studyDay.Add(10 * time.Hour)
	exp := studyDay.AddDate(0, 0, 30).Add(marketdata.SettlementHourUTC) // 30 DTE
	far := studyDay.AddDate(0, 0, 70).Add(marketdata.SettlementHourUTC) // in no band
	q := func(typ string, k, delta, bid, ask, oi float64, e time.Time) recorder.Quote {
		return recorder.Quote{Instrument: fmt.Sprintf("SOL_USDC-%s-%v-%s", e.Format("2Jan06"), k, typ[:1]), Underlying: "SOL_USDC",
			UnderlyingPrice: 150, Strike: k, Expiry: e, OptionType: typ, Bid: bid, Ask: ask, Delta: delta, OpenInterest: oi}
	}
	snap := recorder.Snapshot{At: at, Quotes: []recorder.Quote{
		q("call", 170, 0.20, 1.9, 2.1, 300, exp),   // spread 10 %
		q("call", 180, 0.15, 0.95, 1.05, 740, exp), // nearest 0.16, spread 10 %
		q("put", 130, -0.17, 1.4, 1.6, 600, exp),   // nearest 0.16, spread 13.33 %
		q("put", 120, -0.11, 0, 0.8, 900, exp),     // one-sided
		q("put", 110, -0.05, 0.1, 0.2, 50, exp),    // under 0.10 Δ: not in the band
		q("call", 200, 0.16, 1, 1.1, 9999, far),    // 70 DTE: no band
	}}
	r := usdcstudy.Build([]usdcstudy.Book{{Underlying: "SOL_USDC", Snapshots: []recorder.Snapshot{snap}}},
		studyDay, studyDay.AddDate(0, 0, 1), nil)
	b := r.Books[0]
	if b.Settlement != "USDC" || b.ContractSize != 10 || b.LargestMove != nil || b.Snapshots != 1 || b.MissingHours != 23 {
		t.Fatalf("book %+v", b)
	}
	bd := b.Bands[0]
	approx := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil || math.Abs(*got-want) > 1e-6 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	if bd.Quotes != 4 || bd.SpreadAll.N != 3 || bd.StrangleHours != 1 || bd.PremiumHours != 1 {
		t.Fatalf("band %+v", bd)
	}
	approx("one-sided %", bd.OneSidedPct, 25)
	approx("spread median", bd.SpreadAll.Median, 10)
	approx("spread p90", bd.SpreadAll.P90, 100*0.2/1.5)
	approx("OI call", bd.OICall, 740)
	approx("OI put", bd.OIPut, 600)
	approx("premium % spot", bd.PremiumBidPct, 100*(0.95+1.4)/150)
	approx("cap coin", bd.CapCoin, 60) // 10 % of 600 = 60, a multiple of 10
	approx("cap USD", bd.CapUSD, 60*150)
	if bd.SpreadVolatile.N != 0 || bd.SpreadVolatile.Median != nil {
		t.Errorf("no move, no volatile hour: %+v", bd.SpreadVolatile)
	}
	if far := b.Bands[1]; far.Quotes != 0 || far.StrangleHours != 0 || far.OneSidedPct != nil || far.CapCoin != nil {
		t.Errorf("40-60 band must be empty: %+v", far)
	}

	// A cap under one contract is counted, not rounded up.
	snap.Quotes[2].OpenInterest = 95 // 9.5 SOL < 10
	r = usdcstudy.Build([]usdcstudy.Book{{Underlying: "SOL_USDC", Snapshots: []recorder.Snapshot{snap}}}, studyDay, studyDay.AddDate(0, 0, 1), nil)
	if bd := r.Books[0].Bands[0]; bd.CapZeroHours != 1 || *bd.CapCoin != 0 {
		t.Errorf("cap under a contract: zero hours %d cap %v", bd.CapZeroHours, *bd.CapCoin)
	}
}

// Scenario: the report compares six books on the same hours, and
// Scenario: missing data is reported, not filled (a day with no SOL
// snapshot). Fixture CSVs written by the real recorder, read back by
// cmd/usdcstudy's Read.
func TestUSDCStudy_ReportComparesSixBooksAndListsMissingDays(t *testing.T) {
	root := t.TempDir()
	btc, eth := filepath.Join(root, "btc"), filepath.Join(root, "eth")
	type book struct {
		dir, dataset, prefix string
		spot, scale, oi      float64
		oneSided             bool
		hours                int // recorded hours from studyDay
	}
	books := []book{
		{btc, "market", "BTC", 100000, 1, 400, false, 48},
		{btc, "market_usdc", "BTC_USDC", 100000, 100000, 3, false, 48},
		{btc, "market_usdc_sol", "SOL_USDC", 150, 150, 500, true, 24}, // day 2 not recorded
		{btc, "market_usdc_xrp", "XRP_USDC", 2.5, 2.5, 25500, false, 48},
		{eth, "market", "ETH", 2500, 1, 2000, false, 48},
		{eth, "market_usdc", "ETH_USDC", 2500, 2500, 40, false, 48},
	}
	for _, b := range books {
		src := &fakeSummary{}
		rec := recorder.New(src, func() float64 { return 50 }, filepath.Join(b.dir, "data", b.dataset), time.Hour)
		for h := 0; h < b.hours; h++ {
			move := 1 + 0.001*float64(h)
			if h >= 30 {
				move *= 1.03 // the window's largest move, at hour 30
			}
			src.rows, src.at = studyRows(b.prefix, studyDay, b.spot*move, b.scale*move, b.oi, b.oneSided), studyDay.Add(time.Duration(h)*time.Hour+time.Minute)
			if _, err := rec.Snapshot(src.at); err != nil {
				t.Fatal(err)
			}
		}
	}

	from, to := studyDay, studyDay.AddDate(0, 0, 2)
	read, notFound, err := usdcstudy.Read([]string{btc, eth}, from, to)
	if err != nil {
		t.Fatal(err)
	}
	r := usdcstudy.Build(read, from, to, notFound)
	var names []string
	for _, b := range r.Books {
		names = append(names, b.Underlying)
	}
	if want := []string{"BTC", "BTC_USDC", "SOL_USDC", "XRP_USDC", "ETH", "ETH_USDC"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("books %v, want %v", names, want)
	}
	if want := []string{filepath.Join(eth, "data", "market_usdc_sol"), filepath.Join(eth, "data", "market_usdc_xrp")}; !reflect.DeepEqual(r.NotFound, want) {
		t.Errorf("not found %v, want %v", r.NotFound, want)
	}

	for _, b := range r.Books {
		if b.LargestMove == nil {
			t.Errorf("%s: no largest move", b.Underlying)
			continue
		}
		wantAt := studyDay.Add(30*time.Hour + time.Minute)
		if b.Underlying == "SOL_USDC" {
			if b.LargestMove.At.After(studyDay.AddDate(0, 0, 1)) {
				t.Errorf("SOL move at %v: no SOL record on day 2", b.LargestMove.At)
			}
		} else if !b.LargestMove.At.Equal(wantAt) || math.Abs(b.LargestMove.Pct-3.1) > 0.01 { // 1.03 × 1.030 ÷ 1.029
			t.Errorf("%s largest move %+v, want ≈ +3 %% at %v", b.Underlying, *b.LargestMove, wantAt)
		}
		if b.VolatileHours != max(1, int(math.Ceil(0.1*float64(b.Snapshots-1)))) {
			t.Errorf("%s volatile hours %d of %d snapshots", b.Underlying, b.VolatileHours, b.Snapshots)
		}
		for _, bd := range b.Bands {
			if bd.Quotes == 0 || bd.SpreadAll.Median == nil || bd.SpreadAll.P90 == nil || bd.SpreadVolatile.Median == nil ||
				bd.OneSidedPct == nil || bd.OICall == nil || bd.OIPut == nil || bd.CapCoin == nil || bd.CapUSD == nil {
				t.Errorf("%s %s: a figure is missing: %+v", b.Underlying, bd.Band, bd)
				continue
			}
			if math.Abs(*bd.SpreadAll.Median-10) > 1e-6 {
				t.Errorf("%s %s spread median %v, want 10 %%", b.Underlying, bd.Band, *bd.SpreadAll.Median)
			}
			if b.Underlying == "SOL_USDC" {
				if math.Abs(*bd.OneSidedPct-50) > 10 || bd.PremiumBidPct != nil {
					t.Errorf("SOL puts one-sided: one-sided %v %% premium %v", *bd.OneSidedPct, bd.PremiumBidPct)
				}
			} else if *bd.OneSidedPct != 0 || bd.PremiumBidPct == nil {
				t.Errorf("%s %s: one-sided %v premium %v", b.Underlying, bd.Band, *bd.OneSidedPct, bd.PremiumBidPct)
			}
		}
	}
	byName := map[string]usdcstudy.BookReport{}
	for _, b := range r.Books {
		byName[b.Underlying] = b
	}
	if sol := byName["SOL_USDC"]; !reflect.DeepEqual(sol.MissingDays, []string{"2026-10-25"}) || sol.MissingHours != 24 {
		t.Errorf("SOL missing days %v hours %v, want [2026-10-25] and 24", sol.MissingDays, sol.MissingHours)
	}
	if x := byName["XRP_USDC"]; len(x.MissingDays) != 0 || *x.Bands[0].CapCoin != 2000 {
		t.Errorf("XRP: missing %v cap %v, want none and 2,000 (2,550 floored to 1,000s)", x.MissingDays, *x.Bands[0].CapCoin)
	}
	if b := byName["BTC_USDC"]; math.Abs(*b.Bands[0].CapCoin-0.3) > 1e-9 {
		t.Errorf("BTC_USDC cap %v, want 0.3 (10 %% of 3)", *b.Bands[0].CapCoin)
	}

	md, js, err := usdcstudy.Write(r, usdcstudy.OutDir(filepath.Join(root, "out"), studyDay))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(md) != filepath.Join(root, "out", "20261024T000000Z") {
		t.Errorf("report folder %s", filepath.Dir(md))
	}
	text, _ := os.ReadFile(md)
	for _, want := range []string{"SOL_USDC | 18-40 DTE", "XRP_USDC | 40-60 DTE", "2026-10-25", "+3.", "Not recorded"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("markdown lacks %q:\n%s", want, text)
		}
	}
	var back usdcstudy.Report
	if data, err := os.ReadFile(js); err != nil || json.Unmarshal(data, &back) != nil || len(back.Books) != 6 {
		t.Errorf("json: %v, %d books", err, len(back.Books))
	}
}

// Coverage: cmd/datacoverage lists the two new datasets.
func TestDataCoverage_ListsSOLAndXRPBooks(t *testing.T) {
	base := t.TempDir()
	src := &fakeSummary{}
	rec := recorder.New(src, nil, filepath.Join(base, "market_usdc_sol"), time.Hour)
	for h := 0; h < 24; h++ {
		src.rows, src.at = studyRows("SOL_USDC", studyDay, 150, 150, 500, false), studyDay.Add(time.Duration(h)*time.Hour)
		if _, err := rec.Snapshot(src.at); err != nil {
			t.Fatal(err)
		}
	}
	res, err := coverage.Run(base, studyDay, studyDay.AddDate(0, 0, 2), studyDay.AddDate(0, 0, 5))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]coverage.Report{}
	for _, r := range res.Datasets {
		byName[r.Dataset] = r
	}
	if s := byName["market_usdc_sol"]; !s.Found || s.Records != 24 || !reflect.DeepEqual(s.MissingDays, []string{"2026-10-25"}) {
		t.Errorf("market_usdc_sol: found %v records %d missing %v", s.Found, s.Records, s.MissingDays)
	}
	if x, ok := byName["market_usdc_xrp"]; !ok || x.Found || x.MissingHours != 48 {
		t.Errorf("market_usdc_xrp: listed %v found %v missing %v h, want listed, absent, 48", ok, x.Found, x.MissingHours)
	}
}
