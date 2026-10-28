package tests

import (
	"path/filepath"
	"testing"

	"optionsbot/internal/backup"
)

// Scenario: the log is backed up — Check reports a copy of assistant.jsonl
// with an unparseable line (#438).
func TestBackupCheckLines_UnparseableLineFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assistant.jsonl")
	writeBackupFile(t, path, `{"kind":"answer","id":"a"}`+"\nnot json\n"+`{"kind":"flag","id":"a"}`+"\n")
	if _, err := backup.CheckLines(path); err == nil {
		t.Fatal("CheckLines accepted a copy with an unparseable line")
	}
}

func TestBackupCheckLines_GoodAndTornLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assistant.jsonl")
	writeBackupFile(t, path, `{"kind":"answer","id":"a"}`+"\n\n"+`{"kind":"flag","id":"a"}`+"\n"+`{"kind":"ans`)
	res, err := backup.CheckLines(path)
	if err != nil {
		t.Fatalf("a torn last line must be allowed: %v", err)
	}
	if res.Lines != 2 || res.Skipped != 1 {
		t.Fatalf("got %+v, want 2 lines and 1 skipped", res)
	}
}

func TestBackupCheckLines_MissingFileIsEmpty(t *testing.T) {
	res, err := backup.CheckLines(filepath.Join(t.TempDir(), "none.jsonl"))
	if err != nil || res.Lines != 0 {
		t.Fatalf("got %+v, %v", res, err)
	}
}
