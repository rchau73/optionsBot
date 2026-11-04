package tests

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/accounts"
	"optionsbot/internal/keystore"
)

// Key lifecycle CLI (#625): add, rotate and revoke an account's key through
// the key store, the registry's key_id following.

const (
	akSecret2      = "client-secret-ROTATED-4c88"
	akCanMoveFunds = "account:read trade:read_write wallet:read_write"
)

// akEnv is one environment's files in a temporary folder, with a fake master key.
type akEnv struct {
	dir, registry, keystore, audit string
	logs                           *bytes.Buffer
}

func akSetup(t *testing.T) *akEnv {
	t.Helper()
	t.Setenv(ksMasterFileEnv, "")
	t.Setenv(ksMasterEnv, base64.StdEncoding.EncodeToString(ksKey(7)))
	dir := t.TempDir()
	e := &akEnv{
		dir:      dir,
		registry: filepath.Join(dir, "data", "accounts.json"),
		keystore: filepath.Join(dir, "data", "keystore.json"),
		audit:    filepath.Join(dir, "data", "accounts_audit.jsonl"),
		logs:     &bytes.Buffer{},
	}
	f := accounts.File{Version: 1, Customers: []accounts.Customer{{ID: "c-001", Accounts: []accounts.Account{{
		ID: "acct-a", Exchange: "deribit", Environment: "testnet", Label: "Account A",
		Bots: []accounts.Bot{{Currency: "BTC", Config: "config_btc.yaml"}},
	}}}}}
	if err := accounts.WriteFile(e.registry, f); err != nil {
		t.Fatal(err)
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return e
}

// keyFile writes an owner-only key file and returns its path.
func (e *akEnv) keyFile(t *testing.T, secret, scope string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"client_id": ksClientID, "client_secret": secret, "scope": scope})
	path := filepath.Join(e.dir, "key-"+secret+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// run runs `accounts key <args>` against e's files; stdin feeds --key-file -.
func (e *akEnv) run(t *testing.T, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	args = append(args, "--registry", e.registry, "--keystore", e.keystore, "--audit", e.audit)
	var o, eo bytes.Buffer
	code = accounts.RunKey(args, strings.NewReader(stdin), &o, &eo)
	return code, o.String(), eo.String()
}

func (e *akEnv) keyID(t *testing.T) string {
	t.Helper()
	f, err := accounts.ReadFile(e.registry)
	if err != nil {
		t.Fatal(err)
	}
	return f.Customers[0].Accounts[0].KeyID
}

func (e *akEnv) entry(t *testing.T, keyID string) *keystore.Entry {
	t.Helper()
	entries, err := keystore.ListFile(e.keystore)
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		if entries[i].AccountID == keyID {
			return &entries[i]
		}
	}
	return nil
}

func (e *akEnv) auditLines(t *testing.T) []accounts.AuditLine {
	t.Helper()
	raw, err := os.ReadFile(e.audit)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var lines []accounts.AuditLine
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		dec := json.NewDecoder(strings.NewReader(sc.Text()))
		dec.DisallowUnknownFields()
		var l accounts.AuditLine
		if err := dec.Decode(&l); err != nil {
			t.Fatalf("audit line %q: %v", sc.Text(), err)
		}
		lines = append(lines, l)
	}
	return lines
}

func (e *akEnv) add(t *testing.T) string {
	t.Helper()
	if code, _, errOut := e.run(t, "", "add", "--account", "acct-a", "--key-file", e.keyFile(t, ksSecret, ksTradeOnly)); code != accounts.ExitOK {
		t.Fatalf("key add: exit %d: %s", code, errOut)
	}
	return e.keyID(t)
}

func (e *akEnv) check(t *testing.T) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := accounts.Check(&out, &errOut, e.registry, e.keystore)
	return code, out.String() + errOut.String()
}

// noLeak fails when any text holds key material.
func noLeak(t *testing.T, where string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		for _, secret := range []string{ksSecret, akSecret2, ksClientID, ksTradeOnly, akCanMoveFunds} {
			if strings.Contains(s, secret) {
				t.Errorf("%s holds key material %q:\n%s", where, secret, s)
			}
		}
	}
}

