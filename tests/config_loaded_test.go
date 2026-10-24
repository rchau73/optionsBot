package tests

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"optionsbot/internal/api"
	"optionsbot/internal/config"
	"optionsbot/internal/orders"
)

// The effective settings are journaled at every start (#256): a fingerprint,
// what changed since the last start, nothing from .env.

func shippedBTC(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("DERIBIT_ENV", "testnet")
	cfg, err := loadWithDummyCreds(t, filepath.Join("..", "config_btc.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// startOn journals a start on the journal in buf (replayed first, as main
// does) and returns the record and the line it wrote.
func startOn(t *testing.T, buf *bytes.Buffer, cfg *config.Config, now time.Time) (orders.ConfigRecord, map[string]any) {
	t.Helper()
	rp, err := orders.ReplayJournal(bytes.NewReader(buf.Bytes()), orders.RecentEvents)
	if err != nil {
		t.Fatal(err)
	}
	before := buf.Len()
	l := orders.NewWriterLogger(buf, 0)
	l.Restore(rp)
	rec := l.LogStartConfig(rp.LastConfig, cfg, "config_btc.yaml", "mvp", "v1.2.3", now)
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()[before:]), &line); err != nil {
		t.Fatalf("config_loaded line: %v", err)
	}
	return rec, line
}

// Scenario: first start.
func TestConfigLoaded_FirstStart(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	rec, line := startOn(t, &buf, shippedBTC(t), now)
	if strings.Count(buf.String(), "\n") != 1 || line["event"] != orders.EventConfig {
		t.Fatalf("want one config_loaded line, got %q", buf.String())
	}
	if line["first"] != true || line["unchanged"] != nil {
		t.Errorf("first start: first %v unchanged %v", line["first"], line["unchanged"])
	}
	if ch, ok := line["changes"].([]any); !ok || len(ch) != 0 {
		t.Errorf("first start: changes %v, want []", line["changes"])
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(rec.Fingerprint) || line["fingerprint"] != rec.Fingerprint {
		t.Errorf("fingerprint %q (line %v)", rec.Fingerprint, line["fingerprint"])
	}
	if line["version"] != "v1.2.3" || line["deployment"] != "mvp" || line["config_file"] != "config_btc.yaml" {
		t.Errorf("version/deployment/config_file: %v %v %v", line["version"], line["deployment"], line["config_file"])
	}
	if !rec.Since.Equal(now) || line["actor"] != nil {
		t.Errorf("since %v (want %v), actor %v (want none: files are edited by hand)", rec.Since, now, line["actor"])
	}
}

// Scenario: a setting changed between deploys.
func TestConfigLoaded_SettingChangedBetweenDeploys(t *testing.T) {
	var buf bytes.Buffer
	cfg := shippedBTC(t)
	if cfg.RepairCooldownHours != 72 {
		t.Fatalf("shipped repair_cooldown_hours %d, want 72", cfg.RepairCooldownHours)
	}
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	first, _ := startOn(t, &buf, cfg, t0)

	cfg.RepairCooldownHours = 48
	t1 := t0.Add(26 * time.Hour)
	rec, line := startOn(t, &buf, cfg, t1)
	if rec.Fingerprint == first.Fingerprint || rec.First || rec.Unchanged {
		t.Fatalf("changed settings: fingerprint %s (was %s), first %v, unchanged %v", rec.Fingerprint, first.Fingerprint, rec.First, rec.Unchanged)
	}
	ch := line["changes"].([]any)
	if len(ch) != 1 {
		t.Fatalf("changes %v, want only repair_cooldown_hours", ch)
	}
	c := ch[0].(map[string]any)
	if c["key"] != "repair_cooldown_hours" || c["from"] != 72.0 || c["to"] != 48.0 {
		t.Errorf("change %v, want repair_cooldown_hours 72 → 48", c)
	}
	if !rec.Since.Equal(t1) {
		t.Errorf("since %v, want this start %v", rec.Since, t1)
	}

	// And /api/status config.since is this start time.
	h := api.New(staticView{sampleView()}, orders.NewWriterLogger(&bytes.Buffer{}, 0),
		api.WithConfig(rec.Fingerprint, rec.Since)).Handler()
	_, body := getJSON(t, h, "/api/status")
	conf, ok := body["config"].(map[string]any)
	if !ok {
		t.Fatalf("/api/status config: %v", body["config"])
	}
	if conf["fingerprint"] != rec.Fingerprint || conf["since"] != t1.Format(time.RFC3339) {
		t.Errorf("/api/status config %v, want fingerprint %s since %s", conf, rec.Fingerprint, t1.Format(time.RFC3339))
	}
}

// Scenario: unchanged restart.
func TestConfigLoaded_UnchangedRestart(t *testing.T) {
	var buf bytes.Buffer
	cfg := shippedBTC(t)
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	first, _ := startOn(t, &buf, cfg, t0)
	rec, line := startOn(t, &buf, cfg, t0.Add(time.Hour))
	if line["unchanged"] != true || line["first"] != nil {
		t.Errorf("restart: unchanged %v first %v", line["unchanged"], line["first"])
	}
	if ch := line["changes"].([]any); len(ch) != 0 {
		t.Errorf("restart: changes %v, want none", ch)
	}
	if rec.Fingerprint != first.Fingerprint || !rec.Since.Equal(t0) {
		t.Errorf("restart: fingerprint %s (was %s), since %v (want the first start %v)", rec.Fingerprint, first.Fingerprint, rec.Since, t0)
	}
	// A third start still dates the settings from the first.
	third, _ := startOn(t, &buf, cfg, t0.Add(2*time.Hour))
	if !third.Since.Equal(t0) {
		t.Errorf("third start since %v, want %v", third.Since, t0)
	}
}

// Scenario: no secret ever reaches the journal.
func TestConfigLoaded_NoSecretInJournal(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	t.Setenv("BOT_ADMIN_TOKEN", "admintok-9f8e7d")
	t.Setenv("BOT_API_ADDR", "127.0.0.1:7777")
	t.Setenv("DERIBIT_CLIENT_ID", "cid-5a4b3c")
	t.Setenv("DERIBIT_CLIENT_SECRET", "csecret-1q2w3e")
	cfg, err := config.Load(filepath.Join("..", "config_btc.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	startOn(t, &buf, cfg, time.Now())
	effJSON, _ := json.Marshal(config.Effective(cfg))
	for _, where := range []string{buf.String(), string(effJSON)} {
		low := strings.ToLower(where)
		for _, bad := range []string{"cid-5a4b3c", "csecret-1q2w3e", "admintok-9f8e7d", "127.0.0.1:7777",
			"client_id", "client_secret", "clientid", "clientsecret", "admin_token", "admintoken", "api_addr", "deribit_"} {
			if strings.Contains(low, bad) {
				t.Errorf("%q reached the journal or the fingerprint input", bad)
			}
		}
	}
	// Other credentials, the same fingerprint.
	fp := config.Fingerprint(config.Effective(cfg))
	cfg.ClientID, cfg.ClientSecret, cfg.AdminToken, cfg.Environment = "x", "y", "z", "live"
	cfg.RateLimit.WsMatchRPS, cfg.Heartbeat.IntervalSec = 1, 1
	if got := config.Fingerprint(config.Effective(cfg)); got != fp {
		t.Errorf("platform settings moved the fingerprint: %s → %s", fp, got)
	}
}

// Scenario: replay is unaffected by config_loaded lines.
func TestConfigLoaded_ReplayUnaffected(t *testing.T) {
	cfg := shippedBTC(t)
	write := func(withConfig bool) []byte {
		var buf bytes.Buffer
		l := orders.NewWriterLogger(&buf, 0)
		if withConfig {
			l.LogStartConfig(nil, cfg, "config_btc.yaml", "", "", time.Now())
		}
		pos := &orders.Position{Instrument: "BTC-27NOV26-60000-P", OptionType: "put", Qty: 1, EntryPrice: 0.02}
		slot := &orders.SlotRef{DTE: 25, Delta: 0.16}
		l.LogOpen(pos, orders.Fill{FillPrice: 0.02, Qty: 1}, orders.TriggerEntry, orders.EventContext{Slot: slot})
		if withConfig {
			l.LogStartConfig(nil, cfg, "config_btc.yaml", "", "", time.Now())
		}
		l.LogClose(pos, orders.Fill{FillPrice: 0.01, Qty: 1}, orders.TriggerStopLoss200Pct, "market", orders.EventContext{Slot: slot})
		return buf.Bytes()
	}
	plain, err := orders.ReplayJournal(bytes.NewReader(write(false)), orders.RecentEvents)
	if err != nil {
		t.Fatal(err)
	}
	withCfg, err := orders.ReplayJournal(bytes.NewReader(write(true)), orders.RecentEvents)
	if err != nil {
		t.Fatal(err)
	}
	if withCfg.LastConfig == nil || plain.LastConfig != nil {
		t.Errorf("LastConfig: with %d bytes, without %d bytes", len(withCfg.LastConfig), len(plain.LastConfig))
	}
	if len(plain.Trades) != 2 || len(withCfg.Trades) != len(plain.Trades) {
		t.Fatalf("trades: %d with config lines, %d without (want 2)", len(withCfg.Trades), len(plain.Trades))
	}
	for i := range plain.Trades {
		a, b := plain.Trades[i], withCfg.Trades[i]
		a.Seq, b.Seq, a.At, b.At = 0, 0, time.Time{}, time.Time{} // the sequence counts every line
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Errorf("trade %d: %s vs %s", i, jb, ja)
		}
	}
	ja, _ := json.Marshal([]any{plain.Realised, plain.Closed})
	jb, _ := json.Marshal([]any{withCfg.Realised, withCfg.Closed})
	if string(ja) != string(jb) || plain.Skipped != 0 || withCfg.Skipped != 0 {
		t.Errorf("realised/closed %s vs %s (skipped %d/%d)", jb, ja, withCfg.Skipped, plain.Skipped)
	}
	sa, _ := json.Marshal(stopsOf(plain.Trades))
	sb, _ := json.Marshal(stopsOf(withCfg.Trades))
	if string(sa) != string(sb) {
		t.Errorf("restored stops differ: %s vs %s", sb, sa)
	}
}

// stopsOf is what a replay restores as held stops: the closes by reason.
func stopsOf(trades []orders.Trade) []string {
	var out []string
	for _, tr := range trades {
		if tr.Kind == "close" {
			out = append(out, tr.Instrument+" "+tr.Reason)
		}
	}
	return out
}

func TestEffective_KeysAndRoundTrip(t *testing.T) {
	cfg := shippedBTC(t)
	eff := config.Effective(cfg)
	for _, k := range []string{"repair_cooldown_hours", "gex_direction", "dte_delta_matrix", "iv_margin_bands",
		"backtest.fill_model", "rebalance_backoff_after"} {
		if _, ok := eff[k]; !ok {
			t.Errorf("Effective lacks %s", k)
		}
	}
	for _, k := range config.FixedKeys() {
		if k == "target_dte" || k == "entry_delta" { // legacy, never set
			continue
		}
		if _, ok := eff[k]; !ok {
			t.Errorf("Effective lacks the fixed setting %s", k)
		}
	}
	// Read back from JSON (as the next start reads the journal), the same
	// settings: no change and the same fingerprint.
	raw, _ := json.Marshal(eff)
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if d := config.Diff(back, eff); len(d) != 0 {
		t.Errorf("round trip changed %v", d)
	}
	if config.Fingerprint(back) != config.Fingerprint(eff) {
		t.Error("round trip changed the fingerprint")
	}
}

func TestDiff_NestedAndMissingKeys(t *testing.T) {
	cfg := shippedBTC(t)
	prev := config.Effective(cfg)
	cfg.Backtest.FillModel = "bid_ask"
	cfg.DTEDeltaMatrix[0].DTE = 30
	d := config.Diff(prev, config.Effective(cfg))
	if len(d) != 2 || d[0].Key != "backtest.fill_model" || d[0].From != "mid" || d[0].To != "bid_ask" || d[1].Key != "dte_delta_matrix" {
		t.Errorf("diff %+v", d)
	}
	d = config.Diff(map[string]any{"old_key": 1.0}, map[string]any{"new_key": 2.0})
	if len(d) != 2 || d[0].Key != "new_key" || d[0].From != nil || d[1].Key != "old_key" || d[1].To != nil {
		t.Errorf("added/removed keys: %+v", d)
	}
}

func TestConfigLoaded_UnreadableLastLineIsFirst(t *testing.T) {
	cfg := shippedBTC(t)
	l := orders.NewWriterLogger(&bytes.Buffer{}, 0)
	rec := l.LogStartConfig([]byte(`{"event":"config_loaded","fingerprint":`), cfg, "", "", "", time.Now())
	if !rec.First || len(rec.Changes) != 0 {
		t.Errorf("unreadable last line: first %v changes %v", rec.First, rec.Changes)
	}
}
