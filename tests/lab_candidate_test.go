package tests

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/compare"
	"optionsbot/internal/config"
)

// #806: cmd/labapply turns a recorded lab change (frontend/lib/labapply.js
// writes the shape of testdata/lab_applied_record.json) into a checked
// config candidate under COMPARE_DIR/candidates/<id>/; it applies nothing.

const candID = "20261104T230000Z"

// candChange is one change of a test record.
type candChange struct {
	Key string
	Old any
	New any
}

// candSetup writes the fixture record (its config, changes and approval
// replaced when given) and the run's report into a new COMPARE_DIR; the
// shipped configs are the repository's own.
func candSetup(t *testing.T, cfg, approval string, changes ...candChange) compare.CandidateOptions {
	t.Helper()
	raw, err := os.ReadFile("testdata/lab_applied_record.json")
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		rec["account"].(map[string]any)["config"] = cfg
	}
	if approval != "" {
		rec["product_owner_approval"] = approval
	}
	if len(changes) > 0 {
		var list []map[string]any
		for _, c := range changes {
			list = append(list, map[string]any{"key": c.Key, "title": c.Key, "old": c.Old, "new": c.New})
		}
		rec["changes"] = list
	}
	root := t.TempDir()
	o := compare.CandidateOptions{CompareDir: filepath.Join(root, "compare"), ConfigDir: ".."}
	body, _ := json.MarshalIndent(rec, "", "  ")
	candWrite(t, filepath.Join(o.CompareDir, "applied", candID+".json"), body)
	report, err := os.ReadFile("testdata/lab_candidate_report.json")
	if err != nil {
		t.Fatal(err)
	}
	candWrite(t, filepath.Join(o.CompareDir, candID, "report.json"), report)
	return o
}

func candWrite(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func candDir(o compare.CandidateOptions) string {
	return filepath.Join(o.CompareDir, "candidates", candID)
}

func readStatus(t *testing.T, o compare.CandidateOptions) compare.CandidateStatus {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(candDir(o), compare.CandidateStatusFile))
	if err != nil {
		t.Fatal(err)
	}
	var st compare.CandidateStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func runCandidate(t *testing.T, o compare.CandidateOptions) compare.CandidateStatus {
	t.Helper()
	if _, err := compare.BuildCandidate(o, candID); err != nil {
		t.Fatal(err)
	}
	return readStatus(t, o)
}

// changedLines are the lines of a and b that differ (same line count expected).
func changedLines(t *testing.T, a, b []byte) []string {
	t.Helper()
	la, lb := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	if len(la) != len(lb) {
		t.Fatalf("line count %d → %d", len(la), len(lb))
	}
	var out []string
	for i := range la {
		if la[i] != lb[i] {
			out = append(out, lb[i])
		}
	}
	return out
}

func reasonsHave(st compare.CandidateStatus, want string) bool {
	for _, r := range st.Reasons {
		if strings.Contains(r, want) {
			return true
		}
	}
	return false
}

func noCandidateConfig(t *testing.T, o compare.CandidateOptions) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(candDir(o), compare.CandidateConfigFile)); !os.IsNotExist(err) {
		t.Fatalf("a refused record has a candidate config (stat: %v)", err)
	}
}

