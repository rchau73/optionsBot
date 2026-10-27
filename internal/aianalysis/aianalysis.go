// Package aianalysis is the contract and the grounding checker for the AI
// analysis of a current-vs-proposed comparison (#257, docs/ai-analysis.md).
// It is pure and offline: nothing here calls a model, serves an endpoint or
// touches trading. Check takes the comparison's report.json (internal/compare)
// and a text and lists everything that breaks the contract: a number not in
// the run, advice or return-promise wording, a missing AI label, and a thin
// result presented without saying so.
//
// Nothing is shown to a user before legal question L10 is answered; until
// then only the comparison's own numbers are shown.
package aianalysis

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Model is the model the prompt is written for. No call is made in this story.
const Model = "claude-opus-5-5"

// Prompt is the instruction a later story sends with the comparison's JSON.
//
//go:embed prompt.txt
var Prompt string

// Label must appear in every analysis (L12: AI-generated, labelled).
const Label = "AI-generated analysis of backtested and simulated results; not advice; past and simulated results do not predict future results"

// MinTrades is the fewest trades a tool may show before the analysis must say
// its result is uncertain.
const MinTrades = 10

// BannedPhrases are advice and return-promise wording, matched
// case-insensitively. A phrase is banned even when negated ("no guarantee"):
// the analysis has other ways to say it.
var BannedPhrases = []string{
	"guarantee", "will earn", "will make", "will return", "will outperform", "will profit",
	"you should", "you must", "we recommend", "i recommend", "i suggest", "go ahead and",
	"apply this change", "apply the change", "apply the proposal", "safe to apply", "safe to use",
	"risk-free", "risk free", "expected profit", "expected return", "sure profit", "can't lose",
}

// uncertainWords say a result is thin. A caveat is one sentence naming the
// tool with one of them.
var uncertainWords = []string{"can't tell", "cannot tell", "can not tell", "uncertain", "too few", "thin", "not enough", "fragile", "unreliable"}

// Violation kinds.
const (
	KindNumber   = "number"
	KindLabel    = "label"
	KindBanned   = "banned_phrase"
	KindCaveat   = "missing_caveat"
	KindBadInput = "bad_input"
)

// Violation is one broken rule. Text is the number, phrase or tool; Sentence
// is the sentence it was found in (empty for a missing label or caveat).
type Violation struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Sentence string `json:"sentence,omitempty"`
}

func (v Violation) String() string {
	if v.Sentence == "" {
		return fmt.Sprintf("%s: %s", v.Kind, v.Text)
	}
	return fmt.Sprintf("%s: %q in %q", v.Kind, v.Text, v.Sentence)
}

// Check returns every violation of analysis against input (the comparison's
// report.json, optionally with "coverage": {"missing_days": [...]}). No
// violations = the analysis meets the contract.
func Check(input []byte, analysis string) []Violation {
	in, err := readInput(input)
	if err != nil {
		return []Violation{{Kind: KindBadInput, Text: err.Error()}}
	}
	var out []Violation
	if !strings.Contains(norm(analysis), norm(Label)) {
		out = append(out, Violation{Kind: KindLabel, Text: Label})
	}
	text := strings.ReplaceAll(analysis, Label, "") // the label has no figures of its own
	sents := sentences(text)
	for _, s := range sents {
		low := strings.ToLower(s)
		for _, p := range BannedPhrases {
			if strings.Contains(low, p) {
				out = append(out, Violation{Kind: KindBanned, Text: p, Sentence: s})
			}
		}
		for _, n := range numbersIn(s, in.dates) {
			if !in.has(n) {
				out = append(out, Violation{Kind: KindNumber, Text: n.raw, Sentence: s})
			}
		}
	}
	return append(out, caveats(in, sents)...)
}

type tool struct {
	name           string
	ran            bool
	base, proposed int // trades
}

type input struct {
	values  []float64
	dates   map[string]bool
	tools   []tool
	missing []string
}

func (in *input) has(n number) bool {
	for _, v := range in.values {
		if math.Abs(round(math.Abs(v), n.decimals)-n.value) < 1e-9 {
			return true
		}
	}
	return n.value == MinTrades && n.decimals == 0 // the contract's own threshold
}

