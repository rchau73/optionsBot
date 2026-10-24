package tests

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/compare"
	"optionsbot/internal/config"
	"optionsbot/internal/evidence"
	"optionsbot/internal/risk"
)

// cmd/compare (#186): two configs through the same backtest, replay and
// stress shocks, side by side, the differing settings listed first.

var cmpStart = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

// cmpCSV writes a daily backtest CSV for BTC with two expiries (35 and 60
// days out, fitting config_btc.yaml's 25- and 50-DTE slots): the 0.16-delta
// strikes lose value as time passes, so a take-profit can fire.
func cmpCSV(t *testing.T, days int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "options.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "instrument", "underlying", "underlying_price", "strike", "expiry", "option_type",
		"bid", "ask", "mid", "delta", "gamma", "theta", "vega", "iv", "dvol_index"})
	num := func(v float64) string { return fmtNum(v) }
	for d := 0; d < days; d++ {
		date := cmpStart.AddDate(0, 0, d)
		for _, out := range []int{35, 60} {
			exp := cmpStart.AddDate(0, 0, out)
			// Flat to day 3 (the comparison's first day), then 20 % cheaper
			// a day: 40 % below the entry price on day 5, between the two
			// take-profits TestCompare_OneChangedKey compares; from day 8 it
			// stays at 200 (the windowed tests read later days).
			mid := math.Max(200, 2000*(1-0.2*math.Max(0, float64(d-3))))
			for _, typ := range []string{"call", "put"} {
				strike, delta := 115000.0, 0.16
				if typ == "put" {
					strike, delta = 85000, -0.16
				}
				name := "BTC-" + exp.Format("2Jan06") + "-" + num(strike) + "-" + strings.ToUpper(typ[:1])
				_ = w.Write([]string{date.Format(time.DateOnly), name, "BTC", "100000", num(strike), exp.Format(time.DateOnly), typ,
					num(mid * 0.95), num(mid * 1.05), num(mid), num(delta), "0.00001", "-20", "100", "0.5", "50"})
			}
		}
	}
	w.Flush()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// cmpData lays out every input of a comparison under one folder: backtest
