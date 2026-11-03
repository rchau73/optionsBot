package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"optionsbot/internal/config"
)

// The lab's settings data (#586): frontend/content/lab-settings.json is
// generated from the catalogue and the shipped configs (go run
// ./cmd/paramdocs), and every cross-rule bound in it is the one Validate
// enforces, loaded through config.Load here — so the monitor's
// POST /api/lab/validate cannot drift from the bot.

const labSettingsFile = "frontend/content/lab-settings.json"

func readLabData(t *testing.T) config.LabData {
	t.Helper()
	var d config.LabData
	if err := json.Unmarshal(readRepoFile(t, labSettingsFile), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// Regenerate: go run ./cmd/paramdocs (or UPDATE_GOLDEN=1).
func TestLabSettings_InSyncWithCatalogue(t *testing.T) {
	t.Setenv("DERIBIT_ENV", "testnet")
	got, err := config.ShippedLabSettingsJSON("..")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", labSettingsFile)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if string(readRepoFile(t, labSettingsFile)) != string(got) {
		t.Error(labSettingsFile + " is out of date with the catalogue or the shipped configs: run go run ./cmd/paramdocs")
	}
}

func TestLabSettings_EveryTunableAndFixedKey(t *testing.T) {
	d := readLabData(t)
	if len(d.Settings) != len(config.Tunables()) {
		t.Errorf("settings: %d, catalogue: %d", len(d.Settings), len(config.Tunables()))
	}
	for _, s := range d.Settings {
		if s.CorePolicy != (s.Policy != config.PolicyNone) {
			t.Errorf("%s: core_policy %v with policy %q", s.Key, s.CorePolicy, s.Policy)
		}
		for u, c := range d.Configs {
			if _, ok := c.Current[s.Key]; !ok {
				t.Errorf("%s: no current value of %s", u, s.Key)
			}
		}
	}
	if len(d.Fixed) != len(config.FixedKeys()) {
		t.Errorf("fixed keys: %d, fixed.go: %d", len(d.Fixed), len(config.FixedKeys()))
	}
	if d.PolicyNote != config.LabPolicyNote || !strings.Contains(d.PolicyNote, "product owner") {
		t.Errorf("policy note %q", d.PolicyNote)
	}
	btc := d.Configs["BTC"]
	if btc == nil || btc.File != "config_btc.yaml" || btc.Current["rollout_dte"] != float64(15) {
		t.Errorf("BTC: %+v", btc)
	}
}

// loadWithKey loads a shipped config with every value at a catalogue key set
// to v (no other key moved), and returns Load's error.
func loadWithKey(t *testing.T, file, key string, v any) error {
	t.Helper()
	doc := readYAMLDoc(t, file)
	setAll(doc, strings.Split(key, "."), v)
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DERIBIT_ENV", "testnet")
	_, err = loadWithDummyCreds(t, path)
	return err
}

// Scenario: value breaks a cross rule — each bound is where Validate starts
// refusing: at the bound as `inclusive` says, one step outside refused, one
// step inside accepted.
func TestLabSettings_CrossBoundsMatchValidate(t *testing.T) {
	d := readLabData(t)
	kinds := map[string]string{}
	for _, s := range d.Settings {
		kinds[s.Key] = s.Kind
	}
	for u, c := range d.Configs {
		check := func(key string, b config.LabBound, outward float64) {
			step := 0.001
			val := func(x float64) any { return x }
			if kinds[key] == config.KindInteger {
				step = 1
				val = func(x float64) any { return int(x) }
			}
			if b.Rule == "" {
				t.Errorf("%s %s: a bound without its rule", u, key)
			}
			cases := []struct {
				v    float64
				want bool
			}{
				{b.Value, b.Inclusive},
				{b.Value + outward*step, false},
				{b.Value - outward*step, true},
			}
			for _, cs := range cases {
				err := loadWithKey(t, c.File, key, val(cs.v))
				if (err == nil) != cs.want {
					t.Errorf("%s %s = %v: loads %v, want %v (err %v)", c.File, key, cs.v, err == nil, cs.want, err)
				}
			}
		}
		for key, b := range c.Above {
			check(key, b, -1)
		}
		for key, b := range c.Below {
			check(key, b, +1)
		}
		for _, key := range []string{"delta_drift_threshold", "delta_exit_threshold", "rollout_dte", "dte_delta_matrix[].deltas[]", "dte_delta_matrix[].dte"} {
			if _, a := c.Above[key]; !a {
				if _, b := c.Below[key]; !b {
					t.Errorf("%s: no cross bound for %s", u, key)
				}
			}
		}
	}
}
