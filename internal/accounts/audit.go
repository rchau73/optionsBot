package accounts

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// DefaultAuditPath is the key lifecycle's audit trail in an environment's
// clone (data/ is gitignored): one JSON line per action that changed a key.
const DefaultAuditPath = "data/accounts_audit.jsonl"

// Key actions (AuditLine.Action).
const (
	ActionKeyAdd    = "key_add"
	ActionKeyRotate = "key_rotate"
	ActionKeyRevoke = "key_revoke"
)

// AuditLine is one action of the key lifecycle: who, which account, what.
// It holds no key, key id, client id or scope.
type AuditLine struct {
	Time      time.Time `json:"time"`
	Actor     string    `json:"actor"` // the OS user who ran the command
	AccountID string    `json:"account_id"`
	Action    string    `json:"action"`
}

// OpenAudit opens the audit trail at path for appending, created owner-only;
// an existing file readable by group or others is refused. The command opens
// it before changing anything, so an action that cannot be audited is not done.
func OpenAudit(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, fmt.Errorf("accounts: audit: create %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, fileMode)
	if err != nil {
		return nil, fmt.Errorf("accounts: audit: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("accounts: audit: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		file.Close()
		return nil, fmt.Errorf("%w (%s is %04o)", ErrPermissions, path, info.Mode().Perm())
	}
	return file, nil
}

// WriteAudit appends line to w as one JSON line.
func WriteAudit(w io.Writer, line AuditLine) error {
	raw, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("accounts: audit: encode: %w", err)
	}
	if _, err := w.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("accounts: audit: write: %w", err)
	}
	return nil
}
