package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/aianalysis"
	"optionsbot/internal/compare"
)

// #426: cmd/analyze — the generator around the #257 checker. Fake models
// only; no test calls the API.

type fakeAnalysisModel struct {
	answer       string
	system, user string
	calls        int
}

func (f *fakeAnalysisModel) Analyze(_ context.Context, system, user string) (string, error) {
	f.calls++
	f.system, f.user = system, user
	return f.answer, nil
}

// analyzeFixture copies the saved comparison report into a fresh folder and
// returns its path and the fixture's good answer.
func analyzeFixture(t *testing.T) (string, string) {
	t.Helper()
	report, err := os.ReadFile("testdata/aianalysis/report.json")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := os.ReadFile("testdata/aianalysis/answer.md")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "20261026T000000Z", "report.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, report, 0o644); err != nil {
		t.Fatal(err)
	}
	return path, string(answer)
}

func runAnalyze(t *testing.T, path, answer string) (string, string, []aianalysis.Violation, error, *fakeAnalysisModel) {
	t.Helper()
	m := &fakeAnalysisModel{answer: answer}
	md, js, vs, err := aianalysis.Run(context.Background(), aianalysis.Options{
		ReportPath: path, Model: m, ModelName: "fake", Now: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)})
	return md, js, vs, err, m
}

// Scenario: a comparison report and a fake model that cites only numbers in
// the report → analysis.md and analysis.json written, the Disclaimer first.
func TestAnalyze_GroundedAnswerIsWrittenDisclaimerFirst(t *testing.T) {
	path, answer := analyzeFixture(t)
	md, js, vs, err, m := runAnalyze(t, path, answer)
	if err != nil || len(vs) != 0 {
		t.Fatalf("err %v, violations %v", err, vs)
	}
	if m.calls != 1 || m.system != aianalysis.Prompt || !strings.Contains(m.user, `"gamma_flip_buffer_sd"`) {
		t.Fatalf("the model got the prompt and the report: calls %d", m.calls)
	}
	dir := filepath.Dir(path)
	if md != filepath.Join(dir, "analysis.md") || js != filepath.Join(dir, "analysis.json") {
		t.Fatalf("written beside the report: %s %s", md, js)
	}
	text, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(text), "\n")
	if first != "> **"+compare.Disclaimer+"**" {
		t.Fatalf("first line %q", first)
	}
	if !strings.Contains(string(text), aianalysis.Label) {
		t.Fatal("the AI label is missing")
	}
	var a aianalysis.Analysis
	raw, err := os.ReadFile(js)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.Text, compare.Disclaimer) || a.Disclaimer != compare.Disclaimer || a.Report != "report.json" || a.Model != "fake" {
		t.Fatalf("analysis.json: %+v", a)
	}
	if len(a.Sections) != 4 {
		t.Fatalf("sections: %+v", a.Sections)
	}
	for i, s := range a.Sections {
		if s.Title != aianalysis.Sections[i] || s.Text == "" {
			t.Fatalf("section %d: %+v", i, s)
		}
	}
	if got := aianalysis.Check(raw0(t, path), a.Text); len(got) != 0 {
		t.Fatalf("the written text fails the checker: %v", got)
	}
}

