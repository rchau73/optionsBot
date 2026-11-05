package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/accounts"
	"optionsbot/internal/gateway"
	"optionsbot/internal/keystore"
)

// cmd/accounts -check -json and -out (#655): the registry and each account's
// key status as a key-free document for the monitor's per-account view.

var acctCheckedAt = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// acctThree is a registry with three accounts on three keys.
func acctThree() accounts.File {
	f := acctFile()
	f.Customers = append(f.Customers, accounts.Customer{ID: "c-003", Accounts: []accounts.Account{{
		ID: "acct-third", Exchange: "deribit", Environment: "live", Label: "Third", KeyID: "key-third",
		Bots: []accounts.Bot{{Currency: "ETH", Config: "config_eth.yaml"}},
	}}})
	return f
}

// acctKeystoreRecords writes a key store file from key id → (status, scope).
func acctKeystoreRecords(t *testing.T, recs map[string][2]string) string {
	t.Helper()
	out := map[string]map[string]any{}
	for id, r := range recs {
		out[id] = map[string]any{"label": id, "status": r[0], "key_scope": r[1], "updated_at": "2026-11-02T00:00:00Z"}
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "accounts": out})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "keystore.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// acctDecodeStrict decodes a status document with unknown fields refused.
func acctDecodeStrict(t *testing.T, raw []byte) accounts.StatusDocument {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc accounts.StatusDocument
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("decode status document (unknown fields refused): %v\n%s", err, raw)
	}
	return doc
}

func acctCheckJSON(t *testing.T, registry, keystorePath, outPath string) (int, []byte, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := accounts.CheckWith(&out, &errOut, registry, keystorePath,
		accounts.CheckOptions{JSON: true, OutPath: outPath, Now: acctCheckedAt})
	return code, out.Bytes(), errOut.String()
}

// Scenario: JSON lists every account with its status.
func TestAccountsJSON_ListsEveryAccountWithItsStatus(t *testing.T) {
	ksPath := acctKeystoreRecords(t, map[string][2]string{
		"key-stable": {"active", gateway.KeyScopeTradeOnly},
		"key-mvp":    {"revoked", gateway.KeyScopeTradeOnly},
		"key-third":  {"active", gateway.KeyScopeCanMoveFunds},
	})
	code, raw, _ := acctCheckJSON(t, acctWrite(t, acctThree()), ksPath, "")
	if code != accounts.ExitViolation {
		t.Errorf("exit = %d, want %d", code, accounts.ExitViolation)
	}
	doc := acctDecodeStrict(t, raw)
	got := map[string]string{}
	for _, a := range doc.Accounts {
		got[a.AccountID] = a.KeyStatus
	}
	want := map[string]string{"acct-stable": accounts.KeyOK, "acct-mvp": accounts.KeyRevoked, "acct-third": accounts.KeyCanMoveFunds}
	if len(doc.Accounts) != 3 {
		t.Fatalf("got %d accounts, want 3: %+v", len(doc.Accounts), doc.Accounts)
	}
	for id, status := range want {
		if got[id] != status {
			t.Errorf("%s key_status = %q, want %q", id, got[id], status)
		}
	}
	stable := doc.Accounts[0]
	if stable.AccountID != "acct-stable" || stable.Label != "Stable" || stable.Exchange != "deribit" || stable.Environment != "testnet" ||
		len(stable.Bots) != 2 || stable.Bots[0] != (accounts.Bot{Currency: "BTC", Config: "config_btc.yaml"}) {
		t.Errorf("acct-stable = %+v", stable)
	}
	if doc.Version != 1 || !doc.CheckedAt.Equal(acctCheckedAt) || len(doc.Violations) != 0 {
		t.Errorf("document = version %d, checked_at %s, violations %v", doc.Version, doc.CheckedAt, doc.Violations)
	}
	// A clean registry: exit 0, every key ok.
	clean := acctKeystoreRecords(t, map[string][2]string{
		"key-stable": {"active", gateway.KeyScopeTradeOnly}, "key-mvp": {"active", gateway.KeyScopeTradeOnly},
		"key-third": {"active", gateway.KeyScopeTradeOnly},
	})
	if code, _, errOut := acctCheckJSON(t, acctWrite(t, acctThree()), clean, ""); code != accounts.ExitOK {
		t.Errorf("clean registry: exit %d (%s), want 0", code, errOut)
	}
}

