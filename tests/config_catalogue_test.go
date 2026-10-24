package tests

import (
	"flag"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"optionsbot/internal/config"
)

// The tunable-parameter catalogue (#255): every setting a shipped config
// holds is described, its range is the one Validate enforces, and
// docs/parameters.md is generated from it.

var shippedConfigs = []string{"config_btc.yaml", "config_btc.mvp.yaml", "config_eth.yaml"}

func catalogueByKey(t *testing.T) map[string]config.Tunable {
	t.Helper()
	out := map[string]config.Tunable{}
	for _, tu := range config.Tunables() {
		if _, dup := out[tu.Key]; dup {
			t.Errorf("catalogue: %s listed twice", tu.Key)
		}
		out[tu.Key] = tu
	}
	return out
}

// yamlPaths lists every leaf of a YAML document as a catalogue key
// ("iv_margin_bands[].max_im_pct").
func yamlPaths(prefix string, node any, out map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for k, sub := range v {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			yamlPaths(p, sub, out)
		}
	case []any:
		for _, sub := range v {
			yamlPaths(prefix+"[]", sub, out)
		}
	default:
		out[prefix] = true
	}
}

func readYAMLDoc(t *testing.T, file string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", file))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func topKey(key string) string {
	if i := strings.IndexAny(key, ".["); i >= 0 {
		return key[:i]
	}
	return key
}

// Scenario: every tuned setting is catalogued.
func TestCatalogue_EveryTunedSettingIsCatalogued(t *testing.T) {
	cat := catalogueByKey(t)
	for _, file := range shippedConfigs {
		paths := map[string]bool{}
		yamlPaths("", readYAMLDoc(t, file), paths)
		for p := range paths {
			if _, ok := cat[p]; !ok {
				t.Errorf("%s sets %s, which has no entry in config.Tunables()", file, p)
			}
		}
	}
	fixed := map[string]bool{}
	for _, k := range config.FixedKeys() {
		fixed[k] = true
	}
	for key := range cat {
		if fixed[topKey(key)] {
			t.Errorf("catalogue entry %s is a key fixed in code (fixed.go)", key)
		}
	}
	// Every YAML key of Config — not only those the shipped files set — has
	// an entry: a new tuned key needs one.
	ct := reflect.TypeFor[config.Config]()
	for i := 0; i < ct.NumField(); i++ {
		name, _, _ := strings.Cut(ct.Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		found := false
		for key := range cat {
			if topKey(key) == name {
				found = true
			}
		}
		if !found {
			t.Errorf("YAML key %s (Config.%s) has no catalogue entry", name, ct.Field(i).Name)
		}
	}
}

func TestCatalogue_EntriesWellFormed(t *testing.T) {
	policies := map[string]bool{config.PolicyEntry: true, config.PolicyExits: true, config.PolicyRepair: true,
		config.PolicyMargin: true, config.PolicyGEX: true, config.PolicySizing: true, config.PolicyNone: true}
	kinds := map[string]bool{config.KindNumber: true, config.KindInteger: true, config.KindEnum: true, config.KindText: true, config.KindBool: true}
	for _, tu := range config.Tunables() {
		if tu.Title == "" || tu.Plain == "" {
			t.Errorf("%s: title and plain text are required", tu.Key)
		}
		if !policies[tu.Policy] {
			t.Errorf("%s: policy %q is not a core-policy area", tu.Key, tu.Policy)
		}
		if !kinds[tu.Kind] {
			t.Errorf("%s: kind %q", tu.Key, tu.Kind)
		}
		if (tu.Min != nil || tu.Max != nil) && tu.Step <= 0 {
			t.Errorf("%s: a range needs a step", tu.Key)
		}
		if tu.Kind == config.KindEnum && len(tu.Values) == 0 {
			t.Errorf("%s: an enum needs its values", tu.Key)
		}
		if tu.Policy != config.PolicyNone && len(tu.VariedBy) == 0 {
			t.Errorf("%s: a core-policy setting needs a tool that can test it", tu.Key)
		}
	}
}

// Each Plain text is short and promises nothing.
func TestCatalogue_PlainTextsShortAndNoPromise(t *testing.T) {
	banned := []string{"guarantee", "profit will", "risk-free", "risk free", "sure profit", "can't lose", "cannot lose"}
	for _, tu := range config.Tunables() {
		if n := len([]rune(tu.Plain)); n > 300 {
			t.Errorf("%s: plain text is %d characters (max 300)", tu.Key, n)
		}
		low := strings.ToLower(tu.Plain)
		for _, w := range banned {
			if strings.Contains(low, w) {
				t.Errorf("%s: plain text contains %q", tu.Key, w)
			}
		}
	}
}

// setAll sets every value at a catalogue key ("[]" = each element).
func setAll(node any, segs []string, val any) {
	name, isList := strings.CutSuffix(segs[0], "[]")
	m := node.(map[string]any)
	if !isList {
		if len(segs) == 1 {
			m[name] = val
			return
		}
		setAll(m[name], segs[1:], val)
		return
	}
	list := m[name].([]any)
	for i := range list {
		if len(segs) == 1 {
			list[i] = val
		} else {
			setAll(list[i], segs[1:], val)
		}
	}
}

// rangeFixtures make the rest of the shipped config compatible with an
// extreme value, so the range test sees the key's own bound and not a cross
// rule (which TestCatalogue_CrossRules checks).
var rangeFixtures = map[string]func(doc map[string]any, v float64){
	// Extreme entry deltas: drift and exit thresholds out of the way.
	"dte_delta_matrix[].deltas[]": func(doc map[string]any, v float64) {
		doc["delta_drift_threshold"], doc["delta_exit_threshold"] = 0.0, 0.99
		setAll(doc, []string{"dte_delta_matrix[]", "deltas[]"}, v)
	},
	// Band starts must differ and the lowest is 0: a value ≤ 0 goes on the
	// lowest band, any other on the highest.
	"iv_margin_bands[].min_iv_pct": func(doc map[string]any, v float64) {
		bands := doc["iv_margin_bands"].([]any)
		lo, hi := 0, 0
		for i, b := range bands {
			x := toFloat(b.(map[string]any)["min_iv_pct"])
			if x < toFloat(bands[lo].(map[string]any)["min_iv_pct"]) {
				lo = i
			}
			if x > toFloat(bands[hi].(map[string]any)["min_iv_pct"]) {
				hi = i
			}
		}
		at := hi
		if v <= 0 {
			at = lo
		}
		bands[at].(map[string]any)["min_iv_pct"] = v
	},
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case float64:
		return x
	}
	return math.NaN()
}

