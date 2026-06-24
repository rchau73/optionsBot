package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"optionsbot/internal/config"
)

// writeTempConfig writes a minimal config yaml to a temp file and returns its path.
// The caller is responsible for cleanup via t.Cleanup or os.Remove.
func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("underlying: BTC\n"+body), 0600); err != nil {
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

// ── Leverage validation ───────────────────────────────────────────────────────

func TestLeverageConfig_ExceedsMaxRejected(t *testing.T) {
	path := writeTempConfig(t, "leverage: 15.0\nmax_leverage: 10.0\n")
	_, err := loadWithDummyCreds(t, path)
	if err == nil {
		t.Fatal("expected error when leverage exceeds max_leverage, got nil")
	}
	if !strings.Contains(err.Error(), "leverage") {
		t.Errorf("error message should mention leverage, got: %v", err)
	}
}

func TestLeverageConfig_EqualToMaxAllowed(t *testing.T) {
	path := writeTempConfig(t, "leverage: 10.0\nmax_leverage: 10.0\n")
	cfg, err := loadWithDummyCreds(t, path)
	if err != nil {
		t.Fatalf("leverage == max_leverage should be allowed: %v", err)
	}
	if cfg.Leverage != 10.0 {
		t.Errorf("Leverage = %.2f, want 10.0", cfg.Leverage)
	}
}

func TestLeverageConfig_ValidLeverageLoaded(t *testing.T) {
	path := writeTempConfig(t, "leverage: 2.0\nmax_leverage: 10.0\n")
	cfg, err := loadWithDummyCreds(t, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Leverage != 2.0 {
		t.Errorf("Leverage = %.2f, want 2.0", cfg.Leverage)
	}
	if cfg.MaxLeverage != 10.0 {
		t.Errorf("MaxLeverage = %.2f, want 10.0", cfg.MaxLeverage)
	}
}

func TestLeverageConfig_NegativeLeverageDefaultsToOne(t *testing.T) {
	// Negative leverage is nonsensical; Load silently clamps it to 1.0.
	path := writeTempConfig(t, "leverage: -3.0\n")
	cfg, err := loadWithDummyCreds(t, path)
	if err != nil {
		t.Fatalf("negative leverage should be clamped, not rejected: %v", err)
	}
	if cfg.Leverage != 1.0 {
		t.Errorf("Leverage = %.2f, want 1.0 (clamped from negative)", cfg.Leverage)
	}
}

func TestLeverageConfig_ZeroLeverageDefaultsToOne(t *testing.T) {
	path := writeTempConfig(t, "leverage: 0\n")
	cfg, err := loadWithDummyCreds(t, path)
	if err != nil {
		t.Fatalf("zero leverage should be clamped to 1.0: %v", err)
	}
	if cfg.Leverage != 1.0 {
		t.Errorf("Leverage = %.2f, want 1.0 (clamped from zero)", cfg.Leverage)
	}
}

func TestLeverageConfig_MaxLeverageZeroDefaultsTen(t *testing.T) {
	// Omitting max_leverage (or setting it to 0) should default to 10.
	path := writeTempConfig(t, "leverage: 2.0\n")
	cfg, err := loadWithDummyCreds(t, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxLeverage != 10.0 {
		t.Errorf("MaxLeverage = %.2f, want 10.0 (default)", cfg.MaxLeverage)
	}
}

func TestLeverageConfig_OmittedLeverageDefaultsToOne(t *testing.T) {
	// A config with no leverage field should behave exactly as before (1×).
	path := writeTempConfig(t, "")
	cfg, err := loadWithDummyCreds(t, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Leverage != 1.0 {
		t.Errorf("Leverage = %.2f, want 1.0 when omitted", cfg.Leverage)
	}
}
