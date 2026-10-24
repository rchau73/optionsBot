package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Effective returns the strategy settings cfg trades with, by name: every
// YAML key (nested ones dotted, "backtest.fill_model") and every value fixed
// in code (fixed.go, named in snake_case, "order_fill_timeout_sec"). Nothing
// from .env is in it — no credential, rate limit, retry, breaker, heartbeat,
// address, token or environment — because a field is taken only when it has
// a YAML key or ApplyFixed sets it, and .env fields have neither (#256).
// Values are JSON-shaped (numbers float64, lists []any), so a map read back
// from the journal compares equal to a fresh one.
func Effective(cfg *Config) map[string]any {
	var fixed Config
	fixed.ApplyFixed()
	fv := reflect.ValueOf(fixed)
	v := reflect.ValueOf(*cfg)
	t := v.Type()
	out := map[string]any{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		key := yamlName(f)
		if key == "" {
			if fv.Field(i).IsZero() {
				continue // neither a YAML key nor a fixed value: .env or legacy
			}
			key = fixedName(f.Name)
		}
		flatten(out, key, jsonShaped(v.Field(i).Interface()))
	}
	return out
}

// Fingerprint is the first 12 hex characters of the SHA-256 of eff as
// canonical JSON (keys sorted), so equal settings give equal fingerprints.
func Fingerprint(eff map[string]any) string {
	b, err := json.Marshal(eff)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// Change is one setting whose value differs between two Effective maps. From
// is nil for a key that did not exist before, To for one that is gone.
type Change struct {
	Key  string `json:"key"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

// Diff lists the settings that differ from prev to cur, by key.
func Diff(prev, cur map[string]any) []Change {
	keys := map[string]bool{}
	for k := range prev {
		keys[k] = true
	}
	for k := range cur {
		keys[k] = true
	}
	out := []Change{}
	for k := range keys {
		if canonical(prev[k]) != canonical(cur[k]) {
			out = append(out, Change{Key: k, From: prev[k], To: cur[k]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func canonical(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// yamlName is the field's YAML key, empty for yaml:"-" or no tag.
func yamlName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
	if name == "-" {
		return ""
	}
	return name
}

// fixedNames name the fixed fields whose snake_case differs from their
// fixedKeys entry.
var fixedNames = map[string]string{"StopSpreadMaxWaitMin": "stop_spread_max_wait_minutes"}

// fixedName turns a Go field name into snake_case: "GEXShedConfirmSnapshots"
// → "gex_shed_confirm_snapshots", "LiquidityCapOIPct" → "liquidity_cap_oi_pct".
func fixedName(goName string) string {
	if n, ok := fixedNames[goName]; ok {
		return n
	}
	r := []rune(goName)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) && i > 0 {
			prevLower := !unicode.IsUpper(r[i-1])
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			if prevLower || nextLower {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// jsonShaped turns v into what JSON decoding would give, using YAML key names
// for nested structs (dte_delta_matrix entries, iv_margin_bands, backtest).
func jsonShaped(v any) any {
	y, err := yaml.Marshal(v)
	if err != nil {
		return nil
	}
	var generic any
	if err := yaml.Unmarshal(y, &generic); err != nil {
		return nil
	}
	j, err := json.Marshal(generic)
	if err != nil {
		return nil
	}
	var out any
	if err := json.Unmarshal(j, &out); err != nil {
		return nil
	}
	return out
}

// flatten stores v under key, nested maps as dotted keys; lists stay whole.
func flatten(out map[string]any, key string, v any) {
	m, ok := v.(map[string]any)
	if !ok {
		out[key] = v
		return
	}
	for k, sub := range m {
		flatten(out, key+"."+k, sub)
	}
}
