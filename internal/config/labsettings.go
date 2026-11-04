package config

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The lab's settings data (#586): what the monitor's POST /api/lab/validate
// checks a proposed value against, generated from the catalogue and the
// shipped config files by cmd/paramdocs (frontend/content/lab-settings.json),
// so the monitor never holds a second copy by hand. Each shipped config adds
// its current values and the bounds Validate's cross rules put on a key given
// the rest of that file (tests/lab_settings_test.go loads each bound through
// Load, so the two cannot drift). Nothing here is applied anywhere.

// LabPolicyNote is what the answer says for a core-policy setting.
const LabPolicyNote = "changing the default needs the product owner"

// LabBound is one side of a cross rule: the value, whether the value itself
// is allowed, and the rule in plain words.
type LabBound struct {
	Value     float64 `json:"value"`
	Inclusive bool    `json:"inclusive"`
	Rule      string  `json:"rule"`
}

// LabSetting is one tunable as the lab check reads it.
type LabSetting struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Unit       string   `json:"unit"`
	Kind       string   `json:"kind"`
	Min        *float64 `json:"min"`
	Max        *float64 `json:"max"`
	Step       float64  `json:"step"`
	Values     []string `json:"values,omitempty"`
	CrossRules []string `json:"cross_rules"`
	// CrossRulesChecked is false when the key's cross rules are listed but no
	// bound checks them: they depend on the whole list (the margin bands'
	// "lowest starts at 0", "no two start at the same percentile"), which one
	// proposed value does not give.
	CrossRulesChecked bool   `json:"cross_rules_checked"`
	Policy            string `json:"policy"`
	CorePolicy        bool   `json:"core_policy"`
}

// LabConfig is one shipped config: its current values and its cross-rule
// bounds, by key.
type LabConfig struct {
	File    string              `json:"file"`
	Current map[string]any      `json:"current"`
	Above   map[string]LabBound `json:"above"` // the value must be above (or at) this
	Below   map[string]LabBound `json:"below"` // the value must be below (or at) this
}

// LabData is the whole file.
type LabData struct {
	Note       string                `json:"_note"`
	PolicyNote string                `json:"policy_note"`
	Settings   []LabSetting          `json:"settings"`
	Fixed      map[string]string     `json:"fixed"`   // fixed key → its value in code
	Configs    map[string]*LabConfig `json:"configs"` // by underlying (BTC, ETH)
}

// LabSettings builds the lab data from the catalogue and the shipped
// configs (underlying → loaded config and its file name).
func LabSettings(ts []Tunable, configs map[string]*Config, files map[string]string) (*LabData, error) {
	d := &LabData{
		Note: "Generated from internal/config (catalogue.go, labsettings.go) and the shipped config files: " +
			"go run ./cmd/paramdocs. Do not edit by hand.",
		PolicyNote: LabPolicyNote,
		Fixed:      map[string]string{},
		Configs:    map[string]*LabConfig{},
	}
	for _, t := range ts {
		d.Settings = append(d.Settings, LabSetting{
			Key: t.Key, Title: t.Title, Unit: t.Unit, Kind: t.Kind, Min: t.Min, Max: t.Max, Step: t.Step,
			Values: t.Values, CrossRules: append([]string{}, t.CrossRules...), Policy: t.Policy, CorePolicy: t.Policy != PolicyNone,
		})
	}
	for k, v := range fixedKeys {
		d.Fixed[k] = v
	}
	for u, c := range configs {
		lc, err := labConfig(ts, c)
		if err != nil {
			return nil, fmt.Errorf("lab settings for %s: %w", u, err)
		}
		lc.File = files[u]
		d.Configs[u] = lc
	}
	for i := range d.Settings {
		d.Settings[i].CrossRulesChecked = boundInEvery(d.Configs, d.Settings[i].Key)
	}
	return d, nil
}

// boundInEvery reports whether every config has a cross-rule bound on key.
func boundInEvery(configs map[string]*LabConfig, key string) bool {
	if len(configs) == 0 {
		return false
	}
	for _, lc := range configs {
		_, above := lc.Above[key]
		_, below := lc.Below[key]
		if !above && !below {
			return false
		}
	}
	return true
}

