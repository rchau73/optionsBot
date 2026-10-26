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
	IndexFile    = "frontend/content/help-index.json"
)

// Source kinds, the prefix of every passage's Source but the strategy's.
const (
	GlossaryPrefix = "glossary:"
	ExplainPrefix  = "explain:"
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
	Strategy  []byte
	Glossary  []byte
	Catalogue []CatalogueEntry
	Explain   []byte
}

// GlossaryEntry is the part of a glossary.json entry the index reads.
type GlossaryEntry struct {
	Short      string `json:"short"`
	Long       string `json:"long"`
	RiskIncome string `json:"risk_income"`
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
// order, then the glossary's terms and the catalogue's keys, each sorted.
// A glossary term's passage is its short, long and risk/income texts.
func Build(s Sources) (File, error) {
	var gloss map[string]GlossaryEntry
	if err := json.Unmarshal(s.Glossary, &gloss); err != nil {
		return File{}, fmt.Errorf("read %s: %w", GlossaryFile, err)
	}
	var chunks []Passage
	add := func(ps []Passage, blob string) {
		for _, p := range ps {
			p.Blob = blob
			chunks = append(chunks, p)
		}
	}
	add(Parse(StrategyFile, s.Strategy), BlobHash(s.Strategy))

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