// CSV, six days of recorded books, a captured book with its account and a
// fake cmd/stress that prints a RESULT line.
func cmpData(t *testing.T) (compare.Paths, string) {
	t.Helper()
	root := t.TempDir()
	market := filepath.Join(root, "market")
	record(t, market, cmpStart, 6*24, func(h int) float64 { return 100000 * (1 + 0.01*math.Sin(float64(h)/9)) })
	stress := filepath.Join(root, "stress")
	if err := os.MkdirAll(stress, 0o755); err != nil {
		t.Fatal(err)
	}
	closes, dvol := flatHistory(cmpStart)
	var mk struct {
		Dvol   [][2]float64 `json:"dvol"`
		Closes [][2]float64 `json:"closes"`
	}
	for i := range closes {
		mk.Closes = append(mk.Closes, [2]float64{float64(closes[i].Date.UnixMilli()), closes[i].Close})
		mk.Dvol = append(mk.Dvol, [2]float64{float64(dvol[i].Date.UnixMilli()), dvol[i].Close})
	}
	data, _ := json.Marshal(mk)
	files := map[string]string{
		"positions.json": "{}",
		"market.json":    string(data),
		"account.json":   `{"snapshot":{"assets":[{"currency":"BTC","margin_balance":1}]}}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(stress, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(root, "fake_stress.sh")
	if err := os.WriteFile(script, []byte("printf 'RESULT\\tx\\t0.3\\t1.02\\t2\\t0\\t0\\t1\\t0\\t3\\t0.01\\t40\\t-6\\t0\\t0\\t0\\t0.02\\t2000\\n'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return compare.Paths{Base: "../config_btc.yaml", CSV: cmpCSV(t, 30), Market: market, Stress: stress}, "sh " + script
}

func runCompare(t *testing.T, p compare.Paths, stressCmd string) compare.Report {
	t.Helper()
	t.Setenv("DERIBIT_ENV", "testnet")
	var notes bytes.Buffer
	from, to := cmpStart.AddDate(0, 0, 3), cmpStart.AddDate(0, 0, 5)
	in, err := compare.Load(p, from, to, 0, &notes)
	if err != nil {
		t.Fatal(err)
	}
	if notes.Len() > 0 {
		t.Logf("notes: %s", notes.String())
	}
	compare.RunStress(&in, stressCmd)
	return compare.Run(in)
}

// Scenario: identical configs — every figure is identical and the diff list
// is empty.
func TestCompare_IdenticalConfigs(t *testing.T) {
	p, stressCmd := cmpData(t)
	p.Proposed = p.Base
	r := runCompare(t, p, stressCmd)
	if len(r.Diffs) != 0 {
		t.Errorf("diffs %v, want none", r.Diffs)
	}
	if !r.Backtest.Ran || !r.Replay.Ran || !r.Stress.Ran {
		t.Fatalf("every section must run: backtest %+v, replay %+v, stress %+v", r.Backtest, r.Replay, r.Stress)
	}
	if r.Backtest.Base.FeesUSD == 0 || r.Replay.Base.Trades == 0 {
		t.Errorf("a comparison of nothing: backtest fees %v, replay trades %d", r.Backtest.Base.FeesUSD, r.Replay.Base.Trades)
	}
	if !reflect.DeepEqual(r.Backtest.Base, r.Backtest.Proposed) {
		t.Errorf("backtest:\n base %+v\n prop %+v", r.Backtest.Base, r.Backtest.Proposed)
	}
	if !reflect.DeepEqual(r.Replay.Base, r.Replay.Proposed) {
		t.Errorf("replay:\n base %+v\n prop %+v", r.Replay.Base, r.Replay.Proposed)
	}
	if !reflect.DeepEqual(r.Stress.Base, r.Stress.Proposed) || len(r.Stress.Base) == 0 {
		t.Errorf("stress:\n base %+v\n prop %+v", r.Stress.Base, r.Stress.Proposed)
	}
	var md bytes.Buffer
	if err := compare.WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "None: the two configs give the same settings") || strings.Contains(md.String(), "| Error |") {
		t.Errorf("markdown:\n%s", md.String())
	}
	for _, line := range strings.Split(md.String(), "\n") {
		if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Figure") && !strings.HasPrefix(line, "| Shock") &&
			strings.Count(line, "|") == 5 && !strings.HasSuffix(line, "| = |") {
			t.Errorf("identical runs must show no change: %q", line)
		}
	}
}

// Scenario: one changed key — proposed differs only in roi_take_profit; the
// report lists that key and both runs' return, worst week, drawdown and fees.
func TestCompare_OneChangedKey(t *testing.T) {
	p, stressCmd := cmpData(t)
	body, err := os.ReadFile(p.Base)
	if err != nil {
		t.Fatal(err)
	}
	const tp = "roi_take_profit:        0.50"
	if !bytes.Contains(body, []byte(tp)) {
		t.Fatalf("config_btc.yaml no longer has %q", tp)
	}
	p.Proposed = filepath.Join(t.TempDir(), "my.yaml")
	if err := os.WriteFile(p.Proposed, bytes.Replace(body, []byte(tp), []byte("roi_take_profit:        0.30"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	r := runCompare(t, p, stressCmd)
	want := []compare.Diff{{Key: "roi_take_profit", Base: "0.5", Proposed: "0.3"}}
	if !reflect.DeepEqual(r.Diffs, want) {
		t.Errorf("diffs %v, want %v", r.Diffs, want)
	}
	if !r.Backtest.Ran || !r.Replay.Ran || !r.Stress.Ran {
		t.Fatalf("every section must run: backtest %+v, replay %+v, stress %+v", r.Backtest, r.Replay, r.Stress)
	}
	if r.Backtest.Base.Rolls >= r.Backtest.Proposed.Rolls {
		t.Errorf("a lower take-profit must roll more in the backtest: base %d, proposed %d", r.Backtest.Base.Rolls, r.Backtest.Proposed.Rolls)
	}
	for i, o := range r.Stress.Base {
		if !strings.Contains(o.Command, "-tp 0.5 ") || !strings.Contains(r.Stress.Proposed[i].Command, "-tp 0.3 ") {
			t.Errorf("stress %s must run each config's take-profit: %q / %q", o.Shock, o.Command, r.Stress.Proposed[i].Command)
		}
	}

	dir := compare.OutDir(t.TempDir(), time.Date(2026, 10, 22, 20, 55, 0, 0, time.UTC))
	md, js, err := compare.Write(r, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "20261022T205500Z" || filepath.Base(md) != "report.md" || filepath.Base(js) != "report.json" {
		t.Errorf("paths %s %s %s", dir, md, js)
	}
	text, _ := os.ReadFile(md)
	s := string(text)
	for _, w := range []string{compare.Disclaimer, "- `roi_take_profit` 0.5 → 0.3", "## Backtest (USD)", "## Replay of recorded books",
		"## Stress shocks", "| Return % |", "| Worst week |", "| Max drawdown |", "| Fees |", "| GEX sheds |", "| Stops |"} {
		if !strings.Contains(s, w) {
			t.Errorf("markdown lacks %q:\n%s", w, s)
		}
	}
	if len(r.Stress.NotModelled) != 0 || strings.Contains(s, "Not modelled by the stress") {
		t.Errorf("cmd/stress takes roi_take_profit: not modelled %v", r.Stress.NotModelled)
	}
	if strings.Index(s, "## Differences") > strings.Index(s, "## Backtest") {
		t.Errorf("the differences come first")
	}
	var back compare.Report
	data, _ := os.ReadFile(js)
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Disclaimer != compare.Disclaimer || len(back.Diffs) != 1 || back.Backtest.Proposed != r.Backtest.Proposed ||
		back.Replay.Base.WorstWeek != r.Replay.Base.WorstWeek || back.Replay.Proposed.Fees != r.Replay.Proposed.Fees {
		t.Errorf("json round trip:\n%s", data)
	}
}

// Every differing YAML leaf is named as in the file; fixed values and
// platform settings are not compared.
func TestCompare_DiffConfigs(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	a, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := config.Load("../config_btc.yaml")
	b.GammaFlipBufferSD = 1.0
	b.DTEDeltaMatrix = append(b.DTEDeltaMatrix, config.DTEDeltaEntry{DTE: 9, Deltas: []float64{0.1}})
	b.IVMarginBands = append([]risk.Band(nil), b.IVMarginBands...)
	b.IVMarginBands[2].MaxIMPct = 25
	b.Backtest.FillModel = "bid_ask"
	b.ClientSecret, b.Environment = "secret", "live" // platform: never compared
	b.ChurnMaxRoundTrips = 9                         // fixed: not a YAML key
	got := compare.DiffConfigs(a, b)
	want := []compare.Diff{
		{Key: "backtest.fill_model", Base: "mid", Proposed: "bid_ask"},
		{Key: "dte_delta_matrix[2].deltas[0]", Base: "—", Proposed: "0.1"},
		{Key: "dte_delta_matrix[2].dte", Base: "—", Proposed: "9"},
		{Key: "gamma_flip_buffer_sd", Base: "1.5", Proposed: "1"},
		{Key: "iv_margin_bands[2].max_im_pct", Base: "20", Proposed: "25"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diffs:\n got %v\nwant %v", got, want)
	}
	if got[3].String() != "gamma_flip_buffer_sd 1.5 → 1" {
		t.Errorf("String() %q", got[3].String())
	}
}

// A differing setting cmd/stress does not take is named under the stress
// table, so identical stress rows don't read as "no effect"; one it takes
// is not, and every one it takes really changes StressArgs.
func TestCompare_StressNotModelled(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	a, err := config.Load("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := config.Load("../config_btc.yaml")
	b.RepairCooldownHours = 24 // not a cmd/stress flag
	b.GammaFlipBufferSD = 1.0  // -flipbufsd
	b.IVMarginBands = append([]risk.Band(nil), b.IVMarginBands...)
	b.IVMarginBands[2].MaxIMPct = 25 // -bands: the three bands cmd/stress models
	r := compare.Run(compare.Inputs{BasePath: "a.yaml", ProposedPath: "b.yaml", Base: a, Proposed: b, StressNote: "no -stress"})
	if want := []string{"repair_cooldown_hours"}; !reflect.DeepEqual(r.Stress.NotModelled, want) {
		t.Errorf("not modelled %v, want %v (diffs %v)", r.Stress.NotModelled, want, r.Diffs)
	}
	var md bytes.Buffer
	if err := compare.WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "Not modelled by the stress (cmd/stress does not take them, so they cannot change a row above): `repair_cooldown_hours`.") {
		t.Errorf("markdown:\n%s", md.String())
	}

	// Bands cmd/stress cannot express (not at percentiles 0/30/70) are not seen.
	odd := append([]risk.Band(nil), b.IVMarginBands...)
	odd[1].MinIVPct = 40
	b.IVMarginBands = odd
	if evidence.StressModels("iv_margin_bands[1].min_iv_pct", a, b) {
		t.Error("bands off 0/30/70 are dropped by StressArgs: not modelled")
	}

	// Every key StressModels claims changes the flags cmd/stress gets.
	changes := map[string]func(c *config.Config){
		"roi_take_profit":       func(c *config.Config) { c.ROITakeProfit = 0.3 },
		"delta_drift_threshold": func(c *config.Config) { c.DeltaDriftThreshold = 0.05 },
		"delta_exit_threshold":  func(c *config.Config) { c.DeltaExitThreshold = 0.4 },
		"max_leg_size_multiple": func(c *config.Config) { c.MaxLegSizeMultiple = 3 },
		"gamma_flip_buffer_sd":  func(c *config.Config) { c.GammaFlipBufferSD = 1 },
		"gex_direction": func(c *config.Config) {
			if c.GEXDirection = "bias"; a.GEXDirection == "bias" {
				c.GEXDirection = "sma"
			}
		},
		"dte_delta_matrix[0].dte": func(c *config.Config) {
			c.DTEDeltaMatrix = append([]config.DTEDeltaEntry(nil), c.DTEDeltaMatrix...)
			c.DTEDeltaMatrix[0].DTE = 9
		},
		"iv_margin_bands[0].max_im_pct": func(c *config.Config) {
			c.IVMarginBands = append([]risk.Band(nil), c.IVMarginBands...)
			c.IVMarginBands[0].MaxIMPct = 1
		},
	}
	want := strings.Join(evidence.StressArgs(a, "d", "crash"), " ")
	for key, change := range changes {
		c, _ := config.Load("../config_btc.yaml")
		change(c)
		if !evidence.StressModels(key, a, c) {
			t.Errorf("%s: StressModels false", key)
		}
		if strings.Join(evidence.StressArgs(c, "d", "crash"), " ") == want {
			t.Errorf("%s: StressModels says cmd/stress sees it, but StressArgs did not change", key)
		}
	}
	if evidence.StressModels("min_premium", a, b) || evidence.StressModels("max_mm_pct", a, b) {
		t.Error("min_premium and max_mm_pct are not cmd/stress flags")
	}
}

// Configs of two coins, or one that does not load, are refused; a missing
// data input skips its section and says why.
func TestCompare_InputsRefusedOrSkipped(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	day := cmpStart
	if _, err := compare.Load(compare.Paths{Base: "../config_btc.yaml", Proposed: "../config_eth.yaml"}, day, day, 0, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "one coin") {
		t.Errorf("BTC vs ETH: %v", err)
	}
	if _, err := compare.Load(compare.Paths{Base: "../config_btc.yaml", Proposed: "nope.yaml"}, day, day, 0, &bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "proposed nope.yaml") {
		t.Errorf("missing proposed: %v", err)
	}

	var notes bytes.Buffer
	in, err := compare.Load(compare.Paths{Base: "../config_btc.yaml", Proposed: "../config_btc.yaml", CSV: filepath.Join(t.TempDir(), "none.csv"),
		Market: filepath.Join(t.TempDir(), "market")}, day, day, 0, &notes)
	if err != nil {
		t.Fatal(err)
	}
	compare.RunStress(&in, "sh -c true")
	r := compare.Run(in)
	if r.Backtest.Ran || !strings.Contains(r.Backtest.Note, "-csv") || !strings.Contains(notes.String(), "backtest data not read") {
		t.Errorf("backtest %+v, notes %q", r.Backtest, notes.String())
	}
	if r.Replay.Ran || !strings.Contains(r.Replay.Note, "no recorded book") {
		t.Errorf("replay %+v", r.Replay)
	}
	if r.Stress.Ran || !strings.Contains(r.Stress.Note, "-stress") {
		t.Errorf("stress %+v", r.Stress)
	}
	var md bytes.Buffer
	if err := compare.WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if strings.Count(md.String(), "Not run: ") != 3 {
		t.Errorf("markdown:\n%s", md.String())
	}
}