func readInput(raw []byte) (*input, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("input is not a JSON object: %w", err)
	}
	in := &input{dates: map[string]bool{}}
	in.walk(doc)
	for _, name := range []string{"backtest", "replay"} {
		sec, _ := doc[name].(map[string]any)
		if sec == nil {
			continue
		}
		in.tools = append(in.tools, tool{name: name, ran: sec["ran"] == true, base: trades(sec["base"]), proposed: trades(sec["proposed"])})
	}
	if cov, ok := doc["coverage"].(map[string]any); ok {
		days, _ := cov["missing_days"].([]any)
		for _, d := range days {
			if s, ok := d.(string); ok {
				in.missing = append(in.missing, s)
			}
		}
	}
	return in, nil
}

func trades(v any) int {
	run, _ := v.(map[string]any)
	n, _ := run["trades"].(json.Number)
	i, _ := n.Int64()
	return int(i)
}

// walk collects every number of the document, and the numbers and dates
// written inside its strings (a diff's "0.16", a base file's name).
func (in *input) walk(v any) {
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			in.walk(e)
		}
	case []any:
		for _, e := range x {
			in.walk(e)
		}
	case json.Number:
		if f, err := x.Float64(); err == nil {
			in.values = append(in.values, f)
		}
	case string:
		for _, d := range dateRe.FindAllString(x, -1) {
			in.dates[d] = true
		}
		for _, n := range numbersIn(x, nil) {
			in.values = append(in.values, n.value)
		}
	}
}

// caveats asks, for each tool that ran on fewer than MinTrades trades in
// either config, and for missing days, one sentence that says so.
func caveats(in *input, sents []string) []Violation {
	said := func(word string) bool {
		for _, s := range sents {
			low := strings.ToLower(s)
			if !strings.Contains(low, word) {
				continue
			}
			for _, u := range uncertainWords {
				if strings.Contains(low, u) {
					return true
				}
			}
		}
		return false
	}
	var out []Violation
	for _, t := range in.tools {
		if t.ran && (t.base < MinTrades || t.proposed < MinTrades) && !said(t.name) {
			out = append(out, Violation{Kind: KindCaveat, Text: fmt.Sprintf("%s ran on %d and %d trades (under %d) and the analysis does not say the result is uncertain", t.name, t.base, t.proposed, MinTrades)})
		}
	}
	if len(in.missing) > 0 && !said("missing") {
		out = append(out, Violation{Kind: KindCaveat, Text: fmt.Sprintf("the run has %d missing days and the analysis does not say so", len(in.missing))})
	}
	return out
}

type number struct {
	raw      string
	value    float64
	decimals int
}

var (
	numberRe = regexp.MustCompile(`\d{1,3}(?:,\d{3})+(?:\.\d+)?|\d+(?:\.\d+)?`)
	dateRe   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	weekRe   = regexp.MustCompile(`\d{4}-W\d{2}`)
	sentRe   = regexp.MustCompile(`[.!?](?:\s+|$)|\n+`)
)

// numbersIn lists the figures of s. A digit run touching a letter or an
// underscore is a name (SMA9, p90, 27NOV), not a figure. A date is skipped
// when known is nil or the date is in known; an unknown date stays, as digits
// that match nothing.
func numbersIn(s string, known map[string]bool) []number {
	for _, d := range dateRe.FindAllString(s, -1) {
		if known == nil || known[d] {
			s = strings.Replace(s, d, strings.Repeat(" ", len(d)), 1)
		}
	}
	s = weekRe.ReplaceAllStringFunc(s, func(w string) string { return strings.Repeat(" ", len(w)) })
	var out []number
	for _, loc := range numberRe.FindAllStringIndex(s, -1) {
		if loc[0] > 0 && isWordByte(s[loc[0]-1]) {
			continue
		}
		if loc[1] < len(s) && isWordByte(s[loc[1]]) {
			continue
		}
		raw := s[loc[0]:loc[1]]
		clean := strings.ReplaceAll(raw, ",", "")
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			continue
		}
		dec := 0
		if i := strings.IndexByte(clean, '.'); i >= 0 {
			dec = len(clean) - i - 1
		}
		out = append(out, number{raw: raw, value: f, decimals: dec})
	}
	return out
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}

func sentences(s string) []string {
	var out []string
	last := 0
	for _, loc := range sentRe.FindAllStringIndex(s, -1) {
		if t := strings.TrimSpace(s[last:loc[1]]); t != "" {
			out = append(out, t)
		}
		last = loc[1]
	}
	if t := strings.TrimSpace(s[last:]); t != "" {
		out = append(out, t)
	}
	return out
}

func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
