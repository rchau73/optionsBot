package compare

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"optionsbot/internal/config"
)

// Diff is one setting that differs between the two configs, by its YAML key
// ("gamma_flip_buffer_sd", "dte_delta_matrix[1].deltas[0]",
// "iv_margin_bands[2].max_im_pct"). A key only one config has shows "—" on
// the other side.
type Diff struct {
	Key      string `json:"key"`
	Base     string `json:"base"`
	Proposed string `json:"proposed"`
}

func (d Diff) String() string { return d.Key + " " + d.Base + " → " + d.Proposed }

// DiffConfigs lists, key by key in key order, the YAML settings of base and
// proposed that differ, after defaults and fixed values (config.Load). Only
// the YAML keys are compared: the fixed values are the same for both by
// construction, and the platform settings (.env) are not strategy.
func DiffConfigs(base, proposed *config.Config) []Diff {
	a, b := settings(base), settings(proposed)
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []Diff
	for _, k := range keys {
		va, oka := a[k]
		vb, okb := b[k]
		if oka && okb && va == vb {
			continue
		}
		if !oka {
			va = "—"
		}
		if !okb {
			vb = "—"
		}
		out = append(out, Diff{Key: k, Base: va, Proposed: vb})
	}
	return out
}

// settings flattens cfg's YAML-tagged fields to key → value.
func settings(cfg *config.Config) map[string]string {
	fields := map[string]any{}
	v := reflect.ValueOf(*cfg)
	for i := 0; i < v.NumField(); i++ {
		name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue // a fixed value or a platform setting
		}
		fields[name] = v.Field(i).Interface()
	}
	// A YAML round trip turns nested structs into maps keyed by their YAML
	// names, so every leaf is named as in the file.
	raw, err := yaml.Marshal(fields)
	if err != nil {
		return map[string]string{"(settings)": err.Error()}
	}
	var tree any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		return map[string]string{"(settings)": err.Error()}
	}
	out := map[string]string{}
	flatten("", tree, out)
	return out
}

func flatten(key string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if key != "" {
				k = key + "." + k
			}
			flatten(k, x, out)
		}
	case []any:
		if len(t) == 0 {
			out[key] = "[]"
		}
		for i, x := range t {
			flatten(fmt.Sprintf("%s[%d]", key, i), x, out)
		}
	case nil:
		out[key] = "—"
	default:
		out[key] = fmt.Sprint(t)
	}
}
