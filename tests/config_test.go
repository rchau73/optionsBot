package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/config"
	"optionsbot/internal/risk"
)

// validConfigBase is the smallest config that passes Validate. Test bodies
// add keys that are not in it (YAML rejects duplicate keys).
const validConfigBase = `underlying: BTC
rollout_dte: 19
max_mm_pct: 35
stop_loss_multiplier: 2.0
`

// validSlots is appended when a test does not define its own slots.
const validSlots = `dte_delta_matrix:
  - dte: 45
    deltas: [0.16]
`

// writeTempConfig writes a minimal config yaml to a temp file and returns its path.
// The caller is responsible for cleanup via t.Cleanup or os.Remove.
func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	full := validConfigBase + body
	if !strings.Contains(body, "dte_delta_matrix") {
		full += validSlots
	}
	if err := os.WriteFile(path, []byte(full), 0600); err != nil {
		t.Fatalf("writeTempConfig: %v", err)
	}
	return path
}

// loadWithDummyCreds sets the required credential env vars for the duration of
// the test, then calls config.Load.
func loadWithDummyCreds(t *testing.T, path string) (*config.Config, error) {
	t.Helper()
	t.Setenv("DERIBIT_CLIENT_ID", "test-id")
	t.Setenv("DERIBIT_CLIENT_SECRET", "test-secret")
	return config.Load(path)
}

// ── Margin policy ────────────────────────────────────────────────────────────

func TestConfigLoad_MarginPolicyDefaults(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.RiskPolicy(true)
	b := p.SortedBands()
	if len(b) != 3 || b[0] != (risk.Band{MinIVPct: 70, MaxIMPct: 50}) || b[2] != (risk.Band{MinIVPct: 0, MaxIMPct: 20}) {
		t.Errorf("default bands = %+v", b)
	}
	if p.MaxMMPct != 35 || p.ConfirmDays != 2 || !p.UseRegime {
		t.Errorf("policy = %+v", p)
	}
}

func TestConfigLoad_CustomBands(t *testing.T) {
	path := writeTempConfig(t, "iv_margin_bands:\n  - { min_iv_pct: 0, max_im_pct: 10 }\n  - { min_iv_pct: 50, max_im_pct: 40 }\niv_band_confirm_days: 3\n")
	cfg, err := loadWithDummyCreds(t, path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.RiskPolicy(false)
	if p.ConfirmDays != 3 || p.SortedBands()[0].MaxIMPct != 40 {
		t.Errorf("policy = %+v", p)
	}
}

func TestConfigLoad_RejectsBadBands(t *testing.T) {
	cases := map[string]string{
		"no zero band":     "iv_margin_bands:\n  - { min_iv_pct: 30, max_im_pct: 35 }\n",
		"limit above 100":  "iv_margin_bands:\n  - { min_iv_pct: 0, max_im_pct: 120 }\n",
		"duplicate floor":  "iv_margin_bands:\n  - { min_iv_pct: 0, max_im_pct: 20 }\n  - { min_iv_pct: 0, max_im_pct: 30 }\n",
		"negative confirm": "iv_band_confirm_days: -1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadWithDummyCreds(t, writeTempConfig(t, body)); err == nil {
				t.Error("want a validation error")
			}
		})
	}
}

// The old cap settings must not be silently ignored: someone who still sets
// leverage would believe it is enforced.
func TestConfigLoad_RejectsRemovedMarginKeys(t *testing.T) {
	for _, key := range []string{"leverage: 4.0", "max_margin_pct: 0.35", "max_leverage: 10"} {
		_, err := loadWithDummyCreds(t, writeTempConfig(t, key+"\n"))
		if err == nil || !strings.Contains(err.Error(), "was removed") || !strings.Contains(err.Error(), "iv_margin_bands") {
			t.Errorf("%s: want a removed-key error, got %v", key, err)
		}
	}
}

// ── Validation ───────────────────────────────────────────────────────────────

