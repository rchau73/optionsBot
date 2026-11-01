package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/keystore"
)

// Built by concatenation so this file does not itself name the variables
// TestKeystore_MasterKeyReadOnlyByPackage looks for.
var (
	ksMasterEnv     = "KEYSTORE_" + "MASTER_KEY"
	ksMasterFileEnv = ksMasterEnv + "_FILE"
)

const (
	ksClientID  = "client-id-PLAINTEXT-7f3a"
	ksSecret    = "client-secret-PLAINTEXT-9b21"
	ksTradeOnly = "account:read trade:read_write wallet:none"
)

func ksKey(b byte) []byte { return bytes.Repeat([]byte{b}, keystore.MinMasterKeyBytes) }

func ksOpen(t *testing.T, path string, key []byte) *keystore.Store {
	t.Helper()
	s, err := keystore.Open(path, key)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func ksCreds() keystore.Credentials {
	return keystore.NewCredentials("Account one", ksClientID, ksSecret, ksTradeOnly)
}

// Scenario: a key is sealed on disk.
func TestKeystore_KeyIsSealedOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "keystore.json")
	s := ksOpen(t, path, ksKey(1))
	if err := s.Put("acct-1", ksCreds()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{ksClientID, ksSecret, "PLAINTEXT"} {
		if bytes.Contains(raw, []byte(plain)) {
			t.Errorf("file holds %q in plain text", plain)
		}
	}
	// Same store and a fresh open with the same key both return the credentials.
	for name, st := range map[string]*keystore.Store{"same": s, "reopened": ksOpen(t, path, ksKey(1))} {
		got, err := st.Get("acct-1")
		if err != nil {
			t.Fatalf("%s Get: %v", name, err)
		}
		if got.ClientID() != ksClientID || got.Secret() != ksSecret || got.Label() != "Account one" || got.Scope() != ksTradeOnly {
			t.Errorf("%s Get returned other credentials", name)
		}
	}
	if _, err := s.Get("acct-2"); !errors.Is(err, keystore.ErrNotFound) {
		t.Errorf("Get unknown = %v, want ErrNotFound", err)
	}
}

// Scenario: a wrong or missing master key.
func TestKeystore_WrongOrMissingMasterKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if err := ksOpen(t, path, ksKey(0xA)).Put("acct-1", ksCreds()); err != nil {
		t.Fatal(err)
	}
	got, err := ksOpen(t, path, ksKey(0xB)).Get("acct-1")
	if !errors.Is(err, keystore.ErrDecrypt) {
		t.Errorf("Get with key B = %v, want ErrDecrypt", err)
	}
	if got != (keystore.Credentials{}) {
		t.Error("Get with key B returned partial data")
	}

	for _, tc := range []struct {
		name string
		key  []byte
		want error
	}{
		{"nil", nil, keystore.ErrNoMasterKey},
		{"empty", []byte{}, keystore.ErrNoMasterKey},
		{"31 bytes", bytes.Repeat([]byte{1}, keystore.MinMasterKeyBytes-1), keystore.ErrShortMasterKey},
	} {
		if _, err := keystore.Open(path, tc.key); !errors.Is(err, tc.want) {
			t.Errorf("Open %s = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := keystore.Open(path, ksKey(1)); err != nil {
		t.Errorf("Open with exactly 32 bytes: %v", err)
	}
}

func TestKeystore_LoadMasterKey(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(ksKey(7))
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "master.key")
	if err := os.WriteFile(keyFile, []byte(good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openFile := filepath.Join(dir, "open.key")
	if err := os.WriteFile(openFile, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(openFile, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, env, file string
		wantErr         error // nil = any error when wantOK is false
		wantOK          bool
	}{
		{name: "env", env: good, wantOK: true},
		{name: "file", file: keyFile, wantOK: true},
		{name: "none", wantErr: keystore.ErrNoMasterKey},
		{name: "short", env: base64.StdEncoding.EncodeToString(make([]byte, 16)), wantErr: keystore.ErrShortMasterKey},
		{name: "not base64", env: "not base64 !!"},
		{name: "both", env: good, file: keyFile},
		{name: "file readable by others", file: openFile, wantErr: keystore.ErrPermissions},
		{name: "file missing", file: filepath.Join(dir, "nope"), wantErr: fs.ErrNotExist},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ksMasterEnv, tc.env)
			t.Setenv(ksMasterFileEnv, tc.file)
			key, err := keystore.LoadMasterKey()
			if tc.wantOK {
				if err != nil || !bytes.Equal(key, ksKey(7)) {
					t.Fatalf("LoadMasterKey = %v", err)
				}
				return
			}
			if err == nil || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
				t.Fatalf("LoadMasterKey err = %v, want %v", err, tc.wantErr)
			}
			if key != nil {
				t.Error("key returned with an error")
			}
		})
	}
}