// Scenario: add a key.
func TestAccountsKey_AddAKey(t *testing.T) {
	e := akSetup(t)
	code, out, errOut := e.run(t, "", "add", "--account", "acct-a", "--key-file", e.keyFile(t, ksSecret, ksTradeOnly))
	if code != accounts.ExitOK {
		t.Fatalf("key add: exit %d: %s", code, errOut)
	}
	keyID := e.keyID(t)
	if keyID != "acct-a-key-1" || !strings.Contains(out, keyID) {
		t.Errorf("key_id = %q (output %q), want acct-a-key-1", keyID, out)
	}
	if !strings.Contains(out, "restart") {
		t.Errorf("output does not say a restart is needed: %q", out)
	}
	store := ksOpen(t, e.keystore, ksKey(7))
	creds, err := store.Get(keyID)
	if err != nil || creds.Secret() != ksSecret || creds.ClientID() != ksClientID || creds.Label() != "Account A" {
		t.Fatalf("Get(%s) = %v, %v; want the sealed key", keyID, creds, err)
	}
	raw, _ := os.ReadFile(e.keystore)
	noLeak(t, "the key store file", string(raw))
	if code, report := e.check(t); code != accounts.ExitOK || !strings.Contains(report, accounts.StatusOK) {
		t.Errorf("-check after add: exit %d, want 0 with the account ok:\n%s", code, report)
	}

	// The key read from stdin works the same; a second add is refused (use rotate).
	raw, _ = json.Marshal(map[string]string{"client_id": ksClientID, "client_secret": akSecret2, "scope": ksTradeOnly})
	if code, _, errOut := e.run(t, string(raw), "add", "--account", "acct-a"); code != accounts.ExitViolation || !strings.Contains(errOut, "rotate") {
		t.Errorf("add on an active key: exit %d (%s), want %d pointing to rotate", code, errOut, accounts.ExitViolation)
	}
	if e.keyID(t) != keyID {
		t.Error("a refused add moved key_id")
	}
}

// A key read from stdin; an add after a revoke replaces the revoked key.
func TestAccountsKey_AddFromStdinAfterRevoke(t *testing.T) {
	e := akSetup(t)
	first := e.add(t)
	if code, _, errOut := e.run(t, "", "revoke", "--account", "acct-a"); code != accounts.ExitOK {
		t.Fatalf("revoke: exit %d: %s", code, errOut)
	}
	raw, _ := json.Marshal(map[string]string{"client_id": ksClientID, "client_secret": akSecret2, "scope": ksTradeOnly})
	if code, _, errOut := e.run(t, string(raw), "add", "--account", "acct-a", "--key-file", "-"); code != accounts.ExitOK {
		t.Fatalf("add from stdin: exit %d: %s", code, errOut)
	}
	second := e.keyID(t)
	if second == first || e.entry(t, second).Status != keystore.StatusActive || e.entry(t, first).Status != keystore.StatusRevoked {
		t.Errorf("after re-add: key_id %q (first %q); want a new active key, the old one revoked", second, first)
	}
	creds, err := ksOpen(t, e.keystore, ksKey(7)).Get(second)
	if err != nil || creds.Secret() != akSecret2 {
		t.Errorf("Get(%s) = %v; want the stdin secret", second, err)
	}
}

// Scenario: a key that can move funds is refused (and one whose scope is unknown).
func TestAccountsKey_KeyThatCanMoveFundsRefused(t *testing.T) {
	for _, scope := range []string{akCanMoveFunds, ""} {
		e := akSetup(t)
		// add on an account with no key: nothing written.
		code, out, errOut := e.run(t, "", "add", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, scope))
		if code != accounts.ExitViolation || !strings.Contains(errOut, "refused") {
			t.Errorf("scope %q: add exit %d (%s), want %d refused", scope, code, errOut, accounts.ExitViolation)
		}
		if _, err := os.Stat(e.keystore); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("scope %q: a refused add wrote the key store", scope)
		}
		if e.keyID(t) != "" {
			t.Errorf("scope %q: a refused add set key_id", scope)
		}
		noLeak(t, "add output", out, errOut, e.logs.String())

		// rotate on a working key: the old key stays, the store and registry unchanged.
		oldID := e.add(t)
		storeBefore, _ := os.ReadFile(e.keystore)
		regBefore, _ := os.ReadFile(e.registry)
		auditBefore := len(e.auditLines(t))
		code, out, errOut = e.run(t, "", "rotate", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, scope))
		if code != accounts.ExitViolation {
			t.Errorf("scope %q: rotate exit %d, want %d", scope, code, accounts.ExitViolation)
		}
		storeAfter, _ := os.ReadFile(e.keystore)
		regAfter, _ := os.ReadFile(e.registry)
		if !bytes.Equal(storeBefore, storeAfter) || !bytes.Equal(regBefore, regAfter) || len(e.auditLines(t)) != auditBefore {
			t.Errorf("scope %q: a refused rotate wrote something", scope)
		}
		if e.keyID(t) != oldID || e.entry(t, oldID).Status != keystore.StatusActive {
			t.Errorf("scope %q: a refused rotate moved off the old key", scope)
		}
		noLeak(t, "rotate output", out, errOut, e.logs.String())
	}
}

