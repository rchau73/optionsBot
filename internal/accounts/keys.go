package accounts

import (
	"errors"
	"fmt"
	"log/slog"

	"optionsbot/internal/keystore"
)

// The key lifecycle (#625, docs/account-isolation.md §4 items 4–5): add,
// rotate and revoke an account's key in the key store, with the registry's
// key_id following. A failure at any step leaves the account on the key it
// had before.

// KeyStore is what the key lifecycle needs of the key store
// (*keystore.Store); tests stand in a store that fails a step.
type KeyStore interface {
	Put(keyID string, creds keystore.Credentials) error
	Revoke(keyID string) error
	List() []keystore.Entry
}

var (
	// ErrUnknownAccount: the registry has no account with that id.
	ErrUnknownAccount = errors.New("accounts: no such account in the registry")
	// ErrHasActiveKey: key add on an account whose key is active (use key rotate).
	ErrHasActiveKey = errors.New("accounts: the account already has an active key: use key rotate")
	// ErrNoActiveKey: key rotate or revoke on an account with no active key (use key add).
	ErrNoActiveKey = errors.New("accounts: the account has no active key: use key add")
)

// AddKey seals creds in store under a new key id and points the account's
// key_id at it. The account must have no active key (a missing or revoked one
// is replaced). The store refuses a key that can move funds or whose scope is
// unknown; then nothing is written. Returns the new key id.
func AddKey(registryPath string, store KeyStore, accountID string, creds keystore.Credentials) (string, error) {
	f, acct, err := loadAccount(registryPath, accountID)
	if err != nil {
		return "", err
	}
	if entry := findEntry(store.List(), acct.KeyID); entry != nil && entry.Status == keystore.StatusActive {
		return "", fmt.Errorf("%w (account %q)", ErrHasActiveKey, accountID)
	}
	newID := nextKeyID(f, store.List(), accountID)
	if err := store.Put(newID, creds); err != nil {
		return "", storeError(err)
	}
	acct.KeyID = newID
	if err := WriteFile(registryPath, f); err != nil {
		undoPut(store, accountID, newID)
		return "", err
	}
	return newID, nil
}

// RotateKey replaces the account's active key: it seals creds under a new key
// id, then points key_id at it, then revokes the old key. A failure at any
// step leaves key_id on the old key and the old key active (a new key already
// sealed is revoked again). Returns the new and the old key id.
func RotateKey(registryPath string, store KeyStore, accountID string, creds keystore.Credentials) (newID, oldID string, err error) {
	f, acct, err := loadAccount(registryPath, accountID)
	if err != nil {
		return "", "", err
	}
	oldID = acct.KeyID
	if entry := findEntry(store.List(), oldID); entry == nil || entry.Status != keystore.StatusActive {
		return "", "", fmt.Errorf("%w (account %q)", ErrNoActiveKey, accountID)
	}
	newID = nextKeyID(f, store.List(), accountID)
	if err := store.Put(newID, creds); err != nil {
		return "", "", storeError(err)
	}
	acct.KeyID = newID
	if err := WriteFile(registryPath, f); err != nil {
		undoPut(store, accountID, newID)
		return "", "", err
	}
	if err := store.Revoke(oldID); err != nil {
		acct.KeyID = oldID
		if werr := WriteFile(registryPath, f); werr != nil {
			slog.Error("accounts: rotate: could not point the account back at its old key; fix key_id by hand",
				"account_id", accountID, "old_key_id", oldID, "new_key_id", newID, "err", werr)
			return "", "", fmt.Errorf("accounts: revoke old key: %w; restoring the registry also failed: %v", err, werr)
		}
		undoPut(store, accountID, newID)
		return "", "", fmt.Errorf("accounts: revoke old key (the account stays on it): %w", err)
	}
	return newID, oldID, nil
}

// RevokeKey revokes the account's key in store; the registry keeps key_id, so
// the account shows blocked ("revoked") from then on. alreadyRevoked reports a
// key that was revoked before (nothing changed).
func RevokeKey(registryPath string, store KeyStore, accountID string) (keyID string, alreadyRevoked bool, err error) {
	_, acct, err := loadAccount(registryPath, accountID)
	if err != nil {
		return "", false, err
	}
	entry := findEntry(store.List(), acct.KeyID)
	if entry == nil {
		return "", false, fmt.Errorf("%w (account %q)", ErrNoActiveKey, accountID)
	}
	if entry.Status == keystore.StatusRevoked {
		return acct.KeyID, true, nil
	}
	if err := store.Revoke(acct.KeyID); err != nil {
		return "", false, err
	}
	return acct.KeyID, false, nil
}

// loadAccount reads and validates the registry and returns the file and a
// pointer to the account inside it, so a change to it is written by WriteFile.
func loadAccount(registryPath, accountID string) (File, *Account, error) {
	f, err := ReadFile(registryPath)
	if err != nil {
		return File{}, nil, err
	}
	if err := f.Validate(); err != nil {
		return File{}, nil, fmt.Errorf("%s: %w", registryPath, err)
	}
	for ci := range f.Customers {
		for ai := range f.Customers[ci].Accounts {
			if f.Customers[ci].Accounts[ai].ID == accountID {
				return f, &f.Customers[ci].Accounts[ai], nil
			}
		}
	}
	return File{}, nil, fmt.Errorf("%w: %q", ErrUnknownAccount, accountID)
}

// findEntry is the store's entry for keyID, or nil.
func findEntry(entries []keystore.Entry, keyID string) *keystore.Entry {
	if keyID == "" {
		return nil
	}
	for i := range entries {
		if entries[i].AccountID == keyID {
			return &entries[i]
		}
	}
	return nil
}

// nextKeyID is "<account>-key-<n>" for the smallest n used neither in the
// store (revoked tombstones included) nor as a key_id in the registry. Pure.
func nextKeyID(f File, entries []keystore.Entry, accountID string) string {
	used := map[string]bool{}
	for _, e := range entries {
		used[e.AccountID] = true
	}
	for _, c := range f.Customers {
		for _, a := range c.Accounts {
			used[a.KeyID] = true
		}
	}
	for n := 1; ; n++ {
		id := fmt.Sprintf("%s-key-%d", accountID, n)
		if !used[id] {
			return id
		}
	}
}

// storeError keeps a refusal's message to the store's sentinel, so no scope
// token of the refused key reaches the output.
func storeError(err error) error {
	for _, sentinel := range []error{keystore.ErrWithdrawalRights, keystore.ErrScopeUnknown} {
		if errors.Is(err, sentinel) {
			return fmt.Errorf("%w; create a trade-only key (account:read trade:read_write) with no withdrawal or transfer rights", sentinel)
		}
	}
	return err
}

// undoPut revokes a key sealed by a step that could not finish, so no unused
// working key is left in the store.
func undoPut(store KeyStore, accountID, keyID string) {
	if err := store.Revoke(keyID); err != nil {
		slog.Error("accounts: could not revoke the unused new key; revoke it by hand",
			"account_id", accountID, "key_id", keyID, "err", err)
	}
}