// KeyState: one word per KeyStatus case.
func TestAccountsJSON_KeyState(t *testing.T) {
	ksPath := acctKeystoreRecords(t, map[string][2]string{
		"key-stable": {"active", gateway.KeyScopeUnknown},
		"key-third":  {"active", ""},
	})
	f := acctThree()
	f.Customers[1].Accounts[0].KeyID = "" // acct-mvp names no key
	code, raw, _ := acctCheckJSON(t, acctWrite(t, f), ksPath, "")
	doc := acctDecodeStrict(t, raw)
	want := []string{accounts.KeyUnknownScope, accounts.KeyMissing, accounts.KeyUnknownScope}
	for i, a := range doc.Accounts {
		if a.KeyStatus != want[i] {
			t.Errorf("%s key_status = %q, want %q", a.AccountID, a.KeyStatus, want[i])
		}
	}
	if code != accounts.ExitViolation {
		t.Errorf("exit = %d, want 1", code)
	}
	// A key id the store does not hold is missing too.
	code, raw, _ = acctCheckJSON(t, acctWrite(t, acctThree()), filepath.Join(t.TempDir(), "none.json"), "")
	for _, a := range acctDecodeStrict(t, raw).Accounts {
		if a.KeyStatus != accounts.KeyMissing {
			t.Errorf("no key store: %s key_status = %q, want missing", a.AccountID, a.KeyStatus)
		}
	}
	if code != accounts.ExitViolation {
		t.Errorf("no key store: exit = %d, want 1", code)
	}
}

// Scenario: No secret can appear — not the key material, the key id, the
// client id nor a raw scope string, in the output or the status file.
func TestAccountsJSON_NoSecretCanAppear(t *testing.T) {
	sealedPath := filepath.Join(t.TempDir(), "keystore.json")
	st := ksOpen(t, sealedPath, ksKey(1))
	for _, id := range []string{"key-stable", "key-mvp"} {
		if err := st.Put(id, keystore.NewCredentials("Account", ksClientID, ksSecret, ksTradeOnly)); err != nil {
			t.Fatal(err)
		}
	}
	outPath := filepath.Join(t.TempDir(), "data", "accounts_status.json")
	code, raw, _ := acctCheckJSON(t, acctWrite(t, acctFile()), sealedPath, outPath)
	if code != accounts.ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	file, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file, raw) {
		t.Errorf("status file differs from the printed document")
	}
	for _, secret := range []string{ksClientID, ksSecret, ksTradeOnly, "key-stable", "key-mvp", "key_id", gateway.KeyScopeTradeOnly, "c-001"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("output holds %q:\n%s", secret, raw)
		}
	}
	doc := acctDecodeStrict(t, file)
	if len(doc.Accounts) != 2 || doc.Accounts[0].KeyStatus != accounts.KeyOK {
		t.Errorf("document = %+v", doc)
	}
}

