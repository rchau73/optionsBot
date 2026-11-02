// Package keystore keeps exchange API keys sealed at rest (#572,
// docs/account-isolation.md §4 item 4): one owner-only file per environment,
// each secret encrypted with its own data key (AES-256-GCM), the data key
// wrapped by a key derived from the master key (LoadMasterKey). Keys whose
// scope can move funds are refused. Nothing in cmd/bot uses it yet.
package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"optionsbot/internal/gateway"
)

// DefaultPath is the store's file in an environment's clone (data/ is gitignored).
const DefaultPath = "data/keystore.json"

// Account status words (Entry.Status).
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

const (
	fileVersion  = 1
	dataKeyBytes = 32
	fileMode     = 0o600
	dirMode      = 0o700
	kekInfo      = "optionsbot keystore v1 key-encryption key"
)

var (
	// ErrNotFound: no record for the account id.
	ErrNotFound = errors.New("keystore: account not found")
	// ErrRevoked: the account's key was revoked; only its tombstone is left.
	ErrRevoked = errors.New("keystore: account key revoked")
	// ErrDecrypt: authentication failed — a wrong master key or an altered file.
	ErrDecrypt = errors.New("keystore: decryption failed: authentication error (wrong master key or altered file)")
	// ErrWithdrawalRights: the key's scope can move funds out of the account.
	ErrWithdrawalRights = errors.New("keystore: key refused: it has withdrawal rights")
	// ErrScopeUnknown: the key's scope could not be read, so withdrawal rights cannot be ruled out.
	ErrScopeUnknown = errors.New("keystore: key refused: its scope is unknown, so withdrawal rights cannot be ruled out")
	// ErrPermissions: the store file is readable by the group or others.
	ErrPermissions = errors.New("keystore: file is readable by group or others: chmod 600 it")
)

// Entry is what List shows of an account: never a secret, client id or scope string.
type Entry struct {
	AccountID string     `json:"account_id"`
	Label     string     `json:"label"`
	Status    string     `json:"status"`    // active | revoked
	KeyScope  string     `json:"key_scope"` // gateway.KeyScope* status of the stored key
	UpdatedAt time.Time  `json:"updated_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// record is one account on disk. A revoked record is a tombstone: label,
// times, status, no key material.
type record struct {
	Label      string     `json:"label"`
	Status     string     `json:"status"`
	KeyScope   string     `json:"key_scope"`
	UpdatedAt  time.Time  `json:"updated_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	WrappedKey []byte     `json:"wrapped_key,omitempty"` // nonce ‖ data key sealed by the master-derived key
	Ciphertext []byte     `json:"ciphertext,omitempty"`  // nonce ‖ credentials sealed by the data key
}

type fileData struct {
	Version  int                `json:"version"`
	Accounts map[string]*record `json:"accounts"`
}

// Store is an open key store. Safe for concurrent use.
type Store struct {
	path string
	kek  cipher.AEAD
	now  func() time.Time

	mu       sync.Mutex
	accounts map[string]*record
}

// Open opens the store at path with masterKey (≥ MinMasterKeyBytes bytes).
// A missing file is an empty store, created owner-only on the first Put; an
// existing file readable by group or others is refused. A wrong master key is
// not detected here but by Get (ErrDecrypt).
func Open(path string, masterKey []byte) (*Store, error) {
	if err := checkMasterKey(masterKey); err != nil {
		return nil, err
	}
	kekBytes, err := hkdf.Key(sha256.New, masterKey, nil, kekInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("keystore: derive key: %w", err)
	}
	kek, err := newAEAD(kekBytes)
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, kek: kek, now: time.Now, accounts: map[string]*record{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	err := checkOwnerOnly(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("keystore: read %s: %w", s.path, err)
	}
	var fd fileData
	if err := json.Unmarshal(raw, &fd); err != nil {
		return fmt.Errorf("keystore: parse %s: %w", s.path, err)
	}
	if fd.Version != fileVersion {
		return fmt.Errorf("keystore: %s has version %d, want %d", s.path, fd.Version, fileVersion)
	}
	for id, r := range fd.Accounts {
		if r != nil {
			s.accounts[id] = r
		}
	}
	return nil
}

// checkOwnerOnly refuses a file that the group or others may read or write.
func checkOwnerOnly(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w (%s is %04o)", ErrPermissions, path, info.Mode().Perm())
	}
	return nil
}

