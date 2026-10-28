package tests

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"optionsbot/internal/api"
	"optionsbot/internal/helpkb"
	"optionsbot/internal/orders"
)

// The assistant's knowledge index (#312): frontend/content/help-index.json is
// OptionStrategy.md, the glossary and the plain-words catalogue as cited
// passages. Regenerate: go run ./cmd/helpindex (or UPDATE_GOLDEN=1 go test
// ./tests -run TestHelpIndex_CommittedEqualsFreshBuild, or go test ./tests
// -run 'TestHelp' -args -update-help, which also records the retrieval
// baseline).
var updateHelp = flag.Bool("update-help", false, "rewrite frontend/content/help-index.json and the retrieval baseline")

func updatingHelp() bool { return os.Getenv("UPDATE_GOLDEN") != "" || *updateHelp }

func helpSources(t *testing.T) helpkb.Sources {
	t.Helper()
	s, err := helpkb.LoadSources("..")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func buildHelpIndex(t *testing.T, s helpkb.Sources) helpkb.File {
	t.Helper()
	f, err := helpkb.Build(s)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Scenario: stale index fails — the committed index equals a fresh build.
func TestHelpIndex_CommittedEqualsFreshBuild(t *testing.T) {
	got, err := buildHelpIndex(t, helpSources(t)).Encode()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", helpkb.IndexFile)
	if updatingHelp() {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("helpindex: %s is stale (OptionStrategy.md, the glossary or the plain-words catalogue changed): run go run ./cmd/helpindex", helpkb.IndexFile)
	}
}

// Scenario: stale index fails — given a change to OptionStrategy.md §9, the
// fresh build differs from the committed index, in a §9 passage.
func TestHelpIndex_StaleIndexFails(t *testing.T) {
	s := helpSources(t)
	committed, err := os.ReadFile(filepath.Join("..", helpkb.IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	const rule = "- **Normal:** the confirmed volatility band and gamma regime are unchanged"
	if !bytes.Contains(s.Strategy, []byte(rule)) {
		t.Fatalf("OptionStrategy.md §9 no longer has %q: pick another line of §9 for this test", rule)
	}
	s.Strategy = bytes.Replace(s.Strategy, []byte(rule), []byte(rule+" (edited)"), 1)
	f := buildHelpIndex(t, s)
	got, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(committed, got) {
		t.Fatal("a change to §9 left the index unchanged: the stale check would not catch it")
	}
	var old helpkb.File
	if err := json.Unmarshal(committed, &old); err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, c := range f.Chunks {
		if strings.Contains(c.Text, "(edited)") {
			changed = strings.HasPrefix(c.Source, "OptionStrategy.md#9-")
		}
	}
	if !changed {
		t.Error("the edited text is not in a §9 passage")
	}
}

// githubAnchors computes the heading anchors of a markdown file the way the
// glossary's jest test does (frontend/test/glossarySources.js), independently
// of helpkb.
func githubAnchors(md string) map[string]bool {
	out := map[string]bool{}
	strip := regexp.MustCompile(`[^\p{L}\p{N} _-]`)
	for _, m := range regexp.MustCompile(`(?m)^#{1,6} (.+)$`).FindAllStringSubmatch(md, -1) {
		out[strings.ReplaceAll(strip.ReplaceAllString(strings.ToLower(strings.TrimSpace(m[1])), ""), " ", "-")] = true
	}
	return out
}

// Scenario: every chunk is citable — its source resolves to an existing
// heading anchor, glossary term or catalogue key; no chunk is empty, ids are
// unique, chunks keep to the size limit and carry their source's blob hash.
func TestHelpIndex_EveryChunkIsCitable(t *testing.T) {
	s := helpSources(t)
	f := buildHelpIndex(t, s)
	anchors := githubAnchors(string(s.Strategy))
	var gloss map[string]json.RawMessage
	if err := json.Unmarshal(s.Glossary, &gloss); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, e := range api.Catalogue() {
		keys[e.Key] = true
	}
	blobs := map[string]string{
		helpkb.StrategyFile:   helpkb.BlobHash(s.Strategy),
		helpkb.GlossaryPrefix: helpkb.BlobHash(s.Glossary),
		helpkb.ExplainPrefix:  helpkb.BlobHash(s.Explain),
		"parameters.md":       helpkb.BlobHash(s.Parameters),
		"monitor-guide.md":    helpkb.BlobHash(s.Guide),
	}
	docAnchors := map[string]map[string]bool{"parameters.md": {}, "monitor-guide.md": {}}
	for name, md := range map[string][]byte{"parameters.md": s.Parameters, "monitor-guide.md": s.Guide} {
		for _, a := range helpkb.Anchors(md) {
			docAnchors[name][a] = true
		}
	}
	ids := map[string]bool{}
	kinds := map[string]int{}
	for _, c := range f.Chunks {
		if ids[c.ID] {
			t.Errorf("duplicate id %s", c.ID)
		}
		ids[c.ID] = true
		if !strings.HasPrefix(c.ID, c.Source+"/") {
			t.Errorf("%s: id is not its source plus an ordinal (%s)", c.ID, c.Source)
		}
		if strings.TrimSpace(c.Text) == "" || strings.TrimSpace(c.Title) == "" {
			t.Errorf("%s: empty text or title", c.ID)
		}
		if n := utf8.RuneCountInString(c.Text); n > helpkb.MaxChunkChars && !strings.HasPrefix(c.Text, "|") {
			t.Errorf("%s: %d characters, over %d", c.ID, n, helpkb.MaxChunkChars)
		}
		var kind string
		var ok bool
		switch {
		case strings.HasPrefix(c.Source, helpkb.StrategyFile+"#"):
			kind, ok = helpkb.StrategyFile, anchors[c.Anchor()]
		case strings.HasPrefix(c.Source, "parameters.md#"):
			kind, ok = "parameters.md", docAnchors["parameters.md"][c.Anchor()]
		case strings.HasPrefix(c.Source, "monitor-guide.md#"):
			kind, ok = "monitor-guide.md", docAnchors["monitor-guide.md"][c.Anchor()]
		case strings.HasPrefix(c.Source, helpkb.GlossaryPrefix):
			_, ok = gloss[c.Anchor()]
			kind = helpkb.GlossaryPrefix
		case strings.HasPrefix(c.Source, helpkb.ExplainPrefix):
			kind, ok = helpkb.ExplainPrefix, keys[c.Anchor()]
		}
		if !ok {
			t.Errorf("%s: source %q resolves to no heading anchor, glossary term or catalogue key", c.ID, c.Source)
			continue
		}
		kinds[kind]++
		if c.Blob != blobs[kind] {
			t.Errorf("%s: blob %s, want %s", c.ID, c.Blob, blobs[kind])
		}
	}
	// Every heading, every glossary term and every catalogue key is in it.
	cited := map[string]bool{}
	for _, c := range f.Chunks {
		cited[c.Source] = true
	}
	for a := range anchors {
		if !cited[helpkb.StrategyFile+"#"+a] {
			t.Errorf("heading #%s has no passage", a)
		}
	}
	for term := range gloss {
		if !cited[helpkb.GlossaryPrefix+term] {
			t.Errorf("glossary term %q has no passage", term)
		}
	}
	for k := range keys {
		if !cited[helpkb.ExplainPrefix+k] {
			t.Errorf("catalogue key %q has no passage", k)
		}
	}
	if kinds[helpkb.StrategyFile] < 30 || kinds[helpkb.GlossaryPrefix] < 100 || kinds[helpkb.ExplainPrefix] < 50 {
		t.Errorf("too few passages per source: %v", kinds)
	}
}

// The glossary's no-return-promises check runs over every chunk; a promise
// word that only denies a promise is allowed by its exact phrase.
func TestHelpIndex_NoReturnPromises(t *testing.T) {
	f := buildHelpIndex(t, helpSources(t))
	if got := helpkb.Promises(f.Chunks); len(got) > 0 {
		t.Errorf("promise words in the help index: %v", got)
	}
	bad := []helpkb.Passage{
		{ID: "x/1", Title: "t", Text: "A guaranteed return, risk-free."},
		{ID: "x/2", Title: "t", Text: "Stop-loss levels are triggers, not guaranteed prices. Selling is risk free."},
	}
	got := helpkb.Promises(bad)
	want := []string{"x/1: guarantee", "x/1: risk-free", "x/2: risk free"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("Promises = %v, want %v", got, want)
	}
}

func orderEvent(event, data string) orders.RecentEvent {
	return orders.RecentEvent{Event: event, Data: json.RawMessage(data)}
}

// The catalogue the index cites is the API's own sentences: none generic,
// each one what ExplainEvent serves for its key.
func TestHelpIndex_CatalogueIsTheAPIsWording(t *testing.T) {
	cat := api.Catalogue()
	byKey := map[string]string{}
	for _, e := range cat {
		if strings.HasPrefix(e.Text, api.GenericPlain) || strings.TrimSpace(e.Text) == "" {
			t.Errorf("%s: %q is not a catalogue sentence", e.Key, e.Text)
		}
		if _, dup := byKey[e.Key]; dup {
			t.Errorf("duplicate catalogue key %s", e.Key)
		}
		byKey[e.Key] = e.Text
	}
	for key, ev := range map[string]string{
		"skipped:gex_wait":  `{"skip_reason":"gex_wait: no snapshot"}`,
		"risk_limit:frozen": `{"change":"frozen"}`,
		"closed:roi_target": `{"close_reason":"roi_target"}`,
	} {
		event, _, _ := strings.Cut(key, ":")
		plain, unknown := api.ExplainEvent(orderEvent(event, ev))
		if unknown != "" || plain != byKey[key] {
			t.Errorf("%s: catalogue %q, API %q (unknown %q)", key, byKey[key], plain, unknown)
		}
	}
	// `printf 'hello\n' | git hash-object --stdin`
	if helpkb.BlobHash([]byte("hello\n")) != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Error("BlobHash is not git's blob hash")
	}
}
