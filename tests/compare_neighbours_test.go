package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/aianalysis"
	"optionsbot/internal/compare"
	"optionsbot/internal/config"
)

// cmd/compare -neighbours (#495): the proposed config re-run with one
// setting moved −20 %, −10 %, +10 % and +20 %, cliffs flagged.

// proposedWith writes config_btc.yaml with the line old replaced by new.
func proposedWith(t *testing.T, old, new string) string {
	t.Helper()
	body, err := os.ReadFile("../config_btc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(old)) {
		t.Fatalf("config_btc.yaml no longer has %q", old)
	}
	path := filepath.Join(t.TempDir(), "proposed.yaml")
	if err := os.WriteFile(path, bytes.Replace(body, []byte(old), []byte(new), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func neighboursMarkdown(t *testing.T, r compare.Report) string {
	t.Helper()
	var md bytes.Buffer
	if err := compare.WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	return md.String()
}

// Scenario: a smooth parameter — the stop-loss multiple never fires on the
// fixture's falling prices, so its four neighbours give the proposed
// value's results and no cliff is flagged.
func TestCompareNeighbours_SmoothParameter(t *testing.T) {
	p, _ := cmpData(t)
	p.Proposed = proposedWith(t, "stop_loss_multiplier:   2.0", "stop_loss_multiplier:   2.5")
	r := runCompare(t, p, "none")
	in, err := compare.Load(p, cmpStart.AddDate(0, 0, 3), cmpStart.AddDate(0, 0, 5), 0, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compare.PlanNeighbours(in.Proposed, "stop_loss_multiplier")
	if err != nil {
		t.Fatal(err)
	}
	nb := compare.RunNeighbours(in, r, plan)
	r.Neighbours = &nb

	var values []float64
	for _, row := range nb.Rows {
		values = append(values, row.Value)
		if row.Skipped != "" || !row.Backtest.Ran || !row.Replay.Ran {
			t.Errorf("value %v must run: %+v", row.Value, row)
		}
		if row.Backtest.Fees == 0 || row.Replay.Trades == 0 {
			t.Errorf("value %v: a comparison of nothing %+v", row.Value, row)
		}
	}
	if want := []float64{2, 2.3, 2.5, 2.8, 3}; !floatsEqual(values, want) {
		t.Errorf("values %v, want %v", values, want)
	}
	if nb.Rows[2].Backtest.ReturnPct != r.Backtest.Proposed.ReturnPct {
		t.Errorf("the proposed row must be the report's proposed run: %+v vs %+v", nb.Rows[2].Backtest, r.Backtest.Proposed)
	}
	if len(nb.Cliffs) != 0 {
		t.Errorf("a smooth setting has no cliff: %+v", nb.Cliffs)
	}
	md := neighboursMarkdown(t, r)
	for _, w := range []string{"## Neighbours of `stop_loss_multiplier` (fragility)", "| -20 % | 2 |", "| -10 % | 2.3 |", "| proposed | 2.5 |",
		"| +10 % | 2.8 |", "| +20 % | 3 |", "Cliffs: none", "Backtested and replayed, not a promise", "only after a manager confirms it",
		compare.StressNotNeighboured} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
	}
	if strings.Count(md, "| proposed | 2.5 |") != 2 {
		t.Errorf("one table per source (backtest, replay):\n%s", md)
	}
}

func floatsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i]-b[i] > 1e-9 || b[i]-a[i] > 1e-9 {
			return false
		}
	}
	return true
}

// cliffRows are five values of gamma_flip_buffer_sd: the +10 % neighbour's
// backtest worst week is 2.5 pp deeper than the proposed value's.
func cliffRows() []compare.NeighbourRow {
	fig := func(ret, ww, dd float64) compare.NeighbourFigures {
		return compare.NeighbourFigures{Ran: true, ReturnPct: ret, WorstWeekPct: ww, MaxDrawdownPct: dd, Fees: 200, Trades: 20}
	}
	return []compare.NeighbourRow{
		{StepPct: -20, Value: 1.2, Backtest: fig(1.9, -0.7, 0.9), Replay: fig(0.6, -0.3, 0.4)},
		{StepPct: -10, Value: 1.35, Backtest: fig(1.95, -0.75, 0.95), Replay: fig(0.62, -0.31, 0.41)},
		{StepPct: 0, Value: 1.5, Backtest: fig(2.11, -0.88, 1.15), Replay: fig(0.57, -0.32, 0.45)},
		{StepPct: 10, Value: 1.65, Backtest: fig(-0.42, -3.38, 3.61), Replay: fig(0.55, -0.33, 0.46)},
		{StepPct: 20, Value: 1.8, Skipped: "outside the valid range"},
	}
}

