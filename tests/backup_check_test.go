package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/backup"
)

// journalLines returns n journal lines of a non-trade event (decoded by
// event and time only, like most of orders.log).
func journalLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(`{"timestamp":"2026-10-23T00:00:00Z","event":"skip","detail":"x"}` + "\n")
	}
	return b.String()
}

func writeBackupFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

var backupNow = time.Date(2026, 10, 24, 0, 6, 0, 0, time.UTC)

// Scenario: A nightly backup is made — the copy replays and its result is
// recorded for the next night.
func TestBackupCheck_GoodCopyReplaysAndRecords(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, filepath.Join(dir, "orders.log"), journalLines(5))
	writeBackupFile(t, filepath.Join(dir, "archive", "20261001T000000Z", "orders.log"), journalLines(3))

	res, err := backup.Check(dir, "", backupNow)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Events != 5 || res.ArchivedEvents != 3 || res.Total() != 8 || res.Skipped != 0 {
		t.Fatalf("result = %+v, want 5 events + 3 archived", res)
	}
	if err := backup.WriteResult(dir, res); err != nil {
		t.Fatal(err)
	}
	got, ok, err := backup.ReadResult(dir)
	if err != nil || !ok || got.Total() != 8 || !got.CheckedAt.Equal(backupNow) {
		t.Fatalf("ReadResult = %+v, %v, %v", got, ok, err)
	}
}

// Scenario: A corrupt journal is caught — an unreadable line in the middle.
func TestBackupCheck_CorruptJournalFails(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, filepath.Join(dir, "orders.log"), journalLines(2)+"garbage not json\n"+journalLines(2))
	if _, err := backup.Check(dir, "", backupNow); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("Check err = %v, want unreadable lines", err)
	}
}

// A corrupt archived journal is caught too.
func TestBackupCheck_CorruptArchiveFails(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, filepath.Join(dir, "orders.log"), journalLines(2))
	writeBackupFile(t, filepath.Join(dir, "archive", "a", "orders.log"), `{"event":"filled","timestamp":"x",broken}`+"\n")
	if _, err := backup.Check(dir, "", backupNow); err == nil {
		t.Fatal("Check passed a corrupt archived journal")
	}
}

// A copy that raced an append holds a torn last line: allowed, not a failure.
func TestBackupCheck_TornLastLineAllowed(t *testing.T) {
	dir := t.TempDir()
	writeBackupFile(t, filepath.Join(dir, "orders.log"), journalLines(3)+`{"timestamp":"2026-10-23T00:00:00Z","ev`)
	res, err := backup.Check(dir, "", backupNow)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Events != 3 || res.Skipped != 1 {
		t.Fatalf("result = %+v, want 3 events, 1 torn line", res)
	}
}

// Scenario: A corrupt journal is caught — fewer events than the day before.
func TestBackupCheck_ShrunkJournalFails(t *testing.T) {
	prev, dir := t.TempDir(), t.TempDir()
	if err := backup.WriteResult(prev, backup.Result{Events: 10}); err != nil {
		t.Fatal(err)
	}
	writeBackupFile(t, filepath.Join(dir, "orders.log"), journalLines(4))
	if _, err := backup.Check(dir, prev, backupNow); err == nil || !strings.Contains(err.Error(), "shrank") {
		t.Fatalf("Check err = %v, want journal shrank", err)
	}
}

// A lost journal (no orders.log where the day before had events) fails.
func TestBackupCheck_MissingJournalAfterGoodOneFails(t *testing.T) {
	prev, dir := t.TempDir(), t.TempDir()
	if err := backup.WriteResult(prev, backup.Result{Events: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Check(dir, prev, backupNow); err == nil {
		t.Fatal("Check passed a missing journal after a non-empty one")
	}
}

// --reset-history moves the journal into archive/: the total is kept, so a
// reset is not reported as a loss.
func TestBackupCheck_ResetHistoryIsNotALoss(t *testing.T) {
	prev, dir := t.TempDir(), t.TempDir()
	if err := backup.WriteResult(prev, backup.Result{Events: 6}); err != nil {
		t.Fatal(err)
	}
	writeBackupFile(t, filepath.Join(dir, "orders.log"), journalLines(1))
	writeBackupFile(t, filepath.Join(dir, "archive", "20261023T120000Z", "orders.log"), journalLines(6))
	if _, err := backup.Check(dir, prev, backupNow); err != nil {
		t.Fatalf("Check after a reset: %v", err)
	}
}

// copyAuditFixture copies tests/testdata/audit_chain (an archive with two
// chained lines, audit.jsonl with two more: written as the monitor writes
// them, and verified by the Audit page's check in frontend
// __tests__/audit-chain.test.js) into a temporary folder.
func copyAuditFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"audit.20261104T031433123Z.jsonl", "audit.jsonl"} {
		b, err := os.ReadFile(filepath.Join("testdata", "audit_chain", name))
		if err != nil {
			t.Fatal(err)
		}
		writeBackupFile(t, filepath.Join(dir, name), string(b))
	}
	return dir
}

