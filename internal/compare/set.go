package compare

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// A proposed config as "the base with these values" (-set, #641): the lab's
// run requests carry key/value pairs, not a file, so cmd/compare writes the
// proposed file itself — a temporary copy of the base with only those keys
// changed, removed when the run ends. Nothing here touches a shipped config.

// Setting is one proposed value: a YAML key as in the Differences
// ("gamma_flip_buffer_sd", "dte_delta_matrix[0].dte"; "[]" sets every element
// of a list, as the catalogue names a list key) and its value.
type Setting struct {
	Key   string
	Value any
}

// SettingKeyRe is what a setting key may hold: lower-case YAML names, dots
// and list indexes ("[]" = every element).
var SettingKeyRe = regexp.MustCompile(`^[a-z0-9_]+(\[\d*\])*(\.[a-z0-9_]+(\[\d*\])*)*$`)

// listIndexRe is one list index of a key: "[2]", or "[]" for every element.
var listIndexRe = regexp.MustCompile(`\[\d*\]`)

// ParseSetting reads "key=value": the value is read as YAML, so 1.2 is a
// number, true a bool and bias (or "bias") a text.
func ParseSetting(s string) (Setting, error) {
	key, raw, ok := strings.Cut(s, "=")
	if !ok || !SettingKeyRe.MatchString(key) {
		return Setting{}, fmt.Errorf("set %q: want key=value, the key as in the Differences (e.g. gamma_flip_buffer_sd=1.2)", s)
	}
	var v any
	if err := yaml.Unmarshal([]byte(raw), &v); err != nil {
		return Setting{}, fmt.Errorf("set %q: value: %w", s, err)
	}
	switch v.(type) {
	case nil, map[string]any, []any:
		return Setting{}, fmt.Errorf("set %q: the value must be one number, true/false or a word", s)
	}
	return Setting{Key: key, Value: v}, nil
}

// SettingsLabel names a proposed config built by -set, in the report's
// "proposed" field: "config_btc.yaml with a=1, b=2", keys in order.
func SettingsLabel(base string, sets []Setting) string {
	parts := make([]string, len(sets))
	for i, s := range sets {
		parts[i] = fmt.Sprintf("%s=%v", s.Key, s.Value)
	}
	sort.Strings(parts)
	return base + " with " + strings.Join(parts, ", ")
}

// WriteWith writes a temporary copy of the config file at path with each
// setting applied and returns its name; the caller removes it. A list
// element the file does not hold is an error; a missing map key is added.
func WriteWith(path string, sets []Setting) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	for _, s := range sets {
		if doc, err = setPath(doc, s.Key, s.Value); err != nil {
			return "", err
		}
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "proposed-*.yaml")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(out); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

var segRe = regexp.MustCompile(`^([a-z0-9_]+)((?:\[\d*\])*)$`)

// everyElement is the index "[]" stands for in setIndexed.
const everyElement = -1

// setPath sets key ("a.b", "list[1].deltas[0]", "list[].dte") in a decoded
// YAML document. A missing map key is added; a missing list element is an
// error; "[]" is every element of the list.
func setPath(doc any, key string, value any) (any, error) {
	head, rest, nested := strings.Cut(key, ".")
	m := segRe.FindStringSubmatch(head)
	if m == nil {
		return nil, fmt.Errorf("key %q: cannot read %q", key, head)
	}
	obj, ok := doc.(map[string]any)
	if doc == nil {
		obj, ok = map[string]any{}, true
	}
	if !ok {
		return nil, fmt.Errorf("key %q: %q is not a section", key, head)
	}
	var idx []int
	for _, s := range listIndexRe.FindAllString(m[2], -1) {
		n, err := strconv.Atoi(strings.Trim(s, "[]"))
		if err != nil {
			n = everyElement
		}
		idx = append(idx, n)
	}
	set := func(cur any) (any, error) {
		if nested {
			return setPath(cur, rest, value)
		}
		return value, nil
	}
	child, err := setIndexed(obj[m[1]], idx, set, key)
	if err != nil {
		return nil, err
	}
	obj[m[1]] = child
	return obj, nil
}

func setIndexed(cur any, idx []int, set func(any) (any, error), key string) (any, error) {
	if len(idx) == 0 {
		return set(cur)
	}
	list, ok := cur.([]any)
	if !ok || len(list) == 0 || idx[0] >= len(list) {
		return nil, fmt.Errorf("key %q: no element %d in the proposed file", key, max(idx[0], 0))
	}
	at := []int{idx[0]}
	if idx[0] == everyElement {
		at = at[:0]
		for i := range list {
			at = append(at, i)
		}
	}
	for _, i := range at {
		v, err := setIndexed(list[i], idx[1:], set, key)
		if err != nil {
			return nil, err
		}
		list[i] = v
	}
	return list, nil
}
