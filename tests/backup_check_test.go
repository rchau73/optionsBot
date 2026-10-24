package tests

import (
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