// Put seals creds for accountID, replacing any earlier key (rotation: the old
// ciphertext is not kept) or tombstone. A key whose scope can move funds — or
// whose scope is unknown — is refused and nothing is written.
func (s *Store) Put(accountID string, creds Credentials) error {
	if strings.TrimSpace(accountID) == "" {
		return errors.New("keystore: empty account id")
	}
	if creds.clientID == "" || creds.secret == "" {
		return errors.New("keystore: credentials need a client id and a secret")
	}
	scope := gateway.ParseKeyScope(creds.scope)
	switch {
	case scope.CanMoveFunds == nil:
		return ErrScopeUnknown
	case *scope.CanMoveFunds:
		return fmt.Errorf("%w (%s); create a trade-only key (account:read trade:read_write) with no withdrawal or transfer rights",
			ErrWithdrawalRights, strings.Join(scope.FundScopes, ", "))
	}

	plain, err := json.Marshal(sealed{Label: creds.label, ClientID: creds.clientID, Secret: creds.secret, Scope: creds.scope})
	if err != nil {
		return fmt.Errorf("keystore: encode credentials: %w", err)
	}
	defer clear(plain)
	dataKey := make([]byte, dataKeyBytes)
	defer clear(dataKey)
	if _, err := rand.Read(dataKey); err != nil {
		return fmt.Errorf("keystore: data key: %w", err)
	}
	dek, err := newAEAD(dataKey)
	if err != nil {
		return err
	}
	ciphertext, err := seal(dek, plain, aad("credentials", accountID))
	if err != nil {
		return err
	}
	wrapped, err := seal(s.kek, dataKey, aad("data-key", accountID))
	if err != nil {
		return err
	}
	rec := &record{
		Label: creds.label, Status: StatusActive, KeyScope: scope.Status, UpdatedAt: s.now().UTC(),
		WrappedKey: wrapped, Ciphertext: ciphertext,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, rotated := s.accounts[accountID]
	if err := s.writeWith(accountID, rec); err != nil {
		return err
	}
	slog.Info("keystore: key stored", "account_id", accountID, "label", creds.label, "rotated", rotated, "key_scope", scope.Status)
	return nil
}

// Get unseals accountID's credentials. ErrNotFound, ErrRevoked, or ErrDecrypt
// (wrong master key, altered file) return empty credentials, never part of them.
func (s *Store) Get(accountID string) (Credentials, error) {
	s.mu.Lock()
	rec, ok := s.accounts[accountID]
	s.mu.Unlock()
	if !ok {
		return Credentials{}, ErrNotFound
	}
	if rec.Status == StatusRevoked {
		return Credentials{}, ErrRevoked
	}
	dataKey, err := open(s.kek, rec.WrappedKey, aad("data-key", accountID))
	if err != nil {
		return Credentials{}, err
	}
	defer clear(dataKey)
	dek, err := newAEAD(dataKey)
	if err != nil {
		return Credentials{}, ErrDecrypt
	}
	plain, err := open(dek, rec.Ciphertext, aad("credentials", accountID))
	if err != nil {
		return Credentials{}, err
	}
	defer clear(plain)
	var sc sealed
	if err := json.Unmarshal(plain, &sc); err != nil {
		return Credentials{}, ErrDecrypt
	}
	return NewCredentials(sc.Label, sc.ClientID, sc.Secret, sc.Scope), nil
}

// Revoke replaces accountID's sealed key with a tombstone (label, time), so a
// later Get returns ErrRevoked. Revoking a revoked account changes nothing.
func (s *Store) Revoke(accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.accounts[accountID]
	if !ok {
		return ErrNotFound
	}
	if old.Status == StatusRevoked {
		return nil
	}
	now := s.now().UTC()
	tomb := &record{Label: old.Label, Status: StatusRevoked, KeyScope: old.KeyScope, UpdatedAt: now, RevokedAt: &now}
	if err := s.writeWith(accountID, tomb); err != nil {
		return err
	}
	slog.Info("keystore: key revoked", "account_id", accountID, "label", old.Label)
	return nil
}

// List is every account's label and status, sorted by account id — never a secret.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.accounts))
	for id, r := range s.accounts {
		out = append(out, Entry{AccountID: id, Label: r.Label, Status: r.Status, KeyScope: r.KeyScope, UpdatedAt: r.UpdatedAt, RevokedAt: r.RevokedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AccountID < out[j].AccountID })
	return out
}

// writeWith writes the store with accountID set to rec, and keeps the change
// in memory only once the file is written. Callers hold s.mu.
func (s *Store) writeWith(accountID string, rec *record) error {
	next := make(map[string]*record, len(s.accounts)+1)
	for id, r := range s.accounts {
		next[id] = r
	}
	next[accountID] = rec
	if err := writeFile(s.path, fileData{Version: fileVersion, Accounts: next}); err != nil {
		slog.Error("keystore: write failed", "account_id", accountID, "err", err)
		return err
	}
	s.accounts = next
	return nil
}

// writeFile replaces path atomically (temp file in the same folder, then
// rename), owner-only, so a crash never leaves a half-written store and the
// replaced ciphertext is gone from the file.
func writeFile(path string, fd fileData) error {
	raw, err := json.MarshalIndent(fd, "", "  ")
	if err != nil {
		return fmt.Errorf("keystore: encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("keystore: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".keystore-*.tmp")
	if err != nil {
		return fmt.Errorf("keystore: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // a no-op once renamed
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("keystore: chmod: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("keystore: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("keystore: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("keystore: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("keystore: replace %s: %w", path, err)
	}
	return nil
}

// aad binds a ciphertext to its account and purpose, so a sealed record moved
// to another account id (or a wrapped key swapped for a ciphertext) fails.
func aad(purpose, accountID string) []byte {
	return []byte("optionsbot keystore v1 " + purpose + " " + accountID)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("keystore: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keystore: cipher: %w", err)
	}
	return gcm, nil
}

// seal returns nonce ‖ ciphertext.
func seal(a cipher.AEAD, plain, additional []byte) ([]byte, error) {
	nonce := make([]byte, a.NonceSize(), a.NonceSize()+len(plain)+a.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("keystore: nonce: %w", err)
	}
	return a.Seal(nonce, nonce, plain, additional), nil
}

// open reverses seal; any failure is ErrDecrypt.
func open(a cipher.AEAD, sealedBytes, additional []byte) ([]byte, error) {
	if len(sealedBytes) < a.NonceSize()+a.Overhead() {
		return nil, ErrDecrypt
	}
	n := a.NonceSize()
	plain, err := a.Open(nil, sealedBytes[:n], sealedBytes[n:], additional)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