// Scenario: A broken registry is reported, not hidden — two bots on one
// account and currency are listed by rule name, exit 1.
func TestAccountsJSON_BrokenRegistryIsReported(t *testing.T) {
	f := acctFile()
	f.Customers[1].Accounts[0].Bots = append(f.Customers[1].Accounts[0].Bots, accounts.Bot{Currency: "BTC", Config: "config_btc.yaml"})
	f.Customers[1].Accounts[0].KeyID = "key-stable" // and a shared key: its id must not leak
	path := filepath.Join(t.TempDir(), "accounts.json")
	raw, _ := json.Marshal(f)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ksPath := acctKeystore(t, map[string]string{"key-stable": gateway.KeyScopeTradeOnly})
	outPath := filepath.Join(t.TempDir(), "accounts_status.json")
	code, out, errOut := acctCheckJSON(t, path, ksPath, outPath)
	if code != accounts.ExitViolation {
		t.Errorf("exit = %d, want 1", code)
	}
	doc := acctDecodeStrict(t, out)
	want := []accounts.StatusViolation{
		{Rule: accounts.RuleOneAccountPerKey, AccountID: "acct-mvp"},
		{Rule: accounts.RuleOneBotPerCurrency, AccountID: "acct-mvp"},
	}
	if len(doc.Violations) != len(want) || doc.Violations[0] != want[0] || doc.Violations[1] != want[1] {
		t.Errorf("violations = %+v, want %+v", doc.Violations, want)
	}
	if len(doc.Accounts) != 0 {
		t.Errorf("an invalid registry lists accounts: %+v", doc.Accounts)
	}
	if bytes.Contains(out, []byte("key-stable")) {
		t.Errorf("output quotes the key id:\n%s", out)
	}
	if !strings.Contains(errOut, "two bots trade BTC") {
		t.Errorf("stderr lacks the break's text: %s", errOut)
	}
	if file, err := os.ReadFile(outPath); err != nil || !bytes.Equal(file, out) {
		t.Errorf("status file = %q, %v; want the printed document", file, err)
	}

	// Each rule has a name; a break on a non-opaque account id names no account.
	rules := []struct {
		name    string
		edit    func(f *accounts.File)
		rule    string
		account string
	}{
		{"version", func(f *accounts.File) { f.Version = 2 }, accounts.RuleVersion, ""},
		{"customer email", func(f *accounts.File) { f.Customers[0].ID = "jane@example.com" }, accounts.RuleOpaqueID, ""},
		{"account email", func(f *accounts.File) { f.Customers[0].Accounts[0].ID = "jane@example.com" }, accounts.RuleOpaqueID, ""},
		{"duplicate customer", func(f *accounts.File) { f.Customers[1].ID = "c-001" }, accounts.RuleUniqueID, ""},
		{"duplicate account", func(f *accounts.File) { f.Customers[1].Accounts[0].ID = "acct-stable" }, accounts.RuleUniqueID, "acct-stable"},
		{"exchange", func(f *accounts.File) { f.Customers[0].Accounts[0].Exchange = "okx" }, accounts.RuleKnownExchange, "acct-stable"},
		{"environment", func(f *accounts.File) { f.Customers[0].Accounts[0].Environment = "prod" }, accounts.RuleKnownEnvironment, "acct-stable"},
		{"label", func(f *accounts.File) { f.Customers[0].Accounts[0].Label = "" }, accounts.RuleLabel, "acct-stable"},
		{"currency", func(f *accounts.File) { f.Customers[0].Accounts[0].Bots[0].Currency = "btc" }, accounts.RuleCurrencyCode, "acct-stable"},
		{"config", func(f *accounts.File) { f.Customers[0].Accounts[0].Bots[0].Config = "../x.yaml" }, accounts.RuleConfigFileName, "acct-stable"},
	}
	for _, tc := range rules {
		f := acctFile()
		tc.edit(&f)
		vs := f.Violations()
		if len(vs) == 0 || vs[0].Rule != tc.rule || vs[0].AccountID != tc.account {
			t.Errorf("%s: Violations = %+v, want rule %q on %q", tc.name, vs, tc.rule, tc.account)
		}
	}
}