// sealAuditLine is frontend/lib/auth/auditchain.js sealLine for an entry
// given as its JSON fields without braces.
func sealAuditLine(fields, prev string) string {
	body := "{" + fields + `,"prev":"` + prev + `"}`
	sum := sha256.Sum256([]byte(body))
	return body[:len(body)-1] + `,"line_hash":"` + hex.EncodeToString(sum[:]) + `"}`
}

func sha256Text(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// #650 Scenario: rotated audit archives are included — the chain check on the
// copy passes across the archive and audit.jsonl; and a damaged audit chain
// fails the backup, naming the file, the line and the Audit page's reason.
func TestBackupCheck_AuditChain(t *testing.T) {
	const archive = "audit.20261104T031433123Z.jsonl"
	legacy := `{"time":"2026-10-01T00:00:00.000Z","action":"signed_in"}`
	first := sealAuditLine(`"time":"2026-11-01T00:00:00.000Z","action":"signed_in"`, sha256Text(legacy))
	cases := []struct {
		name   string
		change func(t *testing.T, dir string) // nil = the fixture as it is
		want   backup.ChainResult
		broken string // the error's text, "" = intact
	}{
		{name: "intact across the archive", want: backup.ChainResult{Checked: 4}},
		{name: "no trail at all", change: func(t *testing.T, dir string) {
			os.Remove(filepath.Join(dir, archive))
			os.Remove(filepath.Join(dir, backup.AuditFile))
		}},
		{name: "a line edited", change: func(t *testing.T, dir string) {
			editAudit(t, filepath.Join(dir, backup.AuditFile), `"username":"bob"`, `"username":"eve"`)
		}, broken: "audit.jsonl line 2: edited"},
		{name: "a line removed", change: func(t *testing.T, dir string) {
			p := filepath.Join(dir, archive)
			b, _ := os.ReadFile(p)
			writeBackupFile(t, p, strings.SplitAfter(string(b), "\n")[0])
		}, broken: "audit.jsonl line 1: link"},
		{name: "the archive is missing", change: func(t *testing.T, dir string) {
			os.Remove(filepath.Join(dir, archive))
		}, broken: "audit.jsonl line 1: link"},
		{name: "a damaged line", change: func(t *testing.T, dir string) {
			editAudit(t, filepath.Join(dir, archive), "\n{", "\nnot json\n{")
		}, broken: archive + " line 2: damaged"},
		{name: "an unchained line after the chain started", change: func(t *testing.T, dir string) {
			appendAudit(t, filepath.Join(dir, backup.AuditFile), `{"action":"signed_in"}`+"\n")
		}, broken: "audit.jsonl line 3: unchained"},
		{name: "a torn last line is skipped", change: func(t *testing.T, dir string) {
			appendAudit(t, filepath.Join(dir, backup.AuditFile), `{"time":"2026-11-05T02:00`)
		}, want: backup.ChainResult{Checked: 4, Skipped: 1}},
		{name: "a torn line inside an archive is damage", change: func(t *testing.T, dir string) {
			appendAudit(t, filepath.Join(dir, archive), `{"time":"2026-11-05T02:00`)
		}, broken: archive + " line 3: damaged"},
		{name: "legacy lines before the chain", change: func(t *testing.T, dir string) {
			os.Remove(filepath.Join(dir, archive))
			writeBackupFile(t, filepath.Join(dir, backup.AuditFile), legacy+"\n\n"+first+"\n")
		}, want: backup.ChainResult{Checked: 1, Legacy: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyAuditFixture(t)
			if tc.change != nil {
				tc.change(t, dir)
			}
			got, err := backup.CheckAuditChain(dir)
			if tc.broken != "" {
				if err == nil || !strings.Contains(err.Error(), tc.broken) {
					t.Fatalf("CheckAuditChain = %v, want an error naming %q", err, tc.broken)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckAuditChain: %v", err)
			}
			if got != tc.want {
				t.Fatalf("CheckAuditChain = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func editAudit(t *testing.T, path, old, repl string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), old) {
		t.Fatalf("%s has no %q (%v)", path, old, err)
	}
	writeBackupFile(t, path, strings.Replace(string(b), old, repl, 1))
}

func appendAudit(t *testing.T, path, text string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeBackupFile(t, path, string(b)+text)
}

// No previous check (the first night, or a previous copy without check.json):
// nothing to compare with.
func TestBackupCheck_NoPreviousResult(t *testing.T) {
	prev, dir := t.TempDir(), t.TempDir()
	writeBackupFile(t, filepath.Join(dir, "orders.log"), journalLines(1))
	if _, err := backup.Check(dir, prev, backupNow); err != nil {
		t.Fatalf("Check: %v", err)
	}
	writeBackupFile(t, filepath.Join(prev, backup.ResultFile), "{not json")
	if _, err := backup.Check(dir, prev, backupNow); err == nil {
		t.Fatal("Check accepted an unreadable previous check.json")
	}
}
