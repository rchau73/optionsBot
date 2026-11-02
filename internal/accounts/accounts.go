// Package accounts is the platform's account registry (#585, E8,
// docs/account-isolation.md §5 item 1): which customer owns which exchange
// account, which bots run on it, and — joined to the key store
// (internal/keystore) — whether the account's key may be used. One
// owner-only JSON file per environment (data/accounts.json). It holds ids,
// labels and file names only: never a key, money, positions or personal data.
//
// Read-only for the bot: nothing in cmd/bot, the deploy tooling or
// scripts/env_lib.sh uses it yet. cmd/accounts -check prints it.
package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"optionsbot/internal/exchange"
	"optionsbot/internal/gateway"
	"optionsbot/internal/keystore"
)

// DefaultPath is the registry's file in an environment's clone (data/ is gitignored).
const DefaultPath = "data/accounts.json"

// Account status words (AccountStatus.Status).
const (
	StatusOK      = "ok"      // its key is in the store, active and trade-only
	StatusBlocked = "blocked" // its key is missing, revoked, can move funds or has an unknown scope
)

// Scope words for an account whose key is not in the store (AccountStatus.KeyScope);
// otherwise the store's gateway.KeyScope* status.
const (
	ScopeNoKey   = "no_key"  // the account names no key
	ScopeMissing = "missing" // the key id is not in the key store
)

const (
	fileVersion = 1
	fileMode    = 0o600
	dirMode     = 0o700
)

// Environments are the DERIBIT_ENV values an account may run in.
var Environments = []string{"testnet", "live"}