func raw0(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Scenario: a fake model answer with a number not in the report → nothing
// written, non-zero exit (Run's ErrRejected; cmd/analyze exits 1 on it).
func TestAnalyze_InventedNumberWritesNothing(t *testing.T) {
	path, answer := analyzeFixture(t)
	bad := strings.Replace(answer, "2.11 % against 1.84 %", "2.31 % against 1.84 %", 1)
	md, js, vs, err, _ := runAnalyze(t, path, bad)
	if !errors.Is(err, aianalysis.ErrRejected) || md != "" || js != "" {
		t.Fatalf("err %v md %q js %q", err, md, js)
	}
	if len(vs) != 1 || vs[0].Kind != aianalysis.KindNumber || vs[0].Text != "2.31" {
		t.Fatalf("violations: %v", vs)
	}
	assertNothingWritten(t, path)
}

func TestAnalyze_AdviceAndMissingSectionWriteNothing(t *testing.T) {
	path, answer := analyzeFixture(t)
	for name, bad := range map[string]string{
		"advice":   answer + "\nYou should apply this change.\n",
		"no label": strings.Replace(answer, aianalysis.Label, "", 1),
		"section":  strings.Replace(answer, "## What to test next", "## Next", 1),
		"order":    strings.Replace(strings.Replace(answer, "## What changed", "## TMP", 1), "## What to test next", "## What changed", 1),
	} {
		_, _, vs, err, _ := runAnalyze(t, path, bad)
		if !errors.Is(err, aianalysis.ErrRejected) || len(vs) == 0 {
			t.Errorf("%s: err %v violations %v", name, err, vs)
		}
	}
	assertNothingWritten(t, path)
}

func assertNothingWritten(t *testing.T, path string) {
	t.Helper()
	for _, f := range []string{"analysis.md", "analysis.json"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), f)); !os.IsNotExist(err) {
			t.Fatalf("%s was written", f)
		}
	}
}

// Scenario: a report with NotModelled keys → those keys are named in the
// fragile section, added when the model left them out, never twice.
func TestAnalyze_NotModelledKeysNamedInFragileSection(t *testing.T) {
	path, answer := analyzeFixture(t)
	if strings.Contains(sectionText(aianalysis.SplitSections(answer), "Where it is fragile"), "repair_cooldown_hours") {
		t.Fatal("fixture: the answer must leave the key out of the fragile section")
	}
	_, js, _, err, _ := runAnalyze(t, path, answer)
	if err != nil {
		t.Fatal(err)
	}
	var a aianalysis.Analysis
	if err := json.Unmarshal(raw0(t, js), &a); err != nil {
		t.Fatal(err)
	}
	fragile := sectionText(a.Sections, "Where it is fragile")
	if !strings.Contains(fragile, "`repair_cooldown_hours`") || !strings.Contains(fragile, "stress does not model") {
		t.Fatalf("fragile section: %q", fragile)
	}
	if strings.Contains(sectionText(a.Sections, "What to test next"), "repair_cooldown_hours") {
		t.Fatal("the sentence went into the wrong section")
	}
	if len(a.NotModelled) != 1 || a.NotModelled[0] != "repair_cooldown_hours" {
		t.Fatalf("not_modelled: %v", a.NotModelled)
	}

	named := aianalysis.NameNotModelled("## Where it is fragile\nThe stress ignores repair_cooldown_hours.\n", []string{"repair_cooldown_hours"})
	if strings.Count(named, "repair_cooldown_hours") != 1 {
		t.Fatalf("named twice: %q", named)
	}
	if got := aianalysis.NameNotModelled("no sections", []string{"k"}); got != "no sections" {
		t.Fatalf("no fragile section: %q", got)
	}
}

func sectionText(ss []aianalysis.Section, title string) string {
	for _, s := range ss {
		if s.Title == title {
			return s.Text
		}
	}
	return ""
}

// Scenario: no ANTHROPIC_API_KEY, no -fake → a clear stop, nothing sent.
func TestAnalyze_NoAPIKeyStopsBeforeSending(t *testing.T) {
	m, _, err := aianalysis.ModelFor("", "")
	if !errors.Is(err, aianalysis.ErrNoAPIKey) || m != nil {
		t.Fatalf("model %v err %v", m, err)
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("message: %v", err)
	}
	if m, name, err := aianalysis.ModelFor("", "k"); err != nil || name != aianalysis.Model {
		t.Fatalf("with a key: %v %s %v", m, name, err)
	}
	path, answer := analyzeFixture(t)
	fake := filepath.Join(t.TempDir(), "answer.md")
	if err := os.WriteFile(fake, []byte(answer), 0o644); err != nil {
		t.Fatal(err)
	}
	m, name, err := aianalysis.ModelFor(fake, "")
	if err != nil || name != "fake:answer.md" {
		t.Fatalf("fake: %s %v", name, err)
	}
	if _, _, vs, err := aianalysis.Run(context.Background(), aianalysis.Options{ReportPath: path, Model: m, ModelName: name, Now: time.Now()}); err != nil || len(vs) != 0 {
		t.Fatalf("-fake run: %v %v", err, vs)
	}
}

