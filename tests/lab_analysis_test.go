package tests

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/aianalysis"
	"optionsbot/internal/compare"
)

// #792: the lab's runner writes the checked AI analysis after each done run,
// best effort. Fake models only; no test calls the API.

const labReportID = "20261105T010000Z"

// labCompareWithReport is a cmd/compare that writes the saved comparison
// report (testdata/aianalysis/report.json) as the run's report.json.
func labCompareWithReport(t *testing.T, o compare.LabRunOptions) compare.CompareFunc {
	t.Helper()
	report, err := os.ReadFile("testdata/aianalysis/report.json")
	if err != nil {
		t.Fatal(err)
	}
	return func(_ context.Context, _ []string) (string, error) {
		dir := filepath.Join(o.OutDir, labReportID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "report.json"), report, 0o644); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, "report.md") + ", " + filepath.Join(dir, "report.json") + "\n", nil
	}
}

func labAnalysisFiles(o compare.LabRunOptions) (md, js string) {
	dir := filepath.Join(o.OutDir, labReportID)
	return filepath.Join(dir, "analysis.md"), filepath.Join(dir, "analysis.json")
}

func labNowFn() time.Time { return labNow }

// Scenario: a finished run gets its analysis — analysis.json and analysis.md
// beside report.json, the Disclaimer first; the request done with its run_id.
func TestLabAnalysis_FinishedRunGetsItsAnalysis(t *testing.T) {
	o, _ := labQueue(t, labReqID)
	answer, err := os.ReadFile("testdata/aianalysis/answer.md")
	if err != nil {
		t.Fatal(err)
	}
	m := &fakeAnalysisModel{answer: string(answer)}
	sum, err := compare.RunLabQueue(context.Background(), o, labNowFn, labCompareWithReport(t, o), aianalysis.LabAnalyzer(m, "fake", labNowFn))
	if err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || m.system != aianalysis.Prompt {
		t.Fatalf("one model call with the contract's prompt: %d", m.calls)
	}
	md, js := labAnalysisFiles(o)
	text, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(string(text), "\n"); first != "> **"+compare.Disclaimer+"**" {
		t.Errorf("analysis.md first line %q", first)
	}
	data, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"report": "report.json"`)) || !bytes.Contains(data, []byte(`"model": "fake"`)) {
		t.Errorf("analysis.json is not the run's: %s", data)
	}
	r := labRequest(t, o, labReqID)
	if r.Status != compare.StatusDone || r.RunID != labReportID || r.Note != "" || r.Reason != "" {
		t.Errorf("request after the run: %+v", r)
	}
	if sum != (compare.LabRunSummary{Done: 1}) {
		t.Errorf("summary %+v", sum)
	}
}

// Scenario: the analysis fails and the run stays done — a model error or an
// answer that fails aianalysis.Check writes no file and one plain note.
func TestLabAnalysis_FailedAnalysisLeavesRunDone(t *testing.T) {
	for _, c := range []struct {
		name    string
		analyze compare.AnalyzeFunc
		want    string
	}{
		{"model error", func(context.Context, string) error { return errors.New("model call: the API answered 529 overloaded") },
			"analysis not written: model call: the API answered 529 overloaded"},
		{"failed check", aianalysis.LabAnalyzer(&fakeAnalysisModel{answer: "You should buy now: returns of 99.9 % are guaranteed."}, "fake", labNowFn),
			"analysis not written: the answer failed the checks ("},
	} {
		t.Run(c.name, func(t *testing.T) {
			o, _ := labQueue(t, labReqID)
			sum, err := compare.RunLabQueue(context.Background(), o, labNowFn, labCompareWithReport(t, o), c.analyze)
			if err != nil {
				t.Fatal(err)
			}
			r := labRequest(t, o, labReqID)
			if r.Status != compare.StatusDone || r.RunID != labReportID || r.Reason != "" {
				t.Errorf("the run must stay done: %+v", r)
			}
			if !strings.HasPrefix(r.Note, c.want) || strings.Contains(r.Note, "\n") || len([]rune(r.Note)) > len(compare.AnalysisNotWritten)+compare.ReasonMaxLen {
				t.Errorf("note %q, want it to start %q", r.Note, c.want)
			}
			md, js := labAnalysisFiles(o)
			for _, f := range []string{md, js} {
				if _, err := os.Stat(f); !os.IsNotExist(err) {
					t.Errorf("%s must not be written: %v", filepath.Base(f), err)
				}
			}
			if sum != (compare.LabRunSummary{Done: 1}) {
				t.Errorf("summary %+v", sum)
			}
		})
	}
}

// Scenario: no key means no call — ModelFor without a key gives no model, the
// analyzer sends nothing and the run is done without an analysis.
func TestLabAnalysis_NoKeyMeansNoCall(t *testing.T) {
	model, name, err := aianalysis.ModelFor("", "")
	if !errors.Is(err, aianalysis.ErrNoAPIKey) || model != nil {
		t.Fatalf("no key: model %v, err %v", model, err)
	}
	o, _ := labQueue(t, labReqID)
	if _, err := compare.RunLabQueue(context.Background(), o, labNowFn, labCompareWithReport(t, o), aianalysis.LabAnalyzer(model, name, labNowFn)); err != nil {
		t.Fatal(err)
	}
	r := labRequest(t, o, labReqID)
	if r.Status != compare.StatusDone || r.Note != compare.AnalysisNotWritten+aianalysis.ErrNoLabKey.Error() {
		t.Errorf("request without a key: %+v", r)
	}
	if _, js := labAnalysisFiles(o); fileExists(js) {
		t.Error("no analysis without a key")
	}
}

// Scenario: a failed or expired request is not analysed (nor an orphaned one).
func TestLabAnalysis_FailedOrExpiredNotAnalysed(t *testing.T) {
	old, orphan := "20261103T230000Z-00000003", "20261104T220000Z-00000004"
	o, fixture := labQueue(t, labReqID)
	stale := bytes.ReplaceAll(bytes.ReplaceAll(fixture, []byte(labReqID), []byte(old)), []byte("2026-11-04T23:00:00.000Z"), []byte("2026-11-03T23:00:00.000Z"))
	_ = os.WriteFile(filepath.Join(o.QueueDir, old+".json"), stale, 0o600)
	_ = os.WriteFile(filepath.Join(o.QueueDir, orphan+".json"),
		bytes.Replace(bytes.ReplaceAll(fixture, []byte(labReqID), []byte(orphan)), []byte(`"queued"`), []byte(`"running"`), 1), 0o600)
	never := func(context.Context, string) error {
		t.Error("the analyzer must not be called")
		return nil
	}
	failing := func(context.Context, []string) (string, error) { return "", errors.New("backtest data missing") }
	sum, err := compare.RunLabQueue(context.Background(), o, labNowFn, failing, never)
	if err != nil {
		t.Fatal(err)
	}
	if sum != (compare.LabRunSummary{Failed: 1, Expired: 1, Orphaned: 1}) {
		t.Errorf("summary %+v", sum)
	}
	for _, id := range []string{labReqID, old, orphan} {
		if r := labRequest(t, o, id); r.Note != "" {
			t.Errorf("%s has a note: %+v", id, r)
		}
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