var (
	// ErrPermissions: the registry file is readable by group or others.
	ErrPermissions = errors.New("accounts: file is readable by group or others: chmod 600 it")
	// ErrInvalid: the registry breaks a rule (Validate); the error names every break.
	ErrInvalid = errors.New("accounts: invalid registry")

	// idPattern keeps ids opaque: lower-case letters, digits, '-' and '_' —
	// no spaces, so no name, and no '@', so no e-mail.
	idPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{2,10}$`)
)

// File is the registry on disk.
type File struct {
	Version   int        `json:"version"`
	Customers []Customer `json:"customers"`
}

// Customer owns exchange accounts. ID is opaque: no name or e-mail.
type Customer struct {
	ID       string    `json:"id"`
	Accounts []Account `json:"accounts"`
}

// Account is one exchange account. KeyID names its key in the key store
// (keystore account id) — a reference, never the key.
type Account struct {
	ID          string `json:"id"`
	Exchange    string `json:"exchange"`    // an internal/exchange name (exchange.Deribit)
	Environment string `json:"environment"` // testnet | live
	Label       string `json:"label"`
	KeyID       string `json:"key_id"`
	Bots        []Bot  `json:"bots"`
}

// Bot is one bot process on an account: one per currency (the isolation unit
// "account × currency", docs/account-isolation.md §2 A).
type Bot struct {
	Currency string `json:"currency"` // BTC, ETH, …
	Config   string `json:"config"`   // the config file name, e.g. config_btc.yaml
}

// String names a bot in a violation: "BTC (config_btc.yaml)".
func (b Bot) String() string { return fmt.Sprintf("%s (%s)", b.Currency, b.Config) }

// AccountStatus is one account as the registry reports it: no key, client id
// or scope string, only the key's scope status.
type AccountStatus struct {
	CustomerID  string `json:"customer_id"`
	AccountID   string `json:"account_id"`
	Exchange    string `json:"exchange"`
	Environment string `json:"environment"`
	Label       string `json:"label"`
	Bots        []Bot  `json:"bots"`
	KeyScope    string `json:"key_scope"` // gateway.KeyScope* | no_key | missing
	Status      string `json:"status"`    // ok | blocked
	Reason      string `json:"reason,omitempty"`
}

// Validate checks the registry's rules and returns every break at once
// (wrapping ErrInvalid), or nil:
//   - ids are opaque and unique (customers; accounts across customers);
//   - an account names a known exchange and environment;
//   - one bot per (exchange account, currency): two bots trading the same
//     currency on one account would cancel and adopt each other's orders;
//   - a key belongs to at most one account (one key = one exchange account);
//   - a bot's config is a file name, not a path.
//
// Pure.
func (f File) Validate() error {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	if f.Version != fileVersion {
		add("version %d, want %d", f.Version, fileVersion)
	}
	customers := map[string]bool{}
	accountOwner := map[string]string{} // account id → customer id
	keyAccount := map[string]string{}   // key id → account id
	for _, c := range f.Customers {
		if !idPattern.MatchString(c.ID) {
			add("customer id %q is not an opaque id (a-z, 0-9, '-', '_'; no name or e-mail)", c.ID)
		}
		if customers[c.ID] {
			add("customer %q appears twice", c.ID)
		}
		customers[c.ID] = true
		for _, a := range c.Accounts {
			where := fmt.Sprintf("account %q", a.ID)
			if !idPattern.MatchString(a.ID) {
				add("%s: id is not an opaque id (a-z, 0-9, '-', '_')", where)
			}
			if owner, dup := accountOwner[a.ID]; dup {
				add("%s appears twice (customers %q and %q)", where, owner, c.ID)
			}
			accountOwner[a.ID] = c.ID
			if !slices.Contains(exchange.Names, a.Exchange) {
				add("%s: exchange %q is not one of %s", where, a.Exchange, strings.Join(exchange.Names, ", "))
			}
			if !slices.Contains(Environments, a.Environment) {
				add("%s: environment %q is not one of %s", where, a.Environment, strings.Join(Environments, ", "))
			}
			if strings.TrimSpace(a.Label) == "" {
				add("%s: empty label", where)
			}
			if a.KeyID != "" {
				if other, dup := keyAccount[a.KeyID]; dup {
					add("key %q is used by accounts %q and %q: a key belongs to one account", a.KeyID, other, a.ID)
				} else {
					keyAccount[a.KeyID] = a.ID
				}
			}
			byCurrency := map[string]Bot{}
			for _, b := range a.Bots {
				if !currencyPattern.MatchString(b.Currency) {
					add("%s: bot currency %q is not an upper-case code (BTC, ETH)", where, b.Currency)
				}
				if b.Config == "" || filepath.Base(b.Config) != b.Config {
					add("%s: bot %s: config must be a file name, not a path", where, b)
				}
				if first, dup := byCurrency[b.Currency]; dup {
					add("%s on %s: two bots trade %s on one account: %s and %s", where, a.Exchange, b.Currency, first, b)
					continue
				}
				byCurrency[b.Currency] = b
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n  - %s", ErrInvalid, strings.Join(errs, "\n  - "))
}

// KeyStatus says whether an account's key may be used, from the key store's
// entry for it (nil when the store has none). Pure.
func KeyStatus(keyID string, entry *keystore.Entry) (scope, status, reason string) {
	switch {
	case keyID == "":
		return ScopeNoKey, StatusBlocked, "the account names no key"
	case entry == nil:
		return ScopeMissing, StatusBlocked, fmt.Sprintf("key %q is not in the key store", keyID)
	case entry.Status == keystore.StatusRevoked:
		return entry.KeyScope, StatusBlocked, fmt.Sprintf("key %q was revoked", keyID)
	case entry.KeyScope == gateway.KeyScopeCanMoveFunds:
		return entry.KeyScope, StatusBlocked, "the key can move funds: withdrawal rights are refused by policy; use a trade-only key"
	case entry.KeyScope != gateway.KeyScopeTradeOnly:
		return entry.KeyScope, StatusBlocked, "the key's scope is unknown: withdrawal rights cannot be ruled out"
	}
	return entry.KeyScope, StatusOK, ""
}

// Registry is a loaded, valid registry joined to the key store's statuses.
type Registry struct {
	accounts []AccountStatus // sorted by customer, then account id
}

// New validates f and joins each account to its key in keys (keystore.List
// or keystore.ListFile). Pure.
func New(f File, keys []keystore.Entry) (*Registry, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	byID := make(map[string]keystore.Entry, len(keys))
	for _, k := range keys {
		byID[k.AccountID] = k
	}
	r := &Registry{}
	for _, c := range f.Customers {
		for _, a := range c.Accounts {
			var entry *keystore.Entry
			if k, ok := byID[a.KeyID]; ok {
				entry = &k
			}
			scope, status, reason := KeyStatus(a.KeyID, entry)
			r.accounts = append(r.accounts, AccountStatus{
				CustomerID: c.ID, AccountID: a.ID, Exchange: a.Exchange, Environment: a.Environment,
				Label: a.Label, Bots: slices.Clone(a.Bots), KeyScope: scope, Status: status, Reason: reason,
			})
		}
	}
	sort.Slice(r.accounts, func(i, j int) bool {
		if r.accounts[i].CustomerID != r.accounts[j].CustomerID {
			return r.accounts[i].CustomerID < r.accounts[j].CustomerID
		}
		return r.accounts[i].AccountID < r.accounts[j].AccountID
	})
	return r, nil
}

// List is every account, sorted by customer then account id.
func (r *Registry) List() []AccountStatus { return slices.Clone(r.accounts) }

// Get is one account by id.
func (r *Registry) Get(accountID string) (AccountStatus, bool) {
	for _, a := range r.accounts {
		if a.AccountID == accountID {
			return a, true
		}
	}
	return AccountStatus{}, false
}

// Blocked counts the blocked accounts.
func (r *Registry) Blocked() int {
	n := 0
	for _, a := range r.accounts {
		if a.Status == StatusBlocked {
			n++
		}
	}
	return n
}

// Load reads the registry at path (owner-only, unknown fields refused so no
// stray key or personal field slips in) and joins it to keys. Each blocked
// account is logged at WARN with its reason — never a key.
func Load(path string, keys []keystore.Entry) (*Registry, error) {
	f, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := New(f, keys)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, a := range r.accounts {
		if a.Status == StatusBlocked {
			slog.Warn("accounts: account blocked", "customer_id", a.CustomerID, "account_id", a.AccountID,
				"label", a.Label, "key_scope", a.KeyScope, "reason", a.Reason)
		}
	}
	return r, nil
}

// ReadFile parses the registry at path without validating it.
func ReadFile(path string) (File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return File{}, fmt.Errorf("accounts: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return File{}, fmt.Errorf("%w (%s is %04o)", ErrPermissions, path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("accounts: read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("accounts: parse %s: %w", path, err)
	}
	return f, nil
}

// WriteFile validates f and replaces path atomically (temp file in the same
// folder, then rename), owner-only. An invalid registry is not written.
func WriteFile(path string, f File) error {
	if err := f.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("accounts: encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("accounts: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".accounts-*.tmp")
	if err != nil {
		return fmt.Errorf("accounts: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // a no-op once renamed
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("accounts: chmod: %w", err)
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("accounts: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("accounts: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("accounts: replace %s: %w", path, err)
	}
	return nil
}