func TestConfigValidate_RejectsUnsafeSettings(t *testing.T) {
	tests := []struct {
		name, body, wantErr string
	}{
		{"delta not OTM", "dte_delta_matrix:\n  - dte: 45\n    deltas: [0.6]\n", "entry delta"},
		{"zero delta", "dte_delta_matrix:\n  - dte: 45\n    deltas: [0]\n", "entry delta"},
		{"slot inside rollout window", "dte_delta_matrix:\n  - dte: 15\n    deltas: [0.16]\n", "rollout_dte"},
		{"no slots", "dte_delta_matrix: []\n", "no strangle slots"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWithDummyCreds(t, writeTempConfig(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	// Keys that live in the base are replaced wholesale.
	raw := []struct {
		name, from, to, wantErr string
	}{
		{"MM limit at liquidation", "max_mm_pct: 35", "max_mm_pct: 100", "max_mm_pct"},
		{"no stop-loss", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 0", "stop_loss_multiplier"},
		{"negative flip buffer", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\ngamma_flip_buffer_sd: -1", "gamma_flip_buffer_sd"},
		{"flip buffer too wide", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\ngamma_flip_buffer_sd: 4", "gamma_flip_buffer_sd"},
		{"expiry stretch below 1", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\nexpiry_stretch: 0.5", "expiry_stretch"},
		{"expiry stretch above 3", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\nexpiry_stretch: 4", "expiry_stretch"},
		{"drift threshold at the entry delta (open-and-roll churn)", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\ndelta_drift_threshold: 0.16", "delta_drift_threshold"},
		{"drift threshold just above 75% of the entry delta", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\ndelta_drift_threshold: 0.121", "too close"},
		{"negative drift threshold", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\ndelta_drift_threshold: -0.1", "delta_drift_threshold"},
		{"delta exit at the entry delta", "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\ndelta_exit_threshold: 0.16", "delta_exit_threshold"},
		{"no underlying", "underlying: BTC", "underlying: \"\"", "underlying"},
	}
	for _, tc := range raw {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			body := strings.Replace(validConfigBase+validSlots, tc.from, tc.to, 1)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := loadWithDummyCreds(t, path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestConfigValidate_RejectsUnknownEnvironment(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "Live") // typo: must not silently fall back
	_, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err == nil || !strings.Contains(err.Error(), "DERIBIT_ENV") {
		t.Errorf("want DERIBIT_ENV error, got %v", err)
	}
}

func TestConfigLoad_DefaultsToTestnet(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "")
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsLive() || cfg.Environment != "testnet" {
		t.Errorf("environment = %q, want testnet by default", cfg.Environment)
	}
	if !strings.Contains(cfg.WSEndpoint(), "test.deribit.com") {
		t.Errorf("testnet endpoint expected, got %s", cfg.WSEndpoint())
	}
}

func TestConfigLoad_LiveNeedsExplicitOptIn(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "live")
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsLive() || cfg.WSEndpoint() != "wss://www.deribit.com/ws/api/v2" {
		t.Errorf("live endpoint expected, got %s", cfg.WSEndpoint())
	}
}

// A backtest never talks to Deribit, so it must load without API keys.
func TestConfigLoad_CredentialsOnlyRequiredForTrading(t *testing.T) {
	t.Setenv("DERIBIT_CLIENT_ID", "")
	t.Setenv("DERIBIT_CLIENT_SECRET", "")
	cfg, err := config.Load(writeTempConfig(t, ""))
	if err != nil {
		t.Fatalf("config without credentials should load for backtests: %v", err)
	}
	if err := cfg.RequireCredentials(); err == nil {
		t.Error("RequireCredentials must fail without keys")
	}
}

func TestConfigLoad_MissingFileAndBadYAML(t *testing.T) {
	if _, err := config.Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing file should error")
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	os.WriteFile(bad, []byte("underlying: [unclosed"), 0600)
	if _, err := config.Load(bad); err == nil {
		t.Error("malformed YAML should error")
	}
}

func TestConfigLoad_PlatformSettingsFromEnv(t *testing.T) {
	t.Setenv("DERIBIT_RATE_WS_MATCH_RPS", "3")
	t.Setenv("DERIBIT_CIRCUIT_BREAKER_THRESHOLD", "9")
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RateLimit.WsMatchRPS != 3 || cfg.Circuit.Threshold != 9 {
		t.Errorf("env overrides not applied: %+v %+v", cfg.RateLimit, cfg.Circuit)
	}
	if cfg.EvalIntervalMS <= 0 || cfg.OrderFillTimeoutSec <= 0 {
		t.Error("logic defaults should be filled in")
	}
}

// The shipped configs must always load and validate.
func TestConfigLoad_RepositoryConfigsAreValid(t *testing.T) {
	for _, name := range []string{"config_btc.yaml", "config_eth.yaml"} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadWithDummyCreds(t, filepath.Join("..", name)); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		})
	}
}

func TestConfigLoad_RepairCooldown(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil || cfg.RepairCooldownHours != 72 {
		t.Errorf("default repair_cooldown_hours = %d (%v), want 72", cfg.RepairCooldownHours, err)
	}
	if _, err := loadWithDummyCreds(t, writeTempConfig(t, "repair_cooldown_hours: -1\n")); err == nil || !strings.Contains(err.Error(), "repair_cooldown_hours") {
		t.Errorf("a negative cooldown must be rejected, got %v", err)
	}
}

func TestConfigLoad_ChurnBreaker(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil || cfg.ChurnMaxRoundTrips != 3 || cfg.ChurnWindowMinutes != 60 {
		t.Errorf("default churn breaker = %d in %d min (%v), want 3 in 60", cfg.ChurnMaxRoundTrips, cfg.ChurnWindowMinutes, err)
	}
	if _, err := loadWithDummyCreds(t, writeTempConfig(t, "churn_window_minutes: -5\n")); err == nil || !strings.Contains(err.Error(), "churn_window_minutes") {
		t.Errorf("a negative window must be rejected, got %v", err)
	}
}

func TestConfigLoad_PriceFloors(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil || cfg.EntryPriceFloor != "mid" || cfg.RepairPriceFloor != "bid" {
		t.Errorf("default floors = %q / %q (%v), want mid / bid", cfg.EntryPriceFloor, cfg.RepairPriceFloor, err)
	}
	if _, err := loadWithDummyCreds(t, writeTempConfig(t, "repair_price_floor: market\n")); err == nil || !strings.Contains(err.Error(), "repair_price_floor") {
		t.Errorf("an unknown floor must be rejected, got %v", err)
	}
}

func TestConfigLoad_RebalanceRetry(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil || cfg.RebalanceRetryMinutes != 15 {
		t.Errorf("default rebalance_retry_minutes = %d (%v), want 15", cfg.RebalanceRetryMinutes, err)
	}
	if _, err := loadWithDummyCreds(t, writeTempConfig(t, "rebalance_retry_minutes: -1\n")); err == nil || !strings.Contains(err.Error(), "rebalance_retry_minutes") {
		t.Errorf("a negative retry delay must be rejected, got %v", err)
	}
}

func TestConfigLoad_MaxLegSizeMultiple(t *testing.T) {
	cfg, err := loadWithDummyCreds(t, writeTempConfig(t, ""))
	if err != nil || cfg.MaxLegSizeMultiple != 2 {
		t.Errorf("default max_leg_size_multiple = %v (%v), want 2", cfg.MaxLegSizeMultiple, err)
	}
	if _, err := loadWithDummyCreds(t, writeTempConfig(t, "max_leg_size_multiple: 0.5\n")); err == nil || !strings.Contains(err.Error(), "max_leg_size_multiple") {
		t.Errorf("a multiple below 1 must be rejected, got %v", err)
	}
}

// Today's settings (drift 0.10 under 0.16 entries) and the 75 % edge pass;
// both shipped configs must load.
func TestConfig_DriftThresholdBelowEntryDeltaIsAccepted(t *testing.T) {
	for _, drift := range []string{"0.10", "0.12", "0"} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		body := strings.Replace(validConfigBase+validSlots, "stop_loss_multiplier: 2.0", "stop_loss_multiplier: 2.0\ndelta_drift_threshold: "+drift, 1)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadWithDummyCreds(t, path); err != nil {
			t.Errorf("drift %s: %v", drift, err)
		}
	}
	for _, f := range []string{"../config_btc.yaml", "../config_eth.yaml"} {
		if _, err := loadWithDummyCreds(t, f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