// Scenario: A recorded in-range change becomes a candidate.
func TestLabCandidate_InRangeChangeBecomesCandidate(t *testing.T) {
	o := candSetup(t, "", "")
	st := runCandidate(t, o)
	if st.Status != compare.CandidateReady || len(st.Reasons) != 0 || st.Applied {
		t.Fatalf("status %s, reasons %v, applied %v", st.Status, st.Reasons, st.Applied)
	}
	shipped, _ := os.ReadFile("../config_btc.yaml")
	cand, err := os.ReadFile(filepath.Join(candDir(o), compare.CandidateConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := changedLines(t, shipped, cand)
	if len(lines) != 1 || strings.TrimSpace(lines[0]) != "slippage_pct:             0.002" {
		t.Fatalf("changed lines %q: want only slippage_pct, aligned as before", lines)
	}
	path := filepath.Join(candDir(o), compare.CandidateConfigFile)
	base, _ := config.Load("../config_btc.yaml")
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	diffs := compare.DiffConfigs(base, got)
	if len(diffs) != 1 || diffs[0].Key != "backtest.slippage_pct" || diffs[0].Proposed != "0.002" {
		t.Fatalf("diffs %v", diffs)
	}
	md, _ := os.ReadFile(filepath.Join(candDir(o), compare.CandidateDiffFile))
	for _, want := range []string{"`backtest.slippage_pct`", "Backtest market slippage", "| 0.001 | 0.002 |", "backtested, not a forecast",
		"backtest: worst week -0.90 %, largest drawdown 1.30 %, fees 0.25 % of the starting equity",
		"replay of recorded books: worst week -0.50 %, largest drawdown 1.50 %, fees 0.25 % of the starting equity",
		"past data only", "nothing was applied to a running bot"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("diff.md lacks %q:\n%s", want, md)
		}
	}
}

// A core-policy change with the product owner's approval reference is
// written; mvp's config is a shipped config too.
func TestLabCandidate_CorePolicyWithApprovalIsReady(t *testing.T) {
	o := candSetup(t, "config_btc.mvp.yaml", "#900 (product owner, 2026-11-10)", candChange{"gex_direction", "sma", "bias"})
	st := runCandidate(t, o)
	if st.Status != compare.CandidateReady || st.NeedsProductOwner {
		t.Fatalf("status %s, reasons %v", st.Status, st.Reasons)
	}
	shipped, _ := os.ReadFile("../config_btc.mvp.yaml")
	cand, _ := os.ReadFile(filepath.Join(candDir(o), compare.CandidateConfigFile))
	if lines := changedLines(t, shipped, cand); len(lines) != 1 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "gex_direction:") || !strings.Contains(lines[0], "bias") {
		t.Fatalf("changed lines %q", lines)
	}
	if md, _ := os.ReadFile(filepath.Join(candDir(o), compare.CandidateDiffFile)); !strings.Contains(string(md), "#900 (product owner, 2026-11-10)") {
		t.Errorf("diff.md lacks the approval reference:\n%s", md)
	}
}

// Scenario: A stale record is refused.
func TestLabCandidate_StaleRecordIsRefused(t *testing.T) {
	o := candSetup(t, "", "", candChange{"backtest.slippage_pct", 0.005, "0.002"})
	st := runCandidate(t, o)
	if st.Status != compare.CandidateRefused || !reasonsHave(st, compare.StaleReason) {
		t.Fatalf("status %s, reasons %v", st.Status, st.Reasons)
	}
	noCandidateConfig(t, o)
}

// Scenario: A fixed or out-of-range key is refused.
func TestLabCandidate_FixedOrOutOfRangeKeyIsRefused(t *testing.T) {
	approval := "#900"
	for _, tc := range []struct {
		name   string
		change candChange
		want   string
	}{
		{"fixed key", candChange{"gex_shed_confirm_snapshots", 2, "3"}, `config key "gex_shed_confirm_snapshots" is fixed in code now`},
		{"outside the catalogue's range", candChange{"gamma_flip_buffer_sd", 1.5, "5"}, "gamma_flip_buffer_sd: 5 must be at most 3"},
		{"off the step", candChange{"gamma_flip_buffer_sd", 1.5, "1.25"}, "must be on a step of 0.1"},
		{"not one of the values", candChange{"gex_direction", "bias", "trend"}, "must be one of sma, bias"},
		{"a cross rule Validate refuses", candChange{"delta_drift_threshold", 0.1, "0.15"}, "the candidate does not load"},
		{"not in the catalogue", candChange{"no_such_setting", 1, "2"}, "not a setting of the catalogue"},
		{"a whole list", candChange{"dte_delta_matrix[].dte", 25, "30"}, "a list element needs its index"},
		{"a list element the file lacks", candChange{"dte_delta_matrix[5].dte", 25, "30"}, "dte_delta_matrix[5].dte"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := candSetup(t, "", approval, tc.change)
			st := runCandidate(t, o)
			if st.Status != compare.CandidateRefused || !reasonsHave(st, tc.want) {
				t.Fatalf("status %s, reasons %v: want %q", st.Status, st.Reasons, tc.want)
			}
			noCandidateConfig(t, o)
		})
	}
}