// Scenario: a cliff — a neighbour whose worst week is much worse is flagged,
// and an analysis citing the flagged numbers passes the grounding check.
func TestCompareNeighbours_Cliff(t *testing.T) {
	cliffs := compare.FindCliffs(cliffRows(), compare.CliffPP)
	figures := map[string]bool{}
	for _, c := range cliffs {
		if c.Source != "backtest" || c.From != 1.5 || c.To != 1.65 {
			t.Errorf("only the backtest between 1.5 and 1.65 is a cliff: %+v", c)
		}
		figures[c.Figure] = true
	}
	for _, f := range []string{compare.FigureWorstWeek, compare.FigureDrawdown, compare.FigureReturn} {
		if !figures[f] {
			t.Errorf("cliff on %s not flagged: %+v", f, cliffs)
		}
	}
	if len(cliffs) != 3 {
		t.Errorf("cliffs %+v, want 3", cliffs)
	}

	// At the threshold exactly, no cliff.
	rows := cliffRows()[:2]
	rows[1].Backtest.WorstWeekPct = rows[0].Backtest.WorstWeekPct - compare.CliffPP
	rows[1].Backtest.MaxDrawdownPct = rows[0].Backtest.MaxDrawdownPct
	if c := compare.FindCliffs(rows, compare.CliffPP); len(c) != 0 {
		t.Errorf("a move of exactly %g pp is no cliff: %+v", compare.CliffPP, c)
	}

	r := compare.Report{Disclaimer: compare.Disclaimer, Underlying: "BTC", Neighbours: &compare.Neighbours{
		Key: "gamma_flip_buffer_sd", Proposed: 1.5, CliffThresholdPP: compare.CliffPP, StressNote: compare.StressNotNeighboured,
		Rows: cliffRows(), Cliffs: cliffs}}
	md := neighboursMarkdown(t, r)
	for _, w := range []string{"**Cliffs**", "⚠ backtest worst week pct moves by more than 1 pp from -0.88 % at 1.5 to -3.38 % at 1.65",
		"⚠ backtest return pct flips sign", "| +20 % | 1.8 | | | | | | skipped: outside the valid range |"} {
		if !strings.Contains(md, w) {
			t.Errorf("markdown lacks %q:\n%s", w, md)
		}
	}

	report, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	analysis := aianalysisLabel + "\n## Where it is fragile\n" +
		"Moving `gamma_flip_buffer_sd` from 1.5 to 1.65 deepens the backtest's worst week from -0.88 % to -3.38 %, past the 1 pp cliff threshold.\n" +
		"The backtest return flips from 2.11 % to -0.42 % there, and the +20 % neighbour at 1.8 is skipped.\n"
	if vs := aianalysis.Check(report, analysis); len(vs) != 0 {
		t.Fatalf("the flagged numbers must be citable: %v", vs)
	}
}

// Scenario: a number not in the report — an analysis citing a neighbour
// figure the report does not hold fails the grounding check.
func TestCompareNeighbours_NumberNotInReport(t *testing.T) {
	r := compare.Report{Disclaimer: compare.Disclaimer, Neighbours: &compare.Neighbours{Key: "gamma_flip_buffer_sd", Proposed: 1.5,
		CliffThresholdPP: compare.CliffPP, Rows: cliffRows(), Cliffs: compare.FindCliffs(cliffRows(), compare.CliffPP)}}
	report, _ := json.Marshal(r)
	analysis := aianalysisLabel + "\nAt 1.65 the backtest's worst week is -4.71 %.\n"
	vs := aianalysis.Check(report, analysis)
	if len(vs) != 1 || vs[0].Kind != aianalysis.KindNumber || vs[0].Text != "4.71" {
		t.Fatalf("violations %v, want the invented 4.71", vs)
	}
}

