package tests

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"optionsbot/internal/config"
)

// yamlKeysOf lists every key a config file may set for t: the yaml tags of
// its fields that are not "-", nested ones as "parent.child" (structs and
// lists of structs).
func yamlKeysOf(t reflect.Type, prefix string) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		key := prefix + name
		keys = append(keys, key)
		ft := f.Type
		if ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			keys = append(keys, yamlKeysOf(ft, key+".")...)
		}
	}
	return keys
}

// TestGlossary_EveryConfigKeyHasAnEntry: the monitor's glossary (#188,
// frontend/content/glossary.json) explains every key a config file may set,
// not only those the shipped files use (the jest test checks those). A new
// tunable key needs its plain-words entry in the same PR.
func TestGlossary_EveryConfigKeyHasAnEntry(t *testing.T) {
	raw, err := os.ReadFile("../frontend/content/glossary.json")
	if err != nil {
		t.Fatal(err)
	}
	var glossary map[string]json.RawMessage
	if err := json.Unmarshal(raw, &glossary); err != nil {
		t.Fatal(err)
	}
	keys := yamlKeysOf(reflect.TypeOf(config.Config{}), "")
	for _, want := range []string{"rollout_dte", "iv_margin_bands.max_im_pct", "dte_delta_matrix.deltas", "backtest.fill_model"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("key walk missed %q: %v", want, keys)
		}
	}
	for _, k := range keys {
		if _, ok := glossary[k]; !ok {
			t.Errorf("config key %q has no entry in frontend/content/glossary.json", k)
		}
	}
}