// Scenario: A core-policy key needs the product owner.
func TestLabCandidate_CorePolicyNeedsProductOwner(t *testing.T) {
	o := candSetup(t, "", "", candChange{"gamma_flip_buffer_sd", 1.5, "1.2"})
	st := runCandidate(t, o)
	if st.Status != compare.CandidateRefused || !st.NeedsProductOwner || !reasonsHave(st, compare.NeedsProductOwner) {
		t.Fatalf("status %s, needs_product_owner %v, reasons %v", st.Status, st.NeedsProductOwner, st.Reasons)
	}
	noCandidateConfig(t, o)
}

// Records that cannot be trusted are refused before any config is read.
func TestLabCandidate_UntrustedRecordIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{"another run", func(r map[string]any) { r["run_id"] = "20261101T000000Z" }, "is for run"},
		{"says applied", func(r map[string]any) { r["applied"] = true }, "applied: false"},
		{"not recorded", func(r map[string]any) { r["recorded"] = false }, "recorded: true"},
		{"no change", func(r map[string]any) { r["changes"] = []any{} }, "changes no setting"},
		{"a file that is not a shipped config", func(r map[string]any) { r["account"].(map[string]any)["config"] = "../.env" }, "not a shipped config"},
		{"another coin", func(r map[string]any) { r["account"].(map[string]any)["underlying"] = "ETH" }, "the record is for ETH"},
		{"a list as the value", func(r map[string]any) {
			r["changes"] = []any{map[string]any{"key": "min_premium", "old": 0.001, "new": []any{1}}}
		}, "one number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := candSetup(t, "", "")
			path := filepath.Join(o.CompareDir, "applied", candID+".json")
			raw, _ := os.ReadFile(path)
			var rec map[string]any
			_ = json.Unmarshal(raw, &rec)
			tc.edit(rec)
			body, _ := json.Marshal(rec)
			candWrite(t, path, body)
			st := runCandidate(t, o)
			if st.Status != compare.CandidateRefused || !reasonsHave(st, tc.want) {
				t.Fatalf("status %s, reasons %v: want %q", st.Status, st.Reasons, tc.want)
			}
			noCandidateConfig(t, o)
		})
	}

	o := candSetup(t, "", "")
	candWrite(t, filepath.Join(o.CompareDir, "applied", candID+".json"), []byte("{not json"))
	if st := runCandidate(t, o); st.Status != compare.CandidateRefused || !reasonsHave(st, "cannot be read") {
		t.Fatalf("malformed record: %+v", st)
	}
	if _, err := compare.BuildCandidate(o, "../applied/x"); err == nil {
		t.Fatal("an id that is not a folder time must be an error")
	}
	if _, err := compare.BuildCandidate(o, "20261101T000000Z"); err == nil {
		t.Fatal("a missing record must be an error")
	}
	_ = os.Remove(filepath.Join(o.CompareDir, candID, "report.json"))
	candWrite(t, filepath.Join(o.CompareDir, "applied", candID+".json"), mustRead(t, "testdata/lab_applied_record.json"))
	if st := runCandidate(t, o); st.Status != compare.CandidateRefused || !reasonsHave(st, "the run's report cannot be read") {
		t.Fatalf("no report: %+v", st)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Scenario: Nothing running changes.
func TestLabCandidate_NothingRunningChanges(t *testing.T) {
	watched := []string{"../config_btc.yaml", "../config_btc.mvp.yaml", "../config_eth.yaml"}
	before := map[string][]byte{}
	for _, f := range watched {
		before[f] = mustRead(t, f)
	}
	for _, o := range []compare.CandidateOptions{
		candSetup(t, "", ""),
		candSetup(t, "", "", candChange{"gamma_flip_buffer_sd", 1.5, "1.2"}),
		candSetup(t, "config_btc.mvp.yaml", "#900", candChange{"gex_direction", "sma", "bias"}),
	} {
		record := filepath.Join(o.CompareDir, "applied", candID+".json")
		recBefore := mustRead(t, record)
		runCandidate(t, o)
		if string(mustRead(t, record)) != string(recBefore) {
			t.Fatal("the applied record changed")
		}
	}
	for _, f := range watched {
		if string(mustRead(t, f)) != string(before[f]) {
			t.Fatalf("%s changed", f)
		}
	}
	// No network call: neither the command nor the candidate code imports a
	// network, process or exchange package.
	for _, file := range []string{"../cmd/labapply/main.go", "../internal/compare/candidate.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, banned := range []string{"net", "os/exec", "optionsbot/internal/gateway", "optionsbot/internal/exchange", "optionsbot/internal/strategy", "optionsbot/internal/aianalysis"} {
				if p == banned || strings.HasPrefix(p, banned+"/") {
					t.Errorf("%s imports %s", file, p)
				}
			}
		}
	}
}