// Scenario: a neighbour outside the valid range — it is skipped and the
// report says why: past the catalogue's bound (clipped onto a value already
// run) or refused by Validate's cross rules.
func TestCompareNeighbours_OutsideValidRange(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	// The catalogue's maximum: gamma_flip_buffer_sd is at most 3.
	cfg, err := config.Load(proposedWith(t, "gamma_flip_buffer_sd:      1.5", "gamma_flip_buffer_sd:      2.9"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := compare.PlanNeighbours(cfg, "gamma_flip_buffer_sd")
	if err != nil {
		t.Fatal(err)
	}
	got := map[float64]compare.NeighbourRow{}
	for _, r := range plan.Rows {
		got[r.StepPct] = r
	}
	if r := got[10]; r.Value != 3 || r.Skipped != "" || !strings.Contains(r.Note, "clipped to 3") {
		t.Errorf("+10 %% (3.19) is clipped to 3 and runs: %+v", r)
	}
	if r := got[20]; !strings.Contains(r.Skipped, "outside the valid range (above the catalogue's maximum 3)") {
		t.Errorf("+20 %% is clipped onto 3 again and skipped: %+v", r)
	}
	if r := got[-20]; r.Value != 2.3 || r.Skipped != "" {
		t.Errorf("-20 %%: %+v", r)
	}

	// Validate's cross rule: delta_drift_threshold at most 75 % of the 0.16
	// entry delta (0.12).
	path := proposedWith(t, "delta_drift_threshold:  0.10", "delta_drift_threshold:  0.12")
	in, err := compare.Load(compare.Paths{Base: "../config_btc.yaml", Proposed: path}, cmpStart, cmpStart, 1, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = compare.PlanNeighbours(in.Proposed, "delta_drift_threshold")
	if err != nil {
		t.Fatal(err)
	}
	r := compare.Run(in)
	nb := compare.RunNeighbours(in, r, plan)
	for _, row := range nb.Rows {
		refused := strings.HasPrefix(row.Skipped, "refused by Validate: ") && strings.Contains(row.Skipped, "delta_drift_threshold")
		switch {
		case row.Value > 0.12 && !refused:
			t.Errorf("%v must be refused by Validate: %+v", row.Value, row)
		case row.Value <= 0.12 && row.Skipped != "":
			t.Errorf("%v is valid: %+v", row.Value, row)
		case row.Value <= 0.12 && (row.Backtest.Note == "" || row.Replay.Note == ""):
			t.Errorf("%v: without data each source says why it did not run: %+v", row.Value, row)
		}
	}
	md := neighboursMarkdown(t, compare.Report{Neighbours: &nb})
	if !strings.Contains(md, "| +10 % | 0.13 | | | | | | skipped: refused by Validate:") {
		t.Errorf("markdown must say why:\n%s", md)
	}
}

// Keys that have no neighbours are refused before anything runs; a list
// element is set in the file by its index.
func TestCompareNeighbours_Keys(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	in, err := compare.Load(compare.Paths{Base: "../config_btc.yaml", Proposed: "../config_btc.yaml"}, cmpStart, cmpStart, 1, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"no_such_key":                   "not a setting of the catalogue",
		"gex_direction":                 "only numbers do",
		"dte_delta_matrix[7].deltas[0]": "the proposed config has no such key",
	} {
		if _, err := compare.PlanNeighbours(in.Proposed, key); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", key, err, want)
		}
	}
	plan, err := compare.PlanNeighbours(in.Proposed, "dte_delta_matrix[1].deltas[0]")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Title != "Slot entry delta" || plan.Proposed != 0.16 || plan.Range != "0.01 to 0.49" {
		t.Errorf("plan %+v", plan)
	}
	nb := compare.RunNeighbours(in, compare.Run(in), plan)
	for _, row := range nb.Rows {
		// 0.13 puts the 0.10 drift threshold above 75 % of the delta.
		if refused := strings.HasPrefix(row.Skipped, "refused by Validate"); refused != (row.Value < 0.1333) {
			t.Errorf("%v: %+v", row.Value, row)
		}
	}
}
