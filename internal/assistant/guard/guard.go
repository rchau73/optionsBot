// Package guard checks an assistant answer before it could be shown (#260,
// E10): it must cite the help, its numbers must come from what it cites, and
// it must not advise or forecast. Pure: no model, no network. The monitor's
// assistant (#191) runs its JavaScript twin, frontend/lib/assistant/guard.js,
// whose tests keep both equal (phrases, cases, golden set): change both. The
// answer contract it enforces is docs/product/assistant/contract.md.
package guard

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"optionsbot/internal/helpkb"
)

// Reason codes of a rejected answer, in the order Check reports them.
const (
	ReasonNoCitation       = "no_citation"       // no citation, and not a canonical reply
	ReasonUnknownCitation  = "unknown_citation"  // a citation that resolves to no passage
	ReasonUngroundedNumber = "ungrounded_number" // a number in none of the cited passages
	ReasonAdvice           = "advice"            // tells the user what to do
	ReasonForecast         = "forecast"          // says what prices or returns will be
)

// Answer is an assistant answer: its text and the passages it cites, each a
// passage id ("OptionStrategy.md#8-risk-controls/1") or a source, which cites
// every passage of it ("OptionStrategy.md#8-risk-controls", "glossary:delta").
type Answer struct {
	Text      string
	Citations []string
}

// Verdict is Check's result: OK, or the reason codes (each once) with one
// detail per finding, e.g. "unknown_citation: glossary:nope".
type Verdict struct {
	OK      bool
	Reasons []string
	Details []string
}

func (v *Verdict) add(reason, detail string) {
	v.Details = append(v.Details, reason+": "+detail)
	for _, r := range v.Reasons {
		if r == reason {
			return
		}
	}
	v.Reasons = append(v.Reasons, reason)
}

// Check applies every rule to the answer against the passages it may cite
// (the help index; a caller adds the user's journal facts as passages).
func Check(a Answer, sources []helpkb.Passage) Verdict {
	var v Verdict
	text := strings.TrimSpace(a.Text)

	cited := citedPassages(a.Citations, sources, &v)
	if len(a.Citations) == 0 && !IsCanonical(text) {
		v.add(ReasonNoCitation, "the answer cites no passage")
	}

	grounded := map[string]bool{}
	var values []float64
	for _, p := range cited {
		for _, n := range Numbers(p.Title + "\n" + p.Text) {
			grounded[n.Key] = true
			if n.IsValue {
				values = append(values, n.Value)
			}
		}
	}
	for _, n := range Numbers(text) {
		if !grounded[n.Key] && !(n.IsValue && matchesValue(n.Value, values)) {
			v.add(ReasonUngroundedNumber, n.Text)
		}
	}

	unadvised := text
	for _, re := range adviceNegations {
		unadvised = re.ReplaceAllString(unadvised, " ")
	}
	for _, re := range advicePhrases {
		if m := re.FindString(unadvised); m != "" {
			v.add(ReasonAdvice, strings.TrimSpace(m))
		}
	}
	unnegated := text
	for _, re := range negations {
		unnegated = re.ReplaceAllString(unnegated, " ")
	}
	for _, re := range forecastPhrases {
		if m := re.FindString(unnegated); m != "" {
			v.add(ReasonForecast, strings.TrimSpace(m))
		}
	}

	v.OK = len(v.Reasons) == 0
	return v
}

// Fallback is the reply shown instead of a rejected answer: whatever the
// reason, the canonical safe reply, which points to the help.
func Fallback(v Verdict) string {
	return FallbackReply
}

// IsCanonical reports whether the text is exactly one of the canonical
// replies (spacing aside), the only answers that need no citation.
func IsCanonical(text string) bool {
	t := strings.Join(strings.Fields(text), " ")
	// "I don't know" for the facts and the refusal for the advice (#427).
	return t == DontKnow || t == Refusal || t == FallbackReply || t == DontKnow+" "+Refusal
}

// citedPassages resolves each citation by passage id or by source; one that
// resolves to nothing is reported.
func citedPassages(citations []string, sources []helpkb.Passage, v *Verdict) []helpkb.Passage {
	var out []helpkb.Passage
	for _, c := range citations {
		found := false
		for _, p := range sources {
			if p.ID == c || p.Source == c {
				out = append(out, p)
				found = true
			}
		}
		if !found {
			v.add(ReasonUnknownCitation, c)
		}
	}
	return out
}

// Number is a number found in a text: a date (Key "date:2026-10-12") or a
// value (Key "n:0.3", IsValue), with the text it was read from.
type Number struct {
	Text    string
	Key     string
	Value   float64
	IsValue bool
}

// numberRe finds dates and numbers not glued to a word before them (SMA9,
// rollout_19dte's "19" is glued to "_"): "1,454", "0.30", "72,000", "88k" → 88.
var numberRe = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_.,])(\d{4}-\d{2}-\d{2}|\d+(?:[.,]\d+)*)`)

var thousandsRe = regexp.MustCompile(`^\d{1,3}(,\d{3})+(\.\d+)?$`)

// Numbers lists the numbers of a text.
func Numbers(text string) []Number {
	var out []Number
	for _, m := range numberRe.FindAllStringSubmatch(text, -1) {
		s := m[1]
		if len(s) == 10 && s[4] == '-' && s[7] == '-' {
			out = append(out, Number{Text: s, Key: "date:" + s})
			continue
		}
		plain := s
		if thousandsRe.MatchString(s) {
			plain = strings.ReplaceAll(s, ",", "")
		} else {
			plain = strings.ReplaceAll(s, ",", ".")
		}
		f, err := strconv.ParseFloat(plain, 64)
		if err != nil { // "1.2.3": a version or a list number, kept as text
			out = append(out, Number{Text: s, Key: "text:" + s})
			continue
		}
		out = append(out, Number{Text: s, Key: "n:" + strconv.FormatFloat(f, 'g', -1, 64), Value: f, IsValue: true})
	}
	return out
}

// matchesValue: the same value, or the same as a fraction and a percentage
// (0.30 and 30 %).
func matchesValue(x float64, values []float64) bool {
	for _, v := range values {
		if same(x, v) || same(x, v*100) || same(x*100, v) {
			return true
		}
	}
	return false
}

func same(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}
