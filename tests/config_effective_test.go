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
