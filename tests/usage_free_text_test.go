package tests

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/usage"
)

// Scenario (#363): the usage report never sees free text. Survey answers —
// codes and the free text — are kept in the monitor's auth.db, in the
// survey_answers table; cmd/usage reads coded events from events.jsonl only.

// usageSources are the Go files the usage report is built from.
func usageSources(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"../internal/usage", "../cmd/usage"} {
		m, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(m) == 0 {
			t.Fatalf("no Go files in %s: %v", dir, err)
		}
		files = append(files, m...)
	}
	return files
}

// The report's code can reach neither the user database nor its tables: no
// SQL package, and no mention of auth.db, survey_answers or a free-text column.
func TestUsage_InputCannotIncludeSurveyAnswersTable(t *testing.T) {
	denied := []string{"survey_answers", "auth.db", "AUTH_DB_PATH", "free_text", "sqlite"}
	for _, f := range usageSources(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range denied {
			if strings.Contains(string(raw), w) {
				t.Errorf("%s mentions %q: the usage report must read coded events only, never the survey answers' table", f, w)
			}
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, raw, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(path, "database/") || strings.Contains(path, "sql") {
				t.Errorf("%s imports %s: the usage report reads events.jsonl only", f, path)
			}
		}
	}
}

// Every survey property the report accepts is an enum: no field of an event
// it reads can carry free text.
func TestUsage_SurveyPropertiesAreCodesOnly(t *testing.T) {
	for _, ev := range []string{"survey_shown", "survey_answered", "survey_dismissed"} {
		props, ok := usage.Events[ev]
		if !ok {
			t.Fatalf("%s is not in the usage plan", ev)
		}
		for _, p := range props {
			if r := usage.Properties[p]; r.Type != "enum" {
				t.Errorf("%s.%s is %q: a survey field must be a code", ev, p, r.Type)
			}
		}
	}
}

// A log from a user with free-text answers — the text sent as an event
// property, or an exported survey_answer line pasted into the file — gives a
// report with codes and counts only.
func TestUsage_ReportFromUserWithFreeTextHasCodesAndCountsOnly(t *testing.T) {
	const text = "free-text-kept-in-auth-db-xyz"
	at := usageWeek(t, "2026-W43").From.Add(30 * time.Hour)
	lines := npsAnswers(t, at, 9)
	lines = append(lines,
		usageLine(t, "survey_answered", at.Add(time.Hour), 500,
			map[string]any{"survey_id": "nps", "score": "3", "reason_code": "other", "text": text}),
		`{"record":"survey_answer","survey_id":"exit","codes":{"reason_code":"other"},"text":"`+text+`","created_at":"2026-10-21T10:00:00Z"}`,
	)
	r, written := surveyReport(t, lines)
	if strings.Contains(written, text) || strings.Contains(written, `"text"`) {
		t.Fatal("free text reached the usage report")
	}
	if r.Source.Rejected[usage.RejectProperty] != 1 || r.Source.Rejected[usage.RejectEvent] != 1 {
		t.Fatalf("rejected = %v, want the text event and the survey_answer line refused", r.Source.Rejected)
	}
	if r.Survey.NPS.Answers != 1 || r.Survey.Why["steady_income"] != 1 || r.Survey.Why["other"] != 0 {
		t.Fatalf("nps = %+v, why = %v: only the coded answer counts", r.Survey.NPS, r.Survey.Why)
	}
}