// failingStore is the real store with one step failing.
type failingStore struct {
	*keystore.Store
	failPut    bool
	failRevoke string // the key id whose revoke fails
}

func (s failingStore) Put(keyID string, creds keystore.Credentials) error {
	if s.failPut {
		return errors.New("keystore: write: no space left on device")
	}
	return s.Store.Put(keyID, creds)
}

func (s failingStore) Revoke(keyID string) error {
	if keyID == s.failRevoke {
		return errors.New("keystore: write: no space left on device")
	}
	return s.Store.Revoke(keyID)
}

// Scenario: rotate keeps the old key until the new one is in place — a
// failure at any of its three steps leaves the account on its old key.
func TestAccountsKey_RotateKeepsOldKeyUntilNewInPlace(t *testing.T) {
	newCreds := keystore.NewCredentials("Account A", ksClientID, akSecret2, ksTradeOnly)
	steps := []struct {
		name  string
		store func(e *akEnv, s *keystore.Store, oldID string) accounts.KeyStore
		prep  func(t *testing.T, e *akEnv)
	}{
		{name: "writing the new secret", store: func(_ *akEnv, s *keystore.Store, _ string) accounts.KeyStore {
			return failingStore{Store: s, failPut: true}
		}},
		{name: "pointing key_id at it", store: func(_ *akEnv, s *keystore.Store, _ string) accounts.KeyStore { return s },
			prep: func(t *testing.T, e *akEnv) {
				if os.Geteuid() == 0 {
					t.Skip("root writes a read-only folder")
				}
				// The registry's folder read-only: the store (its own folder) still writes.
				regDir := filepath.Join(e.dir, "reg")
				if err := os.MkdirAll(regDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(e.registry, filepath.Join(regDir, "accounts.json")); err != nil {
					t.Fatal(err)
				}
				e.registry = filepath.Join(regDir, "accounts.json")
				if err := os.Chmod(regDir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(regDir, 0o700) })
			}},
		{name: "revoking the old key", store: func(_ *akEnv, s *keystore.Store, oldID string) accounts.KeyStore {
			return failingStore{Store: s, failRevoke: oldID}
		}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			e := akSetup(t)
			oldID := e.add(t)
			if step.prep != nil {
				step.prep(t, e)
			}
			s := ksOpen(t, e.keystore, ksKey(7))
			if _, _, err := accounts.RotateKey(e.registry, step.store(e, s, oldID), "acct-a", newCreds); err == nil {
				t.Fatal("RotateKey succeeded, want the step's failure")
			}
			if got := e.keyID(t); got != oldID {
				t.Errorf("key_id = %q, want the old key %q", got, oldID)
			}
			if old := e.entry(t, oldID); old == nil || old.Status != keystore.StatusActive {
				t.Errorf("old key %v, want it active", old)
			}
			for _, en := range s.List() {
				if en.AccountID != oldID && en.Status == keystore.StatusActive {
					t.Errorf("an unused new key %s was left active", en.AccountID)
				}
			}
			if creds, err := ksOpen(t, e.keystore, ksKey(7)).Get(oldID); err != nil || creds.Secret() != ksSecret {
				t.Errorf("old key no longer opens: %v", err)
			}
		})
	}

	// And the success path: new key in place, old revoked.
	e := akSetup(t)
	oldID := e.add(t)
	code, out, errOut := e.run(t, "", "rotate", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, ksTradeOnly))
	if code != accounts.ExitOK {
		t.Fatalf("rotate: exit %d: %s", code, errOut)
	}
	newID := e.keyID(t)
	if newID == oldID || e.entry(t, oldID).Status != keystore.StatusRevoked || e.entry(t, newID).Status != keystore.StatusActive {
		t.Errorf("after rotate key_id %q (old %q): want a new active key and the old one revoked", newID, oldID)
	}
	if creds, err := ksOpen(t, e.keystore, ksKey(7)).Get(newID); err != nil || creds.Secret() != akSecret2 {
		t.Errorf("new key: %v", err)
	}
	if !strings.Contains(out, "restart") || !strings.Contains(out, "reconcile") {
		t.Errorf("rotate output lacks the restart reminder: %q", out)
	}
	if code, report := e.check(t); code != accounts.ExitOK {
		t.Errorf("-check after rotate: exit %d:\n%s", code, report)
	}

	// Rotate with no active key points to add.
	e = akSetup(t)
	if code, _, errOut := e.run(t, "", "rotate", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, ksTradeOnly)); code != accounts.ExitViolation ||
		!strings.Contains(errOut, "key add") {
		t.Errorf("rotate with no key: exit %d (%s), want %d pointing to add", code, errOut, accounts.ExitViolation)
	}
}

