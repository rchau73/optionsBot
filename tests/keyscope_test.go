package tests

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	gw "optionsbot/internal/gateway"
	"optionsbot/internal/orders"
)

// The API key's scope at startup (#538): parsed, reported, warn only.

func TestParseKeyScope(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		name, scope string
		can         *bool
		status      string
		funds       []string
	}{
		{"trade-only", "account:read trade:read_write", &no, gw.KeyScopeTradeOnly, []string{}},
		{"Deribit's usual extras", "connection mainaccount account:read trade:read_write wallet:none block_trade:read", &no, gw.KeyScopeTradeOnly, []string{}},
		{"wallet read only", "account:read trade:read_write wallet:read", &no, gw.KeyScopeTradeOnly, []string{}},
		{"withdrawal rights", "account:read trade:read_write wallet:read_write", &yes, gw.KeyScopeCanMoveFunds, []string{"wallet:read_write"}},
		{"account management", "account:read_write trade:read_write", &yes, gw.KeyScopeCanMoveFunds, []string{"account:read_write"}},
		{"custody", "trade:read_write custody:read_write", &yes, gw.KeyScopeCanMoveFunds, []string{"custody:read_write"}},
		{"empty", "", nil, gw.KeyScopeUnknown, []string{}},
		{"blank", "   ", nil, gw.KeyScopeUnknown, []string{}},
		{"no name:access scope", "connection", nil, gw.KeyScopeUnknown, []string{}},
	} {
		ks := gw.ParseKeyScope(c.scope)
		if !reflect.DeepEqual(ks.CanMoveFunds, c.can) || ks.Status != c.status || !reflect.DeepEqual(ks.FundScopes, c.funds) {
			t.Errorf("%s: %q → can=%v status=%s funds=%v", c.name, c.scope, ptrStr(ks.CanMoveFunds), ks.Status, ks.FundScopes)
		}
		if len(ks.Scopes) != len(strings.Fields(c.scope)) {
			t.Errorf("%s: scopes = %v", c.name, ks.Scopes)
		}
	}
}

func ptrStr(b *bool) string {
	if b == nil {
		return "nil"
	}
	if *b {
		return "true"
	}
	return "false"
}

// keyScopeRun connects a gateway whose auth reply grants scope, with every
// log line captured, and returns it with /api/status's key_scope and the logs.
// Placeholder credentials: deliberately low-entropy so the repository's
// secret scan (gitleaks) does not take them for real keys.
const (
	fakeAccountID       = "dummy-id-1111"
	fakeAccountPassword = "dummy-pw-2222"
	fakeAuthReply       = "dummy-tk-3333"
)

func keyScopeRun(t *testing.T, scope string) (*gw.Gateway, map[string]any, func() string) {
	t.Helper()
	var buf bytes.Buffer
	lw := &lockedWriter{w: &buf}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(lw, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	logs := func() string {
		lw.mu.Lock()
		defer lw.mu.Unlock()
		return buf.String()
	}

	m := newMockDeribit(t)
	reply, _ := json.Marshal(map[string]string{"access_token": fakeAuthReply, "scope": scope})
	m.on("public/auth", func(gw.JSONRPCRequest) mockReply { return mockReply{result: string(reply)} })
	cfg := testGatewayConfig()
	cfg.ClientID, cfg.ClientSecret = fakeAccountID, fakeAccountPassword
	g := connectGateway(t, m, cfg) // the bot still starts: Connect succeeds

	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(&bytes.Buffer{}, 0), api.WithKeyScope(g)).Handler()
	_, body := getJSON(t, h, "/api/status")
	ks, ok := body["key_scope"].(map[string]any)
	if !ok {
		t.Fatalf("key_scope = %v", body["key_scope"])
	}
	return g, ks, logs
}

// errorLines are the ERROR log lines.
func errorLines(logs string) []string {
	var out []string
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, `"level":"ERROR"`) {
			out = append(out, l)
		}
	}
	return out
}

