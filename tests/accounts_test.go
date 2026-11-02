package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/accounts"
	"optionsbot/internal/gateway"
	"optionsbot/internal/keystore"
)

// Account registry (#585): customer → exchange account → bots, joined to the
// key store's key-scope statuses.

func acctFile() accounts.File {
	return accounts.File{Version: 1, Customers: []accounts.Customer{
		{ID: "c-001", Accounts: []accounts.Account{{
			ID: "acct-stable", Exchange: "deribit", Environment: "testnet", Label: "Stable", KeyID: "key-stable",
			Bots: []accounts.Bot{{Currency: "BTC", Config: "config_btc.yaml"}, {Currency: "ETH", Config: "config_eth.yaml"}},
		}}},
		{ID: "c-002", Accounts: []accounts.Account{{
			ID: "acct-mvp", Exchange: "deribit", Environment: "testnet", Label: "MVP", KeyID: "key-mvp",
			Bots: []accounts.Bot{{Currency: "BTC", Config: "config_btc.mvp.yaml"}},
		}}},
	}}
}

// acctKeystore writes a key store file with the given key id → scope status,
// as keystore.Put would leave it (no key material is needed to list it). A
// can_move_funds record cannot come from Put; it stands for a store written
// before the refusal or by hand.
func acctKeystore(t *testing.T, scopes map[string]string) string {
	t.Helper()
	recs := map[string]map[string]any{}
	for id, scope := range scopes {
		recs[id] = map[string]any{"label": id, "status": "active", "key_scope": scope, "updated_at": "2026-11-02T00:00:00Z"}
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "accounts": recs})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "keystore.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func acctWrite(t *testing.T, f accounts.File) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "accounts.json")
	if err := accounts.WriteFile(path, f); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// Scenario: two bots on one account — Validate fails naming both bots.