// Scenario: revoke blocks the account.
func TestAccountsKey_RevokeBlocksAccount(t *testing.T) {
	e := akSetup(t)
	keyID := e.add(t)
	code, out, errOut := e.run(t, "", "revoke", "--account", "acct-a")
	if code != accounts.ExitOK {
		t.Fatalf("revoke: exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "restarted or stopped by a person") {
		t.Errorf("revoke output lacks the restart/stop instruction: %q", out)
	}
	if e.keyID(t) != keyID {
		t.Error("revoke moved key_id")
	}
	code, report := e.check(t)
	if code != accounts.ExitViolation || !strings.Contains(report, accounts.StatusBlocked) || !strings.Contains(report, "revoked") {
		t.Errorf("-check after revoke: exit %d, want 1 with the account blocked, 'revoked':\n%s", code, report)
	}
	if _, err := ksOpen(t, e.keystore, ksKey(7)).Get(keyID); !errors.Is(err, keystore.ErrRevoked) {
		t.Errorf("Get after revoke = %v, want ErrRevoked", err)
	}

	// Again: nothing changes and nothing more is audited.
	n := len(e.auditLines(t))
	if code, out, _ := e.run(t, "", "revoke", "--account", "acct-a"); code != accounts.ExitOK || !strings.Contains(out, "already revoked") {
		t.Errorf("second revoke: exit %d %q, want 0 'already revoked'", code, out)
	}
	if len(e.auditLines(t)) != n {
		t.Error("a revoke that changed nothing was audited")
	}

	// An account with no key has nothing to revoke.
	e = akSetup(t)
	if code, _, _ := e.run(t, "", "revoke", "--account", "acct-a"); code != accounts.ExitViolation {
		t.Errorf("revoke with no key: exit %d, want %d", code, accounts.ExitViolation)
	}
}

// Scenario: the secret never leaks — success and failure, outputs, logs,
// audit; a key file the parser cannot read is never quoted; no argument takes the key.
func TestAccountsKey_SecretNeverLeaks(t *testing.T) {
	e := akSetup(t)
	var texts []string
	record := func(_ int, out, errOut string) { texts = append(texts, out, errOut) }
	record(e.run(t, "", "add", "--account", "acct-a", "--key-file", e.keyFile(t, ksSecret, ksTradeOnly)))
	record(e.run(t, "", "add", "--account", "acct-a", "--key-file", e.keyFile(t, ksSecret, ksTradeOnly)))
	record(e.run(t, "", "rotate", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, akCanMoveFunds)))
	record(e.run(t, "", "rotate", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, ksTradeOnly)))
	record(e.run(t, "", "revoke", "--account", "acct-a"))
	// Malformed key files that hold the secret: not JSON, a wrong type, an unknown field named by it.
	for _, bad := range []string{
		ksSecret,
		`{"client_id": "` + ksClientID + `", "client_secret": ` + ksSecret + `}`,
		`{"client_id": "` + ksClientID + `", "client_secret": 7, "scope": "` + ksTradeOnly + `"}`,
		`{"client_id": "` + ksClientID + `", "` + ksSecret + `": "x"}`,
	} {
		code, out, errOut := e.run(t, bad, "add", "--account", "acct-a")
		if code == accounts.ExitOK {
			t.Errorf("malformed key file %q accepted", bad)
		}
		record(code, out, errOut)
	}
	// The key is never an argument: a positional or an unknown flag is refused.
	for _, args := range [][]string{
		{"add", "--account", "acct-a", ksSecret},
		{"add", "--account", "acct-a", "--secret", ksSecret},
	} {
		code, out, errOut := e.run(t, "", args...)
		if code != accounts.ExitUnreadable {
			t.Errorf("args %v: exit %d, want %d", args, code, accounts.ExitUnreadable)
		}
		record(code, out, errOut)
	}
	audit, _ := os.ReadFile(e.audit)
	texts = append(texts, string(audit), e.logs.String())
	noLeak(t, "a key command's output, log or audit", texts...)
}