// Scenario: The status file is private and atomic — owner-only, and an
// interrupted second run (its temp file written, never renamed) leaves the
// first complete document in place.
func TestAccountsJSON_StatusFileIsPrivateAndAtomic(t *testing.T) {
	registry := acctWrite(t, acctFile())
	ksPath := acctKeystore(t, map[string]string{"key-stable": gateway.KeyScopeTradeOnly, "key-mvp": gateway.KeyScopeTradeOnly})
	dir := filepath.Join(t.TempDir(), "data")
	outPath := filepath.Join(dir, "accounts_status.json")
	if code, _, errOut := acctCheckJSON(t, registry, ksPath, outPath); code != accounts.ExitOK {
		t.Fatalf("exit = %d (%s)", code, errOut)
	}
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("status file mode = %04o, want 0600", perm)
	}
	first, _ := os.ReadFile(outPath)

	// The interrupted run: a half-written temp file next to the status file.
	if err := os.WriteFile(filepath.Join(dir, ".accounts_status-123.tmp"), first[:len(first)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(outPath); !bytes.Equal(got, first) {
		t.Errorf("status file changed by an interrupted run")
	}
	acctDecodeStrict(t, first)

	// The next run replaces it whole, still owner-only, even over a file a
	// person widened to 0644.
	if err := os.Chmod(outPath, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	later := acctCheckedAt.Add(time.Hour)
	if code := accounts.CheckWith(&out, &errOut, registry, ksPath, accounts.CheckOptions{OutPath: outPath, Now: later}); code != accounts.ExitOK {
		t.Fatalf("second run: exit %d (%s)", code, errOut.String())
	}
	second, _ := os.ReadFile(outPath)
	if doc := acctDecodeStrict(t, second); !doc.CheckedAt.Equal(later) {
		t.Errorf("second document checked_at = %s, want %s", doc.CheckedAt, later)
	}
	if info, _ := os.Stat(outPath); info.Mode().Perm() != 0o600 {
		t.Errorf("replaced status file mode = %04o, want 0600", info.Mode().Perm())
	}
	// -out without -json prints the table, as before.
	if !strings.HasPrefix(out.String(), "CUSTOMER") {
		t.Errorf("-out alone: stdout = %q, want the table", out.String())
	}
}

// Scenario: An unreadable file — exit 2 and no -out file replaced.
func TestAccountsJSON_UnreadableFile(t *testing.T) {
	ksPath := acctKeystore(t, map[string]string{"key-stable": gateway.KeyScopeTradeOnly, "key-mvp": gateway.KeyScopeTradeOnly})
	bad := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "accounts_status.json")
	previous := []byte(`{"previous": true}`)
	if err := os.WriteFile(outPath, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := acctCheckJSON(t, bad, ksPath, outPath)
	if code != accounts.ExitUnreadable || len(out) != 0 {
		t.Errorf("invalid JSON registry: exit %d, stdout %q; want 2 and nothing", code, out)
	}
	// An unreadable key store too.
	if err := os.Chmod(ksPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := acctCheckJSON(t, acctWrite(t, acctFile()), ksPath, outPath); code != accounts.ExitUnreadable {
		t.Errorf("group-readable key store: exit %d, want 2", code)
	}
	if got, _ := os.ReadFile(outPath); !bytes.Equal(got, previous) {
		t.Errorf("status file replaced: %s", got)
	}
	// A status file that cannot be written is exit 2 as well.
	if err := os.Chmod(ksPath, 0o600); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := acctCheckJSON(t, acctWrite(t, acctFile()), ksPath, filepath.Join(blocker, "status.json")); code != accounts.ExitUnreadable {
		t.Errorf("unwritable -out: exit %d, want 2", code)
	}
}

// Scenario: The default is unchanged — no -json, the table and exit codes
// are exactly as before.
func TestAccountsJSON_DefaultIsUnchanged(t *testing.T) {
	registry := acctWrite(t, acctFile())
	ksPath := acctKeystore(t, map[string]string{"key-stable": gateway.KeyScopeCanMoveFunds, "key-mvp": gateway.KeyScopeTradeOnly})
	var a, aErr, b, bErr bytes.Buffer
	codeA := accounts.Check(&a, &aErr, registry, ksPath)
	codeB := accounts.CheckWith(&b, &bErr, registry, ksPath, accounts.CheckOptions{Now: acctCheckedAt})
	if codeA != accounts.ExitViolation || codeB != codeA || a.String() != b.String() || aErr.String() != bErr.String() {
		t.Errorf("CheckWith zero options differs from Check: %d vs %d\n%s\n%s", codeA, codeB, a.String(), b.String())
	}
	lines := strings.Split(strings.TrimRight(a.String(), "\n"), "\n")
	wantHeader := []string{"CUSTOMER", "ACCOUNT", "LABEL", "EXCHANGE", "ENVIRONMENT", "BOTS", "KEY SCOPE", "STATUS", "REASON"}
	if len(lines) != 3 || strings.Join(strings.Fields(strings.ReplaceAll(lines[0], "KEY SCOPE", "KEY_SCOPE")), "|") !=
		strings.ReplaceAll(strings.Join(wantHeader, "|"), "KEY SCOPE", "KEY_SCOPE") {
		t.Errorf("table = %q", a.String())
	}
	for _, row := range lines[1:] {
		if strings.Contains(row, "key_status") || strings.Contains(row, "{") {
			t.Errorf("table row %q holds JSON", row)
		}
	}
	if !strings.Contains(lines[1], "acct-stable") || !strings.Contains(lines[1], gateway.KeyScopeCanMoveFunds) || !strings.Contains(lines[1], "can move funds") {
		t.Errorf("row %q lacks the key scope and reason", lines[1])
	}
	if aErr.String() != "accounts: 1 of 2 accounts blocked\n" {
		t.Errorf("stderr = %q", aErr.String())
	}
}