func TestAccounts_TwoBotsOnOneAccount(t *testing.T) {
	f := acctFile()
	f.Customers[1].Accounts[0].Bots = append(f.Customers[1].Accounts[0].Bots, accounts.Bot{Currency: "BTC", Config: "config_btc.yaml"})
	err := f.Validate()
	if !errors.Is(err, accounts.ErrInvalid) {
		t.Fatalf("Validate = %v, want ErrInvalid", err)
	}
	for _, want := range []string{`"acct-mvp"`, "BTC (config_btc.mvp.yaml)", "BTC (config_btc.yaml)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	// The same rule when the registry is loaded from a file.
	path := filepath.Join(t.TempDir(), "accounts.json")
	raw, _ := json.Marshal(f)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Load(path, nil); !errors.Is(err, accounts.ErrInvalid) {
		t.Errorf("Load = %v, want ErrInvalid", err)
	}
	// A BTC and an ETH bot on one account are two units (account × currency): valid.
	if err := acctFile().Validate(); err != nil {
		t.Errorf("BTC + ETH on one account: %v", err)
	}
}

// Every other rule, each break named.
func TestAccounts_ValidateRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(f *accounts.File)
		want string
	}{
		{"version", func(f *accounts.File) { f.Version = 2 }, "version 2"},
		{"customer email", func(f *accounts.File) { f.Customers[0].ID = "jane@example.com" }, "not an opaque id"},
		{"customer name", func(f *accounts.File) { f.Customers[0].ID = "Jane Doe" }, "not an opaque id"},
		{"duplicate customer", func(f *accounts.File) { f.Customers[1].ID = "c-001" }, `customer "c-001" appears twice`},
		{"duplicate account", func(f *accounts.File) { f.Customers[1].Accounts[0].ID = "acct-stable" }, `account "acct-stable" appears twice`},
		{"unknown exchange", func(f *accounts.File) { f.Customers[0].Accounts[0].Exchange = "okx" }, `exchange "okx"`},
		{"unknown environment", func(f *accounts.File) { f.Customers[0].Accounts[0].Environment = "prod" }, `environment "prod"`},
		{"empty label", func(f *accounts.File) { f.Customers[0].Accounts[0].Label = " " }, "empty label"},
		{"shared key", func(f *accounts.File) { f.Customers[1].Accounts[0].KeyID = "key-stable" }, `key "key-stable" is used by accounts "acct-stable" and "acct-mvp"`},
		{"config path", func(f *accounts.File) { f.Customers[0].Accounts[0].Bots[0].Config = "../config_btc.yaml" }, "config must be a file name"},
		{"currency", func(f *accounts.File) { f.Customers[0].Accounts[0].Bots[0].Currency = "btc" }, `currency "btc"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := acctFile()
			tc.edit(&f)
			err := f.Validate()
			if !errors.Is(err, accounts.ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate = %v, want ErrInvalid naming %q", err, tc.want)
			}
		})
	}
}

// KeyStatus: only an active trade-only key leaves the account usable.
func TestAccounts_KeyStatus(t *testing.T) {
	entry := func(status, scope string) *keystore.Entry {
		return &keystore.Entry{AccountID: "k", Status: status, KeyScope: scope}
	}
	cases := []struct {
		name, keyID       string
		entry             *keystore.Entry
		scope, status, in string
	}{
		{"trade only", "k", entry(keystore.StatusActive, gateway.KeyScopeTradeOnly), gateway.KeyScopeTradeOnly, accounts.StatusOK, ""},
		{"can move funds", "k", entry(keystore.StatusActive, gateway.KeyScopeCanMoveFunds), gateway.KeyScopeCanMoveFunds, accounts.StatusBlocked, "can move funds"},
		{"unknown", "k", entry(keystore.StatusActive, gateway.KeyScopeUnknown), gateway.KeyScopeUnknown, accounts.StatusBlocked, "unknown"},
		{"empty scope", "k", entry(keystore.StatusActive, ""), "", accounts.StatusBlocked, "unknown"},
		{"revoked", "k", entry(keystore.StatusRevoked, gateway.KeyScopeTradeOnly), gateway.KeyScopeTradeOnly, accounts.StatusBlocked, "revoked"},
		{"missing", "k", nil, accounts.ScopeMissing, accounts.StatusBlocked, "not in the key store"},
		{"no key", "", nil, accounts.ScopeNoKey, accounts.StatusBlocked, "names no key"},
	}
	for _, tc := range cases {
		scope, status, reason := accounts.KeyStatus(tc.keyID, tc.entry)
		if scope != tc.scope || status != tc.status || !strings.Contains(reason, tc.in) || (tc.in == "") != (reason == "") {
			t.Errorf("%s: KeyStatus = (%q, %q, %q), want (%q, %q, …%q…)", tc.name, scope, status, reason, tc.scope, tc.status, tc.in)
		}
	}
}

// Scenario: a key that can move funds — the account loads blocked with the
// reason, and no secret appears in the output.
func TestAccounts_KeyThatCanMoveFundsBlocksAccount(t *testing.T) {
	path := acctWrite(t, acctFile())
	ksPath := acctKeystore(t, map[string]string{"key-stable": gateway.KeyScopeCanMoveFunds, "key-mvp": gateway.KeyScopeTradeOnly})
	keys, err := keystore.ListFile(ksPath)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := accounts.Load(path, keys)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := reg.Get("acct-stable")
	if !ok || got.Status != accounts.StatusBlocked || got.KeyScope != gateway.KeyScopeCanMoveFunds || !strings.Contains(got.Reason, "can move funds") {
		t.Errorf("acct-stable = %+v, want blocked: can move funds", got)
	}
	if mvp, _ := reg.Get("acct-mvp"); mvp.Status != accounts.StatusOK {
		t.Errorf("acct-mvp = %+v, want ok", mvp)
	}
	if reg.Blocked() != 1 || len(reg.List()) != 2 {
		t.Errorf("Blocked = %d of %d, want 1 of 2", reg.Blocked(), len(reg.List()))
	}

	var out, errOut bytes.Buffer
	if code := accounts.Check(&out, &errOut, path, ksPath); code != accounts.ExitViolation {
		t.Errorf("Check exit = %d, want %d", code, accounts.ExitViolation)
	}
	if !strings.Contains(out.String(), "can move funds") {
		t.Errorf("output does not give the reason:\n%s", out.String())
	}

	// A real sealed key: its client id and secret never reach the output.
	sealedPath := filepath.Join(t.TempDir(), "keystore.json")
	st := ksOpen(t, sealedPath, ksKey(1))
	for _, id := range []string{"key-stable", "key-mvp"} {
		if err := st.Put(id, keystore.NewCredentials("Account", ksClientID, ksSecret, ksTradeOnly)); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	errOut.Reset()
	accounts.Check(&out, &errOut, path, sealedPath)
	for _, secret := range []string{ksClientID, ksSecret, ksTradeOnly} {
		if strings.Contains(out.String()+errOut.String(), secret) {
			t.Errorf("output holds key material %q", secret)
		}
	}
}

// Scenario: a clean registry — one row per account, exit 0.
func TestAccounts_CleanRegistryCheck(t *testing.T) {
	path := acctWrite(t, acctFile())
	ksPath := acctKeystore(t, map[string]string{"key-stable": gateway.KeyScopeTradeOnly, "key-mvp": gateway.KeyScopeTradeOnly})
	var out, errOut bytes.Buffer
	if code := accounts.Check(&out, &errOut, path, ksPath); code != accounts.ExitOK {
		t.Fatalf("Check exit = %d, want 0; stderr: %s", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 { // header + one row per account
		t.Fatalf("got %d lines, want header + 2 rows:\n%s", len(lines), out.String())
	}
	for i, want := range []string{"acct-stable", "acct-mvp"} {
		row := lines[i+1]
		for _, field := range []string{want, "deribit", "testnet", gateway.KeyScopeTradeOnly, accounts.StatusOK} {
			if !strings.Contains(row, field) {
				t.Errorf("row %q lacks %q", row, field)
			}
		}
	}
}

// Check's failures: an invalid registry, a missing file, an unreadable key store.
func TestAccounts_CheckFailures(t *testing.T) {
	ksPath := acctKeystore(t, map[string]string{"key-stable": gateway.KeyScopeTradeOnly, "key-mvp": gateway.KeyScopeTradeOnly})
	var out, errOut bytes.Buffer
	if code := accounts.Check(&out, &errOut, filepath.Join(t.TempDir(), "none.json"), ksPath); code != accounts.ExitUnreadable {
		t.Errorf("missing registry: exit %d, want %d", code, accounts.ExitUnreadable)
	}

	bad := acctFile()
	bad.Customers[1].Accounts[0].KeyID = "key-stable"
	badPath := filepath.Join(t.TempDir(), "accounts.json")
	raw, _ := json.Marshal(bad)
	if err := os.WriteFile(badPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := accounts.Check(&out, &errOut, badPath, ksPath); code != accounts.ExitViolation || !strings.Contains(errOut.String(), "key-stable") {
		t.Errorf("invalid registry: exit %d (%s), want %d naming the key", code, errOut.String(), accounts.ExitViolation)
	}

	if err := os.Chmod(ksPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := accounts.Check(&out, &errOut, acctWrite(t, acctFile()), ksPath); code != accounts.ExitUnreadable {
		t.Errorf("group-readable key store: exit %d, want %d", code, accounts.ExitUnreadable)
	}

	// A missing key store: every account blocked (its key is not in the store).
	out.Reset()
	if code := accounts.Check(&out, &errOut, acctWrite(t, acctFile()), filepath.Join(t.TempDir(), "none.json")); code != accounts.ExitViolation ||
		!strings.Contains(out.String(), accounts.ScopeMissing) {
		t.Errorf("missing key store: exit %d, want %d with keys missing:\n%s", code, accounts.ExitViolation, out.String())
	}
}

// Scenario: file permissions — the written file is owner-only; a
// group-readable one, an unknown field or an invalid registry is refused.
func TestAccounts_FileOwnerOnly(t *testing.T) {
	path := acctWrite(t, acctFile())
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("registry mode = %04o, want 0600", perm)
	}
	if f, err := accounts.ReadFile(path); err != nil || len(f.Customers) != 2 {
		t.Errorf("ReadFile = %+v, %v", f, err)
	}

	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Load(path, nil); !errors.Is(err, accounts.ErrPermissions) {
		t.Errorf("group-readable: Load = %v, want ErrPermissions", err)
	}

	stray := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(stray, []byte(`{"version":1,"customers":[{"id":"c-1","email":"a@b.c","accounts":[]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.ReadFile(stray); err == nil || !strings.Contains(err.Error(), "email") {
		t.Errorf("unknown field: ReadFile = %v, want refused", err)
	}

	bad := acctFile()
	bad.Version = 0
	badPath := filepath.Join(t.TempDir(), "accounts.json")
	if err := accounts.WriteFile(badPath, bad); !errors.Is(err, accounts.ErrInvalid) {
		t.Errorf("WriteFile invalid = %v, want ErrInvalid", err)
	}
	if _, err := os.Stat(badPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an invalid registry was written: %v", err)
	}
}

// keystore.ListFile reads statuses without the master key and never a secret.
func TestKeystore_ListFileWithoutMasterKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keystore.json")
	st := ksOpen(t, path, ksKey(1))
	if err := st.Put("acct-1", ksCreds()); err != nil {
		t.Fatal(err)
	}
	got, err := keystore.ListFile(path)
	if err != nil || len(got) != 1 || got[0].AccountID != "acct-1" || got[0].KeyScope != gateway.KeyScopeTradeOnly {
		t.Fatalf("ListFile = %+v, %v", got, err)
	}
	if empty, err := keystore.ListFile(filepath.Join(t.TempDir(), "none.json")); err != nil || len(empty) != 0 {
		t.Errorf("missing file: ListFile = %+v, %v, want empty", empty, err)
	}
	if err := os.Chmod(path, 0o604); err != nil {
		t.Fatal(err)
	}
	if _, err := keystore.ListFile(path); !errors.Is(err, keystore.ErrPermissions) {
		t.Errorf("world-readable: ListFile = %v, want ErrPermissions", err)
	}
}