// Scenario: Running twice is safe.
func TestLabCandidate_RunningTwiceIsSafe(t *testing.T) {
	for _, o := range []compare.CandidateOptions{candSetup(t, "", ""), candSetup(t, "", "", candChange{"gamma_flip_buffer_sd", 1.5, "1.2"})} {
		runCandidate(t, o)
		entries, _ := os.ReadDir(candDir(o))
		snap := map[string]string{}
		mtimes := map[string]time.Time{}
		for _, e := range entries {
			p := filepath.Join(candDir(o), e.Name())
			snap[e.Name()] = string(mustRead(t, p))
			info, _ := os.Stat(p)
			mtimes[e.Name()] = info.ModTime()
		}
		runCandidate(t, o)
		again, _ := os.ReadDir(candDir(o))
		if len(again) != len(entries) {
			t.Fatalf("files %d → %d", len(entries), len(again))
		}
		for name, body := range snap {
			p := filepath.Join(candDir(o), name)
			info, _ := os.Stat(p)
			if string(mustRead(t, p)) != body || !info.ModTime().Equal(mtimes[name]) {
				t.Errorf("%s rewritten", name)
			}
		}
	}
}

// A record refused after a ready run leaves no candidate config behind.
func TestLabCandidate_RefusedAfterReadyRemovesCandidate(t *testing.T) {
	o := candSetup(t, "", "")
	if st := runCandidate(t, o); st.Status != compare.CandidateReady {
		t.Fatalf("first run: %+v", st)
	}
	path := filepath.Join(o.CompareDir, "applied", candID+".json")
	stale := strings.Replace(string(mustRead(t, path)), `"old": 0.001`, `"old": 0.004`, 1)
	candWrite(t, path, []byte(stale))
	if st := runCandidate(t, o); st.Status != compare.CandidateRefused {
		t.Fatalf("second run: %+v", st)
	}
	noCandidateConfig(t, o)
}

// The in-place edit keeps comments, quotes a word YAML would misread, and
// adds a missing top-level key at the end.
func TestLabCandidate_EditConfigText(t *testing.T) {
	src := []byte("a: 1   # one\nb:\n  c: \"x\"\nlist:\n  - { d: 2, e: 3 }\n")
	out, err := compare.EditConfigText(src, []compare.Setting{{Key: "a", Value: 1.5}, {Key: "b.c", Value: "yes"}, {Key: "list[0].e", Value: 4}, {Key: "z", Value: "word"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "a: 1.5   # one\nb:\n  c: \"yes\"\nlist:\n  - { d: 2, e: 4 }\nz: word\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if _, err := compare.EditConfigText(src, []compare.Setting{{Key: "b.missing", Value: 1}}); err == nil {
		t.Fatal("a missing nested key must be an error")
	}
}