// Scenario: trade-only key.
func TestKeyScope_TradeOnlyKey(t *testing.T) {
	_, ks, logs := keyScopeRun(t, "account:read trade:read_write")
	if ks["can_move_funds"] != false || ks["status"] != gw.KeyScopeTradeOnly {
		t.Errorf("key_scope = %v", ks)
	}
	if e := errorLines(logs()); len(e) != 0 {
		t.Errorf("ERROR logged for a trade-only key: %v", e)
	}
}

// Scenario: key that can move funds.
func TestKeyScope_KeyThatCanMoveFunds(t *testing.T) {
	g, ks, logs := keyScopeRun(t, "account:read trade:read_write wallet:read_write")
	if ks["can_move_funds"] != true || ks["status"] != gw.KeyScopeCanMoveFunds {
		t.Errorf("key_scope = %v", ks)
	}
	e := errorLines(logs())
	if len(e) != 1 || !strings.Contains(e[0], "wallet:read_write") || !strings.Contains(e[0], "withdrawal rights are refused by policy") {
		t.Errorf("ERROR lines = %v", e)
	}
	// The bot still starts: the gateway is connected and answers calls.
	if _, err := g.Call(t.Context(), "public/test", map[string]any{}, gw.PriorityLow); err != nil {
		t.Errorf("gateway not usable after a broad key: %v", err)
	}
}

// Scenario: unparsable scope.
func TestKeyScope_UnparsableScope(t *testing.T) {
	_, ks, logs := keyScopeRun(t, "")
	if v, present := ks["can_move_funds"]; !present || v != nil || ks["status"] != gw.KeyScopeUnknown {
		t.Errorf("an empty scope must be unknown (null), never false: %v", ks)
	}
	if !strings.Contains(logs(), "API key scope unreadable") {
		t.Errorf("no warning for an unreadable scope")
	}
	// Not wired at all (monitor demo, tests): null. Wired before any
	// authentication: unknown.
	_, body := getJSON(t, api.New(staticView{sampleView()}, orders.NewWriterLogger(&bytes.Buffer{}, 0)).Handler(), "/api/status")
	if body["key_scope"] != nil {
		t.Errorf("unwired key_scope = %v", body["key_scope"])
	}
	fresh := gw.New(testGatewayConfig())
	_, body = getJSON(t, api.New(staticView{sampleView()}, orders.NewWriterLogger(&bytes.Buffer{}, 0), api.WithKeyScope(fresh)).Handler(), "/api/status")
	if k := body["key_scope"].(map[string]any); k["can_move_funds"] != nil || k["status"] != gw.KeyScopeUnknown {
		t.Errorf("before authentication key_scope = %v", k)
	}
}

// Scenario: no secret — no log line, status field or journal line holds the
// client id, secret or token.
func TestKeyScope_NoSecret(t *testing.T) {
	g, ks, logs := keyScopeRun(t, "account:read trade:read_write wallet:read_write")
	status, _ := json.Marshal(ks)

	var journal bytes.Buffer
	scope, ok := g.KeyScope()
	if !ok {
		t.Fatal("no scope after authentication")
	}
	l := orders.NewWriterLogger(&journal, 0)
	l.LogKeyScope("short-strangle", scope, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC))

	var line map[string]any
	if err := json.Unmarshal(journal.Bytes(), &line); err != nil {
		t.Fatalf("journal line: %v (%s)", err, journal.String())
	}
	if line["event"] != orders.EventKeyScope || line["can_move_funds"] != true || line["status"] != gw.KeyScopeCanMoveFunds {
		t.Errorf("journal line = %v", line)
	}
	for what, text := range map[string]string{"logs": logs(), "status": string(status), "journal": journal.String()} {
		for _, fake := range []string{fakeAccountID, fakeAccountPassword, fakeAuthReply} {
			if strings.Contains(text, fake) {
				t.Errorf("%s holds %q", what, fake)
			}
		}
	}
	if counts := l.EventCounts(); counts[orders.EventKeyScope] != 1 {
		t.Errorf("event counts = %v", counts)
	}
}