// Scenario: secrets never print.
func TestKeystore_SecretsNeverPrint(t *testing.T) {
	c := ksCreds()
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("creds", "creds", c, "ptr", &c)
	jsonOut, err := json.Marshal(map[string]any{"c": c, "p": &c})
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"%v": fmt.Sprintf("%v", c), "%+v": fmt.Sprintf("%+v", c), "%#v": fmt.Sprintf("%#v", c),
		"%s": fmt.Sprintf("%s", c), "%v ptr": fmt.Sprintf("%v", &c), "%+v slice": fmt.Sprintf("%+v", []keystore.Credentials{c}),
		"JSON": string(jsonOut), "slog": logged.String(),
	}
	for name, out := range outputs {
		if !strings.Contains(out, keystore.Redacted) {
			t.Errorf("%s output %q lacks %s", name, out, keystore.Redacted)
		}
		if strings.Contains(out, ksClientID) || strings.Contains(out, ksSecret) {
			t.Errorf("%s output leaks the key: %q", name, out)
		}
	}
}

// Scenario: a withdrawal-enabled key is refused.
func TestKeystore_WithdrawalEnabledKeyRefused(t *testing.T) {
	for _, tc := range []struct {
		name, scope string
		want        error
	}{
		{"wallet read-write", "account:read trade:read_write wallet:read_write", keystore.ErrWithdrawalRights},
		{"account read-write", "account:read_write trade:read_write wallet:none", keystore.ErrWithdrawalRights},
		{"unknown scope", "connection", keystore.ErrScopeUnknown},
		{"empty scope", "", keystore.ErrScopeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keystore.json")
			s := ksOpen(t, path, ksKey(1))
			err := s.Put("acct-1", keystore.NewCredentials("A", ksClientID, ksSecret, tc.scope))
			if !errors.Is(err, tc.want) {
				t.Fatalf("Put = %v, want %v", err, tc.want)
			}
			if tc.want == keystore.ErrWithdrawalRights && !strings.Contains(err.Error(), "withdrawal rights") {
				t.Errorf("reason %q does not name withdrawal rights", err)
			}
			if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("a file was written: %v", statErr)
			}
			if len(s.List()) != 0 {
				t.Error("refused key is listed")
			}
		})
	}
	// Missing key material is refused too.
	s := ksOpen(t, filepath.Join(t.TempDir(), "k.json"), ksKey(1))
	if err := s.Put("acct-1", keystore.NewCredentials("A", "", ksSecret, ksTradeOnly)); err == nil {
		t.Error("Put without a client id accepted")
	}
	if err := s.Put(" ", ksCreds()); err == nil {
		t.Error("Put with a blank account id accepted")
	}
}

// Scenario: revoke and rotate.
func TestKeystore_RevokeAndRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	s := ksOpen(t, path, ksKey(1))
	if err := s.Put("acct-1", ksCreds()); err != nil {
		t.Fatal(err)
	}
	oldCipher := ksField(t, path, "acct-1", "ciphertext")

	if err := s.Revoke("acct-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := s.Get("acct-1"); !errors.Is(err, keystore.ErrRevoked) {
		t.Errorf("Get after revoke = %v, want ErrRevoked", err)
	}
	if err := s.Revoke("acct-1"); err != nil {
		t.Errorf("second Revoke: %v", err)
	}
	if err := s.Revoke("nobody"); !errors.Is(err, keystore.ErrNotFound) {
		t.Errorf("Revoke unknown = %v, want ErrNotFound", err)
	}
	list := s.List()
	if len(list) != 1 || list[0].Status != keystore.StatusRevoked || list[0].Label != "Account one" || list[0].RevokedAt == nil {
		t.Fatalf("List after revoke = %+v", list)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte(oldCipher)) || ksField(t, path, "acct-1", "wrapped_key") != "" {
		t.Error("revoked record still holds key material")
	}
	if _, err := ksOpen(t, path, ksKey(1)).Get("acct-1"); !errors.Is(err, keystore.ErrRevoked) {
		t.Errorf("tombstone not kept on disk: %v", err)
	}

	newCreds := keystore.NewCredentials("Account one", "new-id", "new-secret", ksTradeOnly)
	if err := s.Put("acct-1", newCreds); err != nil {
		t.Fatalf("Put after revoke: %v", err)
	}
	got, err := s.Get("acct-1")
	if err != nil || got.ClientID() != "new-id" || got.Secret() != "new-secret" {
		t.Fatalf("Get after rotate = %v", err)
	}
	if s.List()[0].Status != keystore.StatusActive {
		t.Error("rotated account not active")
	}
	// Rotation over an active key replaces its ciphertext too.
	active := ksField(t, path, "acct-1", "ciphertext")
	if err := s.Put("acct-1", ksCreds()); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if bytes.Contains(raw, []byte(active)) {
		t.Error("old ciphertext kept after rotation")
	}
	if list := s.List(); len(list) != 1 || list[0].KeyScope != "trade_only" {
		t.Errorf("List = %+v", list)
	}
}