// LabSettingsJSON renders the data as the file is written (keys sorted, a
// final newline).
func LabSettingsJSON(d *LabData) ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func labConfig(ts []Tunable, c *Config) (*LabConfig, error) {
	raw, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	lc := &LabConfig{Current: map[string]any{}, Above: map[string]LabBound{}, Below: map[string]LabBound{}}
	for _, t := range ts {
		lc.Current[t.Key] = valueAt(doc, strings.Split(t.Key, "."))
	}
	addCrossBounds(lc, c)
	return lc, nil
}

// valueAt reads a catalogue key from a YAML document: one value, or the list
// of every element's value for a "[]" key.
func valueAt(node any, segs []string) any {
	if len(segs) == 0 {
		return node
	}
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	name, isList := strings.CutSuffix(segs[0], "[]")
	if !isList {
		return valueAt(m[name], segs[1:])
	}
	list, _ := m[name].([]any)
	out := []any{}
	for _, el := range list {
		v := valueAt(el, segs[1:])
		if sub, ok := v.([]any); ok && len(segs) > 1 {
			out = append(out, sub...)
			continue
		}
		out = append(out, v)
	}
	return out
}

// addCrossBounds turns Validate's cross rules into bounds on one key, the
// rest of the config as it is. A list key's bound applies to each element.
func addCrossBounds(lc *LabConfig, c *Config) {
	slots := c.Slots()
	if len(slots) == 0 {
		return
	}
	minDelta, maxDelta, minDTE := slots[0].EntryDelta, slots[0].EntryDelta, slots[0].TargetDTE
	for _, s := range slots {
		minDelta = min(minDelta, s.EntryDelta)
		maxDelta = max(maxDelta, s.EntryDelta)
		minDTE = min(minDTE, s.TargetDTE)
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', 4, 64) }
	// Rounded to 1e-9, Validate's own tolerance: 0.75 × 0.10 is 0.075, not 0.07500000000000001.
	r := func(v float64) float64 { return math.Round(v*1e9) / 1e9 }
	ratio := fmt.Sprintf("%.0f %%", MaxDriftToEntryRatio*100)

	lc.Below["delta_drift_threshold"] = LabBound{Value: r(MaxDriftToEntryRatio * minDelta), Inclusive: true,
		Rule: fmt.Sprintf("must be at most %s of every entry delta: the lowest is %s, so at most %s", ratio, f(minDelta), f(MaxDriftToEntryRatio*minDelta))}
	lc.Above["delta_exit_threshold"] = LabBound{Value: maxDelta,
		Rule: fmt.Sprintf("must be above every entry delta: the highest is %s", f(maxDelta))}
	lc.Above["dte_delta_matrix[].deltas[]"] = LabBound{Value: r(c.DeltaDriftThreshold / MaxDriftToEntryRatio), Inclusive: true,
		Rule: fmt.Sprintf("must keep delta_drift_threshold (%s) at most %s of it, so at least %s", f(c.DeltaDriftThreshold), ratio, f(c.DeltaDriftThreshold/MaxDriftToEntryRatio))}
	lc.Below["dte_delta_matrix[].deltas[]"] = LabBound{Value: c.DeltaExitThreshold,
		Rule: fmt.Sprintf("must be below delta_exit_threshold (%s), or a new leg would be exited at once", f(c.DeltaExitThreshold))}
	lc.Above["dte_delta_matrix[].dte"] = LabBound{Value: float64(c.RolloutDTE),
		Rule: fmt.Sprintf("must be above rollout_dte (%d), or a new strangle would be rolled at once", c.RolloutDTE)}
	lc.Below["rollout_dte"] = LabBound{Value: float64(minDTE),
		Rule: fmt.Sprintf("must be below every slot's days to expiry: the lowest is %d", minDTE)}
}

// LabShippedConfigs are the configs the lab answers for, by underlying: the
// files the stable bots run (mvp's BTC differs only in gex_direction).
var LabShippedConfigs = map[string]string{"BTC": "config_btc.yaml", "ETH": "config_eth.yaml"}

// ShippedLabSettingsJSON loads the shipped configs under root and renders
// the lab data (cmd/paramdocs).
func ShippedLabSettingsJSON(root string) ([]byte, error) {
	configs := map[string]*Config{}
	for u, file := range LabShippedConfigs {
		c, err := Load(filepath.Join(root, file))
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", file, err)
		}
		configs[u] = c
	}
	d, err := LabSettings(Tunables(), configs, LabShippedConfigs)
	if err != nil {
		return nil, err
	}
	return LabSettingsJSON(d)
}
