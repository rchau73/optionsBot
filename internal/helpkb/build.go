package helpkb

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The files the index is built from and the file it is written to, relative
// to the repository root.
const (
	StrategyFile = "OptionStrategy.md"
	GlossaryFile = "frontend/content/glossary.json"
	ExplainFile  = "internal/api/explain.go"
	// ParametersFile and GuideFile are cited by their base names
	// ("parameters.md#<anchor>", "monitor-guide.md#<anchor>").
	ParametersFile = "docs/parameters.md"
	GuideFile      = "frontend/content/monitor-guide.md"
	// TipsFile is the settings' plain-words tips (#492, #588); the monitor
	// serves its copy, frontend/content/tips.json.
	TipsFile  = "docs/product/education/tips.json"
	IndexFile = "frontend/content/help-index.json"
)

// Source kinds, the prefix of every passage's Source but the strategy's.
const (
	GlossaryPrefix = "glossary:"
	ExplainPrefix  = "explain:"
	// TipPrefix cites a setting's tip by its key ("tip:repair_cooldown_hours", #621).
	TipPrefix = "tip:"
)

// IndexVersion is the format of help-index.json; bump it when a field changes.
const IndexVersion = 1

// CatalogueEntry is one sentence of the plain-words catalogue (api.Catalogue).
type CatalogueEntry struct {
	Key  string
	Text string
}

// Sources are the raw inputs of the index: the two files' bytes, the
// catalogue's sentences and the bytes of the file they are written in.
type Sources struct {
	Strategy []byte
	// Parameters is docs/parameters.md, the settings reference; Guide is the
	// user-facing Monitor guide, one section per page (#418).
	Parameters []byte
	Guide      []byte
	Glossary   []byte
	// Tips is TipsFile: one plain-words tip per tunable setting.
	Tips      []byte
	Catalogue []CatalogueEntry
	Explain   []byte
}

// GlossaryEntry is the part of a glossary.json entry the index reads.
type GlossaryEntry struct {
	Short      string `json:"short"`
	Long       string `json:"long"`
	RiskIncome string `json:"risk_income"`
}

// TipEntry is the part of a tips.json entry the index reads: what the setting
// means, what raising it does to risk and to income, and the note on a
// core-policy setting.
type TipEntry struct {
	Meaning     string `json:"meaning"`
	RaiseRisk   string `json:"raise_risk"`
	RaiseIncome string `json:"raise_income"`
	PolicyNote  string `json:"policy_note"`
}

// tipUnitWords are a key's last word when it only names the unit
// ("repair_cooldown_hours" → "repair cooldown").
var tipUnitWords = map[string]bool{"hours": true, "pct": true, "sd": true}

// TipName is a setting's name in words, from its key: "[]" dropped, "." and
// "_" read as spaces, a trailing unit word left out ("repair_cooldown_hours"
// → "repair cooldown", "iv_margin_bands[].max_im_pct" → "iv margin bands max
// im"). It is the tip passage's title, so a question naming the setting
// matches the title exactly; the monitor shows it as "Tip: <name>".
func TipName(key string) string {
	key = strings.ReplaceAll(key, "[]", "")
	words := strings.FieldsFunc(key, func(r rune) bool { return r == '.' || r == '_' })
	if n := len(words); n > 1 && tipUnitWords[words[n-1]] {
		words = words[:n-1]
	}
	return strings.Join(words, " ")
}

// File is help-index.json.
type File struct {
	Version int       `json:"version"`
	Chunks  []Passage `json:"chunks"`
}

// BlobHash is git's blob hash of a file's bytes (what `git hash-object`
// prints), so it needs no git call.
func BlobHash(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// Build makes the index: OptionStrategy.md's passages in the document's
// order, then the glossary's terms, the settings' tips and the catalogue's
// keys, each sorted. A glossary term's passage is its short, long and
// risk/income texts; a tip's is its own words, unchanged: meaning, risk,
// income and policy note (#621: the assistant cites what the tip says).
func Build(s Sources) (File, error) {
	var gloss map[string]GlossaryEntry
	if err := json.Unmarshal(s.Glossary, &gloss); err != nil {
		return File{}, fmt.Errorf("read %s: %w", GlossaryFile, err)
	}
	var tips map[string]TipEntry
	if err := json.Unmarshal(s.Tips, &tips); err != nil {
		return File{}, fmt.Errorf("read %s: %w", TipsFile, err)
	}
	var chunks []Passage
	add := func(ps []Passage, blob string) {
		for _, p := range ps {
			p.Blob = blob
			chunks = append(chunks, p)
		}
	}
	add(Parse(StrategyFile, s.Strategy), BlobHash(s.Strategy))
	add(Parse("parameters.md", s.Parameters), BlobHash(s.Parameters))
	add(Parse("monitor-guide.md", s.Guide), BlobHash(s.Guide))

	terms := make([]string, 0, len(gloss))
	for t := range gloss {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	glossBlob := BlobHash(s.Glossary)
	for _, t := range terms {
		e := gloss[t]
		var paras []string
		for _, p := range []string{e.Short, e.Long, e.RiskIncome} {
			if p = strings.TrimSpace(p); p != "" {
				paras = append(paras, p)
			}
		}
		add(TextPassages(GlossaryPrefix+t, t, strings.Join(paras, "\n\n")), glossBlob)
	}

	keys := make([]string, 0, len(tips))
	for k := range tips {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tipsBlob := BlobHash(s.Tips)
	for _, k := range keys {
		e := tips[k]
		var paras []string
		for _, p := range []string{e.Meaning, e.RaiseRisk, e.RaiseIncome, e.PolicyNote} {
			if p = strings.TrimSpace(p); p != "" {
				paras = append(paras, p)
			}
		}
		add(TextPassages(TipPrefix+k, TipName(k), strings.Join(paras, "\n\n")), tipsBlob)
	}

	cat := append([]CatalogueEntry(nil), s.Catalogue...)
	sort.Slice(cat, func(i, j int) bool { return cat[i].Key < cat[j].Key })
	explainBlob := BlobHash(s.Explain)
	for _, e := range cat {
		add(TextPassages(ExplainPrefix+e.Key, e.Key, e.Text), explainBlob)
	}
	return File{Version: IndexVersion, Chunks: chunks}, nil
}

// Encode is the file's bytes as committed: indented JSON, "<", ">" and "&"
// kept as they are, one trailing newline.
func (f File) Encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
