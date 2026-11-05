package accounts

import (
	"encoding/json"
	"fmt"
	"time"
)

// DefaultStatusPath is where an environment's clone keeps the status file
// when cmd/accounts -check is given -out (data/ is gitignored). Nothing
// writes it unless asked.
const DefaultStatusPath = "data/accounts_status.json"

// statusVersion is the status document's shape; a field change bumps it.
const statusVersion = 1

// StatusDocument is cmd/accounts -check -json (#655): the registry and each
// account's key status as data, for the monitor's per-account view
// (docs/account-isolation.md §4 item 7). The field list is fixed: no key,
// key id, client id, scope string, money, position or personal field, and
// no free-text reason (a reason or a rule's message may quote a key id).
type StatusDocument struct {
	Version    int               `json:"version"`
	CheckedAt  time.Time         `json:"checked_at"`
	Accounts   []StatusAccount   `json:"accounts"`   // empty when the registry breaks a rule
	Violations []StatusViolation `json:"violations"` // empty when it is valid
}

// StatusAccount is one account of the document.
type StatusAccount struct {
	AccountID   string `json:"account_id"`
	Label       string `json:"label"`
	Exchange    string `json:"exchange"`
	Environment string `json:"environment"`
	Bots        []Bot  `json:"bots"`
	KeyStatus   string `json:"key_status"` // Key*: ok | missing | revoked | can_move_funds | unknown_scope
}

// StatusViolation is one rule break: the rule's name and its account
// ("" for a file or customer break, or an account whose id is not opaque).
type StatusViolation struct {
	Rule      string `json:"rule"`
	AccountID string `json:"account_id,omitempty"`
}

// NewStatusDocument builds the document checked at now: the accounts of reg
// (nil when the registry is invalid) and the rule breaks vs. Pure.
func NewStatusDocument(now time.Time, reg *Registry, vs []Violation) StatusDocument {
	doc := StatusDocument{Version: statusVersion, CheckedAt: now.UTC(), Accounts: []StatusAccount{}, Violations: []StatusViolation{}}
	if reg != nil {
		for _, a := range reg.accounts {
			doc.Accounts = append(doc.Accounts, StatusAccount{
				AccountID: a.AccountID, Label: a.Label, Exchange: a.Exchange, Environment: a.Environment,
				Bots: append([]Bot{}, a.Bots...), KeyStatus: a.KeyState,
			})
		}
	}
	for _, v := range vs {
		doc.Violations = append(doc.Violations, StatusViolation{Rule: v.Rule, AccountID: v.AccountID})
	}
	return doc
}

// Encode is the document as indented JSON with a final newline.
func (d StatusDocument) Encode() ([]byte, error) {
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("accounts: encode status: %w", err)
	}
	return append(raw, '\n'), nil
}

// WriteStatusFile replaces path with the document atomically (temp file in
// the same folder, then rename), owner-only: a reader sees the old document
// or the new one, never a part.
func WriteStatusFile(path string, d StatusDocument) error {
	raw, err := d.Encode()
	if err != nil {
		return err
	}
	return writeAtomic(path, ".accounts_status-*.tmp", raw)
}
