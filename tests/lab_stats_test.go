package tests

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/compare"
)

// The lab's usage report (#824, cmd/labstats): one test per scenario of the
// ticket, with the queue built in a temporary COMPARE_DIR.

var labStatsNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// labStatsReq is a request file, made minutes after the start of 2026-11-16
// (ISO week 2026-W47).
type labStatsReq struct {
	n         int
	requester string
	status    string
	minutes   int    // from requested_at to updated_at
	runID     string // a done run's report folder
	note      string
}

func labStatsDir(t *testing.T, reqs ...labStatsReq) string {
	t.Helper()
	dir := t.TempDir()
	q := filepath.Join(dir, "requests")
	if err := os.MkdirAll(q, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, r := range reqs {
		at := time.Date(2026, 11, 16, 8, r.n, 0, 0, time.UTC)
		lr := compare.LabRequest{
			ID:          fmt.Sprintf("%sZ-%08x", at.Format("20060102T150405"), r.n),
			Requester:   r.requester,
			RequestedAt: at,
			Underlying:  "BTC",
			Settings:    []compare.LabSetting{{Key: "gamma_flip_buffer_sd", Value: 1.2345}},
			Status:      r.status,
			UpdatedAt:   at.Add(time.Duration(r.minutes) * time.Minute),
			RunID:       r.runID,
			Note:        r.note,
		}
		if err := compare.SaveLabRequest(q, lr); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func labStatsWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func labStatsRun(t *testing.T, dir string) (compare.LabStats, string, string) {
	t.Helper()
	f, err := compare.ReadLabUsage(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := compare.ComputeLabStats(f, labStatsNow, compare.LabStatsDays)
	js, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return s, compare.LabStatsMarkdown(s), string(js)
}

func TestLabStats_OutcomesAndTimeToFinishedRun(t *testing.T) {
	dir := labStatsDir(t,
		labStatsReq{n: 1, requester: "ana", status: compare.StatusDone, minutes: 10, runID: "20261116T081000Z"},
		labStatsReq{n: 2, requester: "ana", status: compare.StatusDone, minutes: 20, runID: "20261116T082200Z"},
		labStatsReq{n: 3, requester: "ana", status: compare.StatusFailed, minutes: 5},
		labStatsReq{n: 4, requester: "ana", status: compare.StatusExpired, minutes: 1500},
	)
	s, md, _ := labStatsRun(t, dir)
	if s.Requests != 4 || s.DonePct != 50 || s.FailedPct != 25 || s.ExpiredPct != 25 {
		t.Fatalf("requests %d, done %v %%, failed %v %%, expired %v %%; want 4, 50, 25, 25", s.Requests, s.DonePct, s.FailedPct, s.ExpiredPct)
	}
	if s.MedianMinutes == nil || *s.MedianMinutes != 15 {
		t.Fatalf("median minutes %v, want 15", s.MedianMinutes)
	}
	for _, want := range []string{"| Requests | 4 |", "| Done | 2 (50 %) |", "| Failed | 1 (25 %) |", "| Expired | 1 (25 %) |",
		"| Median time to a finished run | 15 min |", "done share >= 90 %: not met", "median time to a finished run <= 30 min: met"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

func TestLabStats_UsePerManager(t *testing.T) {
	dir := labStatsDir(t,
		labStatsReq{n: 1, requester: "alice.manager", status: compare.StatusDone, minutes: 10},
		labStatsReq{n: 2, requester: "alice.manager", status: compare.StatusQueued},
		labStatsReq{n: 3, requester: "alice.manager", status: compare.StatusFailed},
		labStatsReq{n: 4, requester: "bob.manager", status: compare.StatusDone, minutes: 10},
	)
	s, md, js := labStatsRun(t, dir)
	want := []compare.LabManagerWeek{
		{Week: "2026-W47", Manager: compare.ManagerHash("alice.manager"), Requests: 3},
		{Week: "2026-W47", Manager: compare.ManagerHash("bob.manager"), Requests: 1},
	}
	if fmt.Sprint(s.PerManagerWeek) != fmt.Sprint(want) || s.Managers != 2 {
		t.Fatalf("per manager %v (%d managers), want %v", s.PerManagerWeek, s.Managers, want)
	}
	for _, w := range want {
		if len(w.Manager) != 8 {
			t.Errorf("hash %q is not 8 characters", w.Manager)
		}
		if !strings.Contains(md, fmt.Sprintf("| %s | %s | %d |", w.Week, w.Manager, w.Requests)) {
			t.Errorf("markdown lacks the row of %s:\n%s", w.Manager, md)
		}
	}
	for _, name := range []string{"alice", "bob"} {
		if strings.Contains(md, name) || strings.Contains(js, name) {
			t.Errorf("the report names %s", name)
		}
	}
}

func TestLabStats_RecordedChanges(t *testing.T) {
	dir := labStatsDir(t,
		labStatsReq{n: 1, requester: "ana", status: compare.StatusDone, minutes: 10, runID: "20261116T081000Z"},
		labStatsReq{n: 2, requester: "ana", status: compare.StatusDone, minutes: 10, runID: "20261116T082000Z", note: "analysis not written: no key"},
	)
	labStatsWrite(t, filepath.Join(dir, "applied", "20261116T081000Z.json"), `{"run_id":"20261116T081000Z","person":"ana"}`)
	labStatsWrite(t, filepath.Join(dir, "candidates", "20261116T081000Z", compare.CandidateStatusFile),
		`{"status":"refused","needs_product_owner":true}`)
	// Both runs have an analysis file; only the one without a note counts.
	labStatsWrite(t, filepath.Join(dir, "20261116T081000Z", "analysis.json"), "{}")
	labStatsWrite(t, filepath.Join(dir, "20261116T082000Z", "analysis.json"), "{}")
	s, md, _ := labStatsRun(t, dir)
	if s.Recorded != 1 || s.Done != 2 || s.RecordedPctOfDone != 50 {
		t.Fatalf("recorded %d of %d (%v %%), want 1 of 2 (50 %%)", s.Recorded, s.Done, s.RecordedPctOfDone)
	}
	if s.CorePolicyRefused != 1 || s.AnalysesWritten != 1 {
		t.Fatalf("core policy refused %d, analyses %d; want 1 and 1", s.CorePolicyRefused, s.AnalysesWritten)
	}
	for _, want := range []string{"| Done runs with a recorded change | 1 of 2 (50 %) |",
		"| Recorded changes refused as a core policy | 1 |", "| AI analyses written (withheld until L10) | 1 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// #890 "Scenario: the usage report counts decisions": 4 done runs, 1 applied
// and 1 dropped → 1 recorded change, 1 dropped, 2 of 4 with a decision.
func TestLabStats_CountsDecisions(t *testing.T) {
	runs := []string{"20261116T081000Z", "20261116T082000Z", "20261116T083000Z", "20261116T084000Z"}
	var reqs []labStatsReq
	for i, id := range runs {
		reqs = append(reqs, labStatsReq{n: i + 1, requester: "Maria Silva", status: compare.StatusDone, minutes: 10, runID: id})
	}
	dir := labStatsDir(t, reqs...)
	labStatsWrite(t, filepath.Join(dir, "applied", runs[0]+".json"), `{"run_id":"`+runs[0]+`","person":"Maria Silva"}`)
	labStatsWrite(t, filepath.Join(dir, "dropped", runs[1]+".json"),
		`{"run_id":"`+runs[1]+`","person":"`+compare.ManagerHash("Maria Silva")+`","account":{"underlying":"BTC","config":"config_btc.yaml"}}`)
	// A file not named by a run id is counted apart, never as a decision.
	labStatsWrite(t, filepath.Join(dir, "dropped", "not-a-run.json"), "{}")
	s, md, js := labStatsRun(t, dir)
	if s.Done != 4 || s.Recorded != 1 || s.Dropped != 1 || s.Decided != 2 || s.DecidedPctOfDone != 50 || s.DroppedUnreadable != 1 {
		t.Fatalf("done %d, recorded %d, dropped %d, decided %d (%v %%), unreadable %d; want 4, 1, 1, 2 (50 %%), 1",
			s.Done, s.Recorded, s.Dropped, s.Decided, s.DecidedPctOfDone, s.DroppedUnreadable)
	}
	for _, want := range []string{"| Done runs with a recorded change | 1 of 4 (25 %) |", "| Done runs with a dropped proposal | 1 |",
		"| Done runs ending in a decision (recorded or dropped) | 2 of 4 (50 %) |", "Dropped-proposal files not named by a run id: 1."} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	for _, key := range []string{`"dropped_proposals":1`, `"decided":2`, `"decided_pct_of_done":50`} {
		if !strings.Contains(js, key) {
			t.Errorf("json lacks %s: %s", key, js)
		}
	}
	// No values, names or accounts.
	for _, leak := range []string{"Maria", "Silva", "config_btc", "BTC", "20261116T08"} {
		if strings.Contains(md, leak) || strings.Contains(js, leak) {
			t.Errorf("the report carries %q", leak)
		}
	}
}

// A run with both a record and a drop file (written by hand) is one decision, the recorded change.
func TestLabStats_RecordedAndDroppedCountOnce(t *testing.T) {
	id := "20261116T081000Z"
	dir := labStatsDir(t, labStatsReq{n: 1, requester: "ana", status: compare.StatusDone, minutes: 10, runID: id})
	labStatsWrite(t, filepath.Join(dir, "applied", id+".json"), "{}")
	labStatsWrite(t, filepath.Join(dir, "dropped", id+".json"), "{}")
	s, _, _ := labStatsRun(t, dir)
	if s.Recorded != 1 || s.Dropped != 0 || s.Decided != 1 {
		t.Fatalf("recorded %d, dropped %d, decided %d; want 1, 0, 1", s.Recorded, s.Dropped, s.Decided)
	}
}

func TestLabStats_NothingInTheWindow(t *testing.T) {
	for name, dir := range map[string]string{"no folder": filepath.Join(t.TempDir(), "absent"), "empty queue": labStatsDir(t)} {
		s, md, _ := labStatsRun(t, dir)
		if s.Requests != 0 || !strings.Contains(md, compare.LabNoRequests) {
			t.Errorf("%s: %d requests, markdown:\n%s", name, s.Requests, md)
		}
		for _, tg := range s.Targets {
			if tg.Met != nil {
				t.Errorf("%s: target %s judged on no data", name, tg.Name)
			}
		}
	}
	// A request older than the window is not counted.
	dir := labStatsDir(t, labStatsReq{n: 1, requester: "ana", status: compare.StatusDone, minutes: 10})
	f, err := compare.ReadLabUsage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := compare.ComputeLabStats(f, labStatsNow.AddDate(0, 0, 40), compare.LabStatsDays); s.Requests != 0 {
		t.Errorf("a 40-day-old request counted in a 28-day window")
	}
}

func TestLabStats_NoSensitiveData(t *testing.T) {
	dir := labStatsDir(t,
		labStatsReq{n: 1, requester: "Maria Silva", status: compare.StatusDone, minutes: 12, runID: "20261116T081000Z"},
		labStatsReq{n: 2, requester: "Maria Silva", status: compare.StatusFailed},
	)
	labStatsWrite(t, filepath.Join(dir, "applied", "20261116T081000Z.json"),
		`{"run_id":"20261116T081000Z","person":"Maria Silva","account":{"underlying":"BTC","config":"config_btc.yaml"},"changes":[{"key":"gamma_flip_buffer_sd","old":1.5,"new":1.2345}]}`)
	_, md, js := labStatsRun(t, dir)
	for _, leak := range []string{"Maria", "Silva", "1.2345", "gamma_flip_buffer_sd", "config_btc", "BTC", "20261116T08"} {
		if strings.Contains(md, leak) || strings.Contains(js, leak) {
			t.Errorf("the report carries %q:\nmd %s\njson %s", leak, md, js)
		}
	}
}

func TestLabStats_UnreadableFile(t *testing.T) {
	dir := labStatsDir(t,
		labStatsReq{n: 1, requester: "ana", status: compare.StatusDone, minutes: 10},
		labStatsReq{n: 2, requester: "ana", status: compare.StatusFailed},
	)
	labStatsWrite(t, filepath.Join(dir, "requests", "20261116T090000Z-deadbeef.json"), "{not json")
	s, md, _ := labStatsRun(t, dir)
	if s.Unreadable != 1 || s.Requests != 2 {
		t.Fatalf("unreadable %d, requests %d; want 1 and 2", s.Unreadable, s.Requests)
	}
	if !strings.Contains(md, "Unreadable request files (counted in no figure above): 1.") {
		t.Errorf("markdown does not count the unreadable file:\n%s", md)
	}
	// An empty window still says it.
	empty := labStatsDir(t)
	labStatsWrite(t, filepath.Join(empty, "requests", "20261116T090000Z-deadbeef.json"), "")
	if _, md, _ := labStatsRun(t, empty); !strings.Contains(md, compare.LabNoRequests) || !strings.Contains(md, ": 1.") {
		t.Errorf("empty window with an unreadable file:\n%s", md)
	}
}

func TestLabStats_ReadOnly(t *testing.T) {
	// The report reads files only: no write, network or exec package.
	for _, file := range []string{"../internal/compare/labstats.go", "../cmd/labstats/main.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, "net") || p == "os/exec" || strings.Contains(p, "exchange") || strings.Contains(p, "gateway") {
				t.Errorf("%s imports %s", file, p)
			}
		}
	}
	src, err := os.ReadFile("../internal/compare/labstats.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"os.WriteFile", "os.Create", "os.Remove", "os.Rename", "os.Mkdir", "SaveLabRequest("} {
		if strings.Contains(string(src), w) {
			t.Errorf("labstats.go calls %s", w)
		}
	}
}