// Scenario: tampering is detected.
func TestKeystore_TamperingDetected(t *testing.T) {
	for _, field := range []string{"ciphertext", "wrapped_key"} {
		t.Run(field, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keystore.json")
			if err := ksOpen(t, path, ksKey(1)).Put("acct-1", ksCreds()); err != nil {
				t.Fatal(err)
			}
			var fd map[string]any
			raw, _ := os.ReadFile(path)
			if err := json.Unmarshal(raw, &fd); err != nil {
				t.Fatal(err)
			}
			rec := fd["accounts"].(map[string]any)["acct-1"].(map[string]any)
			b, _ := base64.StdEncoding.DecodeString(rec[field].(string))
			b[len(b)-1] ^= 0x01
			rec[field] = base64.StdEncoding.EncodeToString(b)
			out, _ := json.Marshal(fd)
			if err := os.WriteFile(path, out, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ksOpen(t, path, ksKey(1)).Get("acct-1")
			if !errors.Is(err, keystore.ErrDecrypt) || got != (keystore.Credentials{}) {
				t.Errorf("Get on altered %s = %v", field, err)
			}
		})
	}
	// A sealed record copied to another account id fails (bound to its id).
	path := filepath.Join(t.TempDir(), "keystore.json")
	if err := ksOpen(t, path, ksKey(1)).Put("acct-1", ksCreds()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var fd map[string]map[string]any
	_ = json.Unmarshal(raw, &fd)
	fd["accounts"]["acct-2"] = fd["accounts"]["acct-1"]
	out, _ := json.Marshal(map[string]any{"version": 1, "accounts": fd["accounts"]})
	_ = os.WriteFile(path, out, 0o600)
	if _, err := ksOpen(t, path, ksKey(1)).Get("acct-2"); !errors.Is(err, keystore.ErrDecrypt) {
		t.Errorf("record moved to another id = %v, want ErrDecrypt", err)
	}
}

// Scenario: file permissions.
func TestKeystore_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	if err := ksOpen(t, path, ksKey(1)).Put("acct-1", ksCreds()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("store file mode = %04o, want 0600", info.Mode().Perm())
	}
	for _, mode := range []os.FileMode{0o640, 0o604, 0o660} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := keystore.Open(path, ksKey(1)); !errors.Is(err, keystore.ErrPermissions) {
			t.Errorf("Open with mode %04o = %v, want ErrPermissions", mode, err)
		}
	}
	// A corrupt or future-version file is refused, not silently emptied.
	for name, body := range map[string]string{"corrupt": "{", "version": `{"version":2,"accounts":{}}`} {
		p := filepath.Join(t.TempDir(), name+".json")
		_ = os.WriteFile(p, []byte(body), 0o600)
		if _, err := keystore.Open(p, ksKey(1)); err == nil {
			t.Errorf("Open %s file accepted", name)
		}
	}
}

// The master key reaches no other code: only internal/keystore names its variables.
func TestKeystore_MasterKeyReadOnlyByPackage(t *testing.T) {
	var offenders []string
	for _, root := range []string{"../cmd", "../internal", "../tests"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && filepath.ToSlash(p) == "../internal/keystore" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if bytes.Contains(b, []byte(ksMasterEnv)) {
				offenders = append(offenders, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("%s is named outside internal/keystore (use keystore.LoadMasterKey): %v", ksMasterEnv, offenders)
	}
}

// ksField reads one record field (base64 string) from the store file.
func ksField(t *testing.T, path, id, field string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fd struct {
		Accounts map[string]map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(raw, &fd); err != nil {
		t.Fatal(err)
	}
	s, _ := fd.Accounts[id][field].(string)
	return s
}