// loadVariant loads file with the catalogue key set to v.
func loadVariant(t *testing.T, file string, tu config.Tunable, v any) error {
	t.Helper()
	doc := readYAMLDoc(t, file)
	if f, ok := rangeFixtures[tu.Key]; ok {
		f(doc, v.(float64))
	} else {
		if tu.Kind == config.KindInteger {
			if f, ok := v.(float64); ok {
				v = int(math.Round(f))
			}
		}
		setAll(doc, strings.Split(tu.Key, "."), v)
	}
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

func onStep(v, step float64) float64 { return math.Round(v/step) * step }

// Scenario: ranges match Validate — at Min and Max the shipped config loads,
// just outside it does not; every enum value loads, an unknown one does not.
func TestCatalogue_RangesMatchValidate(t *testing.T) {
	for _, file := range shippedConfigs {
		for _, tu := range config.Tunables() {
			fmtv := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
			if tu.Min != nil {
				if err := loadVariant(t, file, tu, *tu.Min); err != nil {
					t.Errorf("%s: %s at Min %s rejected: %v", file, tu.Key, fmtv(*tu.Min), err)
				}
				out := onStep(*tu.Min-tu.Step, tu.Step)
				if out == 0 && tu.Default != "" {
					out = -tu.Step // 0 means "absent": Load puts the default in
				}
				if err := loadVariant(t, file, tu, out); err == nil {
					t.Errorf("%s: %s at %s (under Min) accepted", file, tu.Key, fmtv(out))
				}
			}
			if tu.Max != nil {
				if err := loadVariant(t, file, tu, *tu.Max); err != nil {
					t.Errorf("%s: %s at Max %s rejected: %v", file, tu.Key, fmtv(*tu.Max), err)
				}
				out := onStep(*tu.Max+tu.Step, tu.Step)
				if err := loadVariant(t, file, tu, out); err == nil {
					t.Errorf("%s: %s at %s (over Max) accepted", file, tu.Key, fmtv(out))
				}
			}
			if tu.Kind == config.KindEnum {
				for _, v := range tu.Values {
					if err := loadVariant(t, file, tu, v); err != nil {
						t.Errorf("%s: %s = %s rejected: %v", file, tu.Key, v, err)
					}
				}
				if err := loadVariant(t, file, tu, "not-a-value"); err == nil {
					t.Errorf("%s: %s accepted an unknown value", file, tu.Key)
				}
			}
		}
	}
}

// The cross rules the catalogue names are the ones Validate enforces.
func TestCatalogue_CrossRules(t *testing.T) {
	cat := catalogueByKey(t)
	cases := []struct {
		key  string
		set  func(doc map[string]any)
		want bool // loads
	}{
		{"delta_drift_threshold", func(d map[string]any) { d["delta_drift_threshold"] = 0.12 }, true},  // 75 % of 0.16
		{"delta_drift_threshold", func(d map[string]any) { d["delta_drift_threshold"] = 0.13 }, false}, // over 75 %
		{"delta_exit_threshold", func(d map[string]any) { d["delta_exit_threshold"] = 0.17 }, true},
		{"delta_exit_threshold", func(d map[string]any) { d["delta_exit_threshold"] = 0.16 }, false}, // not above the entry delta
		{"rollout_dte", func(d map[string]any) { d["rollout_dte"] = 24 }, true},
		{"rollout_dte", func(d map[string]any) { d["rollout_dte"] = 25 }, false}, // BTC's 25-DTE slot
		{"iv_margin_bands[].min_iv_pct", func(d map[string]any) {
			d["iv_margin_bands"].([]any)[0].(map[string]any)["min_iv_pct"] = 30 // duplicate start
		}, false},
	}
	for _, c := range cases {
		if len(cat[c.key].CrossRules) == 0 {
			t.Errorf("%s: Validate has a cross rule the catalogue does not name", c.key)
		}
		doc := readYAMLDoc(t, "config_btc.yaml")
		c.set(doc)
		raw, _ := yaml.Marshal(doc)
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("DERIBIT_ENV", "testnet")
		_, err := loadWithDummyCreds(t, path)
		if (err == nil) != c.want {
			t.Errorf("%s: loads=%v, want %v (err %v)", c.key, err == nil, c.want, err)
		}
	}
}

// Scenario: core policies are marked.
func TestCatalogue_CorePoliciesMarked(t *testing.T) {
	cat := catalogueByKey(t)
	want := map[string]string{
		"delta_exit_threshold": config.PolicyExits, "stop_loss_multiplier": config.PolicyExits,
		"repair_cooldown_hours": config.PolicyRepair, "gamma_flip_buffer_sd": config.PolicyGEX,
		"gex_direction": config.PolicyGEX, "max_mm_pct": config.PolicyMargin,
		"max_leg_size_multiple": config.PolicySizing, "dte_delta_matrix[].deltas[]": config.PolicyEntry,
		"backtest.fill_model": config.PolicyNone,
	}
	for k, p := range want {
		if cat[k].Policy != p {
			t.Errorf("%s: policy %q, want %q", k, cat[k].Policy, p)
		}
	}
	doc := config.CatalogueMarkdown(config.Tunables())
	i := strings.Index(doc, "## `delta_exit_threshold`")
	if i < 0 {
		t.Fatal("docs: no section for delta_exit_threshold")
	}
	section := doc[i:]
	if j := strings.Index(section[3:], "\n## "); j >= 0 {
		section = section[:j+3]
	}
	if !strings.Contains(section, "**Policy:** exits — a change to its default needs the product owner") {
		t.Errorf("docs: delta_exit_threshold does not say a change needs the product owner:\n%s", section)
	}
}

// Scenario: docs stay in sync — docs/parameters.md is the catalogue rendered.
// Regenerate: go run ./cmd/paramdocs (or UPDATE_GOLDEN=1 go test ./tests -run
// TestCatalogue_DocsInSync, or go test ./tests -run TestCatalogue_DocsInSync -args -update-docs).
var updateDocs = flag.Bool("update-docs", false, "rewrite docs/parameters.md from the catalogue")

func TestCatalogue_DocsInSync(t *testing.T) {
	got := config.CatalogueMarkdown(config.Tunables())
	path := filepath.Join("..", "docs", "parameters.md")
	if os.Getenv("UPDATE_GOLDEN") != "" || *updateDocs {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Error("docs/parameters.md is out of date with config.Tunables(): run go run ./cmd/paramdocs")
	}
}

// A changed text changes the generated docs (so the golden test catches it).
func TestCatalogue_DocsFollowTheTexts(t *testing.T) {
	ts := config.Tunables()
	before := config.CatalogueMarkdown(ts)
	ts[0].Plain += " Changed."
	if config.CatalogueMarkdown(ts) == before {
		t.Error("a changed plain text left the generated docs unchanged")
	}
	for _, tu := range ts {
		if !strings.Contains(before, "`"+tu.Key+"`") {
			t.Errorf("docs: %s missing", tu.Key)
		}
	}
}