// Scenario: every action is audited — one line per change naming the person,
// the account and the action, without key material.
func TestAccountsKey_EveryActionAudited(t *testing.T) {
	e := akSetup(t)
	e.add(t)
	e.run(t, "", "rotate", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, akCanMoveFunds)) // refused: not audited
	if code, _, errOut := e.run(t, "", "rotate", "--account", "acct-a", "--key-file", e.keyFile(t, akSecret2, ksTradeOnly)); code != accounts.ExitOK {
		t.Fatalf("rotate: %s", errOut)
	}
	if code, _, errOut := e.run(t, "", "revoke", "--account", "acct-a"); code != accounts.ExitOK {
		t.Fatalf("revoke: %s", errOut)
	}
	lines := e.auditLines(t)
	want := []string{accounts.ActionKeyAdd, accounts.ActionKeyRotate, accounts.ActionKeyRevoke}
	if len(lines) != len(want) {
		t.Fatalf("audit has %d lines, want %d: %+v", len(lines), len(want), lines)
	}
	for i, l := range lines {
		if l.Action != want[i] || l.AccountID != "acct-a" || l.Actor != accounts.Actor() || l.Actor == "" || l.Time.IsZero() {
			t.Errorf("audit line %d = %+v, want %s by %s on acct-a", i, l, want[i], accounts.Actor())
		}
	}
	raw, _ := os.ReadFile(e.audit)
	noLeak(t, "the audit trail", string(raw))
	if strings.Contains(string(raw), "-key-") {
		t.Errorf("the audit trail names a key id:\n%s", raw)
	}
	if info, err := os.Stat(e.audit); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("audit trail mode %v (%v), want 0600", info.Mode().Perm(), err)
	}
}

// Refusals before anything is written: no master key, an unknown account, a
// group-readable key file or audit trail, an unknown action, no account.
func TestAccountsKey_RefusedBeforeWriting(t *testing.T) {
	cases := []struct {
		name string
		prep func(t *testing.T, e *akEnv) []string
		want int
	}{
		{"no master key", func(t *testing.T, e *akEnv) []string {
			t.Setenv(ksMasterEnv, "")
			return []string{"add", "--account", "acct-a", "--key-file", e.keyFile(t, ksSecret, ksTradeOnly)}
		}, accounts.ExitUnreadable},
		{"unknown account", func(t *testing.T, e *akEnv) []string {
			return []string{"add", "--account", "acct-zz", "--key-file", e.keyFile(t, ksSecret, ksTradeOnly)}
		}, accounts.ExitViolation},
		{"group-readable key file", func(t *testing.T, e *akEnv) []string {
			p := e.keyFile(t, ksSecret, ksTradeOnly)
			if err := os.Chmod(p, 0o644); err != nil {
				t.Fatal(err)
			}
			return []string{"add", "--account", "acct-a", "--key-file", p}
		}, accounts.ExitUnreadable},
		{"group-readable audit trail", func(t *testing.T, e *akEnv) []string {
			if err := os.WriteFile(e.audit, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(e.audit, 0o644); err != nil {
				t.Fatal(err)
			}
			return []string{"add", "--account", "acct-a", "--key-file", e.keyFile(t, ksSecret, ksTradeOnly)}
		}, accounts.ExitUnreadable},
		{"unknown action", func(*testing.T, *akEnv) []string { return []string{"delete", "--account", "acct-a"} }, accounts.ExitUnreadable},
		{"no account", func(*testing.T, *akEnv) []string { return []string{"revoke"} }, accounts.ExitUnreadable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := akSetup(t)
			args := c.prep(t, e)
			if code, _, errOut := e.run(t, "", args...); code != c.want {
				t.Errorf("exit %d (%s), want %d", code, errOut, c.want)
			}
			if _, err := os.Stat(e.keystore); !errors.Is(err, os.ErrNotExist) {
				t.Error("the key store was written")
			}
			if e.keyID(t) != "" {
				t.Error("key_id was set")
			}
		})
	}
}