func TestAnalyze_BadReportAndModelError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, _, err, m := runAnalyze(t, path, "x")
	if err == nil || errors.Is(err, aianalysis.ErrRejected) || m.calls != 0 {
		t.Fatalf("bad report: err %v, calls %d", err, m.calls)
	}
	if _, _, _, err, _ := runAnalyze(t, filepath.Join(dir, "absent.json"), "x"); err == nil {
		t.Fatal("absent report accepted")
	}
}

// The Messages API client: request shape, the key in its header only, reply
// text joined; an error status and a refusal are errors.
func TestAnalyze_AnthropicClient(t *testing.T) {
	var got struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    string `json:"system"`
		Messages  []struct{ Role, Content string }
	}
	var key, version string
	status, reply := http.StatusOK, `{"stop_reason":"end_turn","content":[{"type":"text","text":"part one "},{"type":"text","text":"part two"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, version = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	defer srv.Close()
	c := &aianalysis.Anthropic{APIKey: "test-key", Model: aianalysis.Model, URL: srv.URL}
	text, err := c.Analyze(context.Background(), "sys", "user turn")
	if err != nil || text != "part one part two" {
		t.Fatalf("text %q err %v", text, err)
	}
	if key != "test-key" || version != aianalysis.AnthropicVersion || got.Model != aianalysis.Model || got.System != "sys" ||
		got.MaxTokens <= 0 || len(got.Messages) != 1 || got.Messages[0].Role != "user" || got.Messages[0].Content != "user turn" {
		t.Fatalf("request: key %q version %q %+v", key, version, got)
	}
	status = http.StatusTooManyRequests
	if _, err := c.Analyze(context.Background(), "s", "u"); err == nil || strings.Contains(err.Error(), "test-key") {
		t.Fatalf("error status: %v", err)
	}
	status, reply = http.StatusOK, `{"stop_reason":"refusal","content":[]}`
	if _, err := c.Analyze(context.Background(), "s", "u"); err == nil {
		t.Fatal("a refusal is an error")
	}
}

// The saved example (docs/product/lab/example-analysis.md) is the fixture's
// analysis: it passes the checker on the fixture report and carries
// percentages and counts only (no coin or USD amount).
func TestAnalyze_SavedExamplePasses(t *testing.T) {
	doc, err := os.ReadFile("../docs/product/lab/example-analysis.md")
	if err != nil {
		t.Fatal(err)
	}
	report := raw0(t, "testdata/aianalysis/report.json")
	body := string(doc)
	start := strings.Index(body, "> **"+compare.Disclaimer+"**")
	end := strings.Index(body, "\n---\n")
	if start < 0 || end < start {
		t.Fatal("the example must hold an analysis.md: the Disclaimer line, the text, then the --- footer")
	}
	text := compare.Disclaimer + "\n\n" + strings.TrimSpace(body[start+len("> **"+compare.Disclaimer+"**"):end])
	if vs := aianalysis.Check(report, text); len(vs) != 0 {
		t.Fatalf("the saved example fails the checker: %v", vs)
	}
	path, answer := analyzeFixture(t)
	md, _, _, err, _ := runAnalyze(t, path, answer)
	if err != nil {
		t.Fatal(err)
	}
	written, _, _ := strings.Cut(string(raw0(t, md)), "\n---\n")
	if want := strings.TrimSpace(body[start:end]); strings.TrimSpace(written) != want {
		t.Fatalf("the saved example is not what cmd/analyze writes for the fixture:\n%s", written)
	}
	for _, unit := range []string{"USD", "$", " BTC", "coin"} {
		if strings.Contains(text, unit) {
			t.Errorf("percentages only: the example names %q", unit)
		}
	}
}
