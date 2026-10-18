package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"optionsbot/internal/config"
)

// The strategy settings each shipped config produces — every field after
// defaults and fixed values, platform (.env) fields cleared — must stay what
// the bots trade with. The golden files were written before the settings
// were fixed in code (2026-10-14), so moving a key from YAML into code cannot
// change a value unnoticed. Regenerate only for an intended rule change:
// UPDATE_GOLDEN=1 go test ./tests -run TestConfig_EffectiveSettingsUnchanged
func TestConfig_EffectiveSettingsUnchanged(t *testing.T) {
	for _, coin := range []string{"btc", "eth"} {
		t.Run(coin, func(t *testing.T) {
			t.Setenv("DERIBIT_ENV", "testnet")
			cfg, err := loadWithDummyCreds(t, filepath.Join("..", "config_"+coin+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			cfg.ClientID, cfg.ClientSecret = "", ""
			cfg.Environment, cfg.APIAddr, cfg.AdminToken = "", "", ""
			cfg.RateLimit, cfg.Retry = config.RateLimitConfig{}, config.RetryConfig{}
			cfg.Circuit, cfg.Heartbeat = config.CircuitConfig{}, config.HeartbeatConfig{}
			cfg.AccountPollSec = 0
			if len(cfg.TargetDTE) == 0 {
				cfg.TargetDTE = nil // the legacy key is empty in both files
			}
			got, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "effective_config_"+coin+".json")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(append(got, '\n')) != string(want) {
				t.Errorf("effective %s settings changed:\n got %s\nwant %s", coin, got, want)
			}
		})
	}
}

// The mvp environment's BTC config (#168) is config_btc.yaml with one
// difference — the direction source — so stable and mvp can be compared on the
// same BTC moves. Any other difference would make that comparison meaningless.
func TestConfig_MVPBTCDiffersOnlyInDirection(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	base, err := loadWithDummyCreds(t, filepath.Join("..", "config_btc.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mvp, err := loadWithDummyCreds(t, filepath.Join("..", "config_btc.mvp.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if base.GEXDirection != "bias" || mvp.GEXDirection != "sma" {
		t.Fatalf("gex_direction: config_btc.yaml %q (want bias, stable), config_btc.mvp.yaml %q (want sma)", base.GEXDirection, mvp.GEXDirection)
	}
	mvp.GEXDirection = base.GEXDirection
	a, _ := json.Marshal(base)
	b, _ := json.Marshal(mvp)
	if string(a) != string(b) {
		t.Errorf("config_btc.mvp.yaml must equal config_btc.yaml except gex_direction:\n base %s\n mvp  %s", a, b)
	}
}
