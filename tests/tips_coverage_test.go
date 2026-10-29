package tests

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"optionsbot/internal/config"
)

// The tip coverage test (#492, epic E11): no tunable setting or glossary term
// reaches the monitor without a plain-words tip. The sources are
// config.Tunables() and frontend/content/glossary.json; the tips of the
// settings live in docs/product/education/tips.json (meaning, unit, effect of
// raising it on risk and on income). A glossary entry is its own tip (short +
// risk_income). The rules mirror the jest plain-words checks (#339).

const maxTipSentenceWords = 25

// tipJargon: words a newcomer may not know; each needs a glossary entry.
var tipJargon = []string{"delta", "gamma", "theta", "vega", "strangle", "iv", "dvol", "gex", "margin", "strike"}

// promiseWords: a tip never guarantees or implies a return.
var promiseWords = regexp.MustCompile(`(?i)\b(guarantee[sd]?|promise[sd]?|risk-free|riskless|no risk|sure profit|expected (profit|return|yield|gain)s?|will (earn|make|profit|yield|return)|always (earn|profit|win|makes?)|assured|certain profit)\b`)

var tipWord = regexp.MustCompile(`[A-Za-z][A-Za-z&'-]*`)
var tipSentenceEnd = regexp.MustCompile(`[.!?;]\s+`)

type tip struct {
	Meaning    string `json:"meaning"`
	Unit       string `json:"unit"`
	RaiseRisk  string `json:"raise_risk"`
	RaiseInc   string `json:"raise_income"`
	PolicyNote string `json:"policy_note"`
	Video      string `json:"video"`
}

type glossaryTip struct {
	Short      string   `json:"short"`
	RiskIncome string   `json:"risk_income"`
	Labels     []string `json:"labels"`
}

func loadTips(t *testing.T) map[string]tip {
	t.Helper()
	var tips map[string]tip
	if err := json.Unmarshal(readRepoFile(t, "docs/product/education/tips.json"), &tips); err != nil {
		t.Fatal(err)
	}
	return tips
}

func loadGlossaryTips(t *testing.T) map[string]glossaryTip {
	t.Helper()
	var g map[string]glossaryTip
	if err := json.Unmarshal(readRepoFile(t, "frontend/content/glossary.json"), &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func tipNorm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// glossaryIndex: every glossary term and label, normalised.
func glossaryIndex(g map[string]glossaryTip) map[string]bool {
	idx := map[string]bool{}
	for term, e := range g {
		idx[tipNorm(term)] = true
		for _, l := range e.Labels {
			idx[tipNorm(l)] = true
		}
	}
	return idx
}

// longTipSentences returns the sentences of text over maxTipSentenceWords words.
func longTipSentences(text string) []string {
	var out []string
	rest := text
	for rest != "" {
		loc := tipSentenceEnd.FindStringIndex(rest)
		sentence := rest
		if loc == nil {
			rest = ""
		} else {
			sentence, rest = rest[:loc[0]+1], rest[loc[1]:]
		}
		if len(strings.Fields(sentence)) > maxTipSentenceWords {
			out = append(out, sentence)
		}
	}
	return out
}

// undefinedTipJargon returns the jargon words of text with no glossary entry,
// except words of own (the key or term being explained).
func undefinedTipJargon(text, own string, defined map[string]bool) []string {
	ownWords := map[string]bool{}
	for _, w := range tipWord.FindAllString(strings.ToLower(own), -1) {
		ownWords[w] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, w := range tipWord.FindAllString(strings.ToLower(text), -1) {
		isJargon := false
		for _, j := range tipJargon {
			isJargon = isJargon || j == w
		}
		if isJargon && !defined[w] && !ownWords[w] && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// tipProblems lists what is wrong with one text, each line naming the owner.
func tipProblems(owner, text, own string, defined map[string]bool) []string {
	var out []string
	for _, s := range longTipSentences(text) {
		out = append(out, owner+": sentence over 25 words: "+s)
	}
	for _, w := range undefinedTipJargon(text, own, defined) {
		out = append(out, owner+": uses \""+w+"\", which has no glossary entry")
	}
	if m := promiseWords.FindString(text); m != "" {
		out = append(out, owner+": promises or implies a return (\""+m+"\")")
	}
	return out
}

func tipTexts(tp tip) []string {
	return []string{tp.Meaning, tp.RaiseRisk, tp.RaiseInc, tp.PolicyNote}
}

// tipsCoverage returns every problem of the tips and the glossary.
func tipsCoverage(tunables []config.Tunable, tips map[string]tip, g map[string]glossaryTip, videos map[string]bool) []string {
	defined := glossaryIndex(g)
	var out []string
	known := map[string]bool{}
	for _, tu := range tunables {
		known[tu.Key] = true
		tp, ok := tips[tu.Key]
		if !ok {
			out = append(out, "tunable "+tu.Key+" has no tip in docs/product/education/tips.json")
			continue
		}
		for field, v := range map[string]string{"meaning": tp.Meaning, "unit": tp.Unit, "raise_risk": tp.RaiseRisk, "raise_income": tp.RaiseInc} {
			if strings.TrimSpace(v) == "" {
				out = append(out, tu.Key+": tip has no "+field)
			}
		}
		if tu.Unit != "" && tp.Unit != tu.Unit && !strings.EqualFold(tp.Unit, strings.ReplaceAll(tu.Unit, "IV", "dvol")) {
			out = append(out, tu.Key+": tip unit \""+tp.Unit+"\" differs from the catalogue's \""+tu.Unit+"\"")
		}
		if tu.Policy != config.PolicyNone && !strings.Contains(tp.PolicyNote, "product owner") {
			out = append(out, tu.Key+": a core-policy setting ("+tu.Policy+") must say a change needs the product owner")
		}
		if tp.Video != "" && !videos[tp.Video] {
			out = append(out, tu.Key+": video "+tp.Video+" is not in video-storyboards.md")
		}
		for _, text := range tipTexts(tp) {
			out = append(out, tipProblems(tu.Key, text, tu.Key, defined)...)
		}
	}
	for key := range tips {
		if !known[key] {
			out = append(out, "tip "+key+" matches no tunable in config.Tunables()")
		}
	}
	for term, e := range g {
		for field, v := range map[string]string{"short": e.Short, "risk_income": e.RiskIncome} {
			if strings.TrimSpace(v) == "" {
				out = append(out, "glossary term \""+term+"\" has no "+field)
				continue
			}
			out = append(out, tipProblems("glossary \""+term+"\" "+field, v, term, defined)...)
		}
	}
	sort.Strings(out)
	return out
}

func storyboardVideos(t *testing.T) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^## (video\.\w+)`).FindAllStringSubmatch(string(readRepoFile(t, "docs/product/education/video-storyboards.md")), -1) {
		ids[m[1]] = true
	}
	return ids
}

// Scenario: every current entry is covered (and no promised returns, no
// undefined jargon, no long sentence in any tip).
func TestTips_EveryTunableAndGlossaryTermIsCovered(t *testing.T) {
	problems := tipsCoverage(config.Tunables(), loadTips(t), loadGlossaryTips(t), storyboardVideos(t))
	if len(problems) > 0 {
		t.Errorf("tips are not plain-words complete:\n%s", strings.Join(problems, "\n"))
	}
}

func TestTips_FileIsInAlphabeticalOrder(t *testing.T) {
	var keys []string
	dec := json.NewDecoder(strings.NewReader(string(readRepoFile(t, "docs/product/education/tips.json"))))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	if !sort.StringsAreSorted(keys) {
		t.Errorf("tips.json keys must stay in alphabetical order: %v", keys)
	}
}

// Scenario: a new tunable without a tip / a tip uses undefined jargon / no
// promised returns — each failure names its key or term.
func TestTips_CoverageFailuresNameTheCulprit(t *testing.T) {
	good := tip{Meaning: "A plain meaning.", Unit: "days", RaiseRisk: "Riskier.", RaiseInc: "More income.", PolicyNote: "A change needs the product owner."}
	g := map[string]glossaryTip{"delta": {Short: "How far.", RiskIncome: "More or less."}}
	tunable := func(key string) config.Tunable {
		return config.Tunable{Key: key, Unit: "days", Policy: config.PolicyEntry}
	}
	with := func(mod func(*tip)) map[string]tip {
		tp := good
		mod(&tp)
		return map[string]tip{"a_key": tp}
	}
	cases := []struct {
		name string
		tips map[string]tip
		g    map[string]glossaryTip
		want string
	}{
		{"new tunable without a tip", map[string]tip{}, g, "tunable a_key has no tip"},
		{"orphan tip", map[string]tip{"a_key": good, "gone": good}, g, "tip gone matches no tunable"},
		{"undefined jargon", with(func(p *tip) { p.RaiseRisk = "Raises gamma and vega." }), g, "uses \"gamma\""},
		{"undefined jargon in the glossary", map[string]tip{"a_key": good}, map[string]glossaryTip{"x": {Short: "Moves with theta.", RiskIncome: "None."}}, "uses \"theta\""},
		{"long sentence", with(func(p *tip) { p.Meaning = strings.Repeat("word ", 26) + "end." }), g, "sentence over 25 words"},
		{"promised return", with(func(p *tip) { p.RaiseInc = "This is a guaranteed income." }), g, "promises or implies a return"},
		{"expected profit", with(func(p *tip) { p.RaiseInc = "You will earn more." }), g, "promises or implies a return"},
		{"missing policy note", with(func(p *tip) { p.PolicyNote = "" }), g, "needs the product owner"},
		{"missing risk text", with(func(p *tip) { p.RaiseRisk = " " }), g, "tip has no raise_risk"},
		{"unknown video", with(func(p *tip) { p.Video = "video.nope" }), g, "video.nope is not in"},
		{"glossary term without risk_income", map[string]tip{"a_key": good}, map[string]glossaryTip{"x": {Short: "Fine."}}, "has no risk_income"},
	}
	for _, c := range cases {
		got := strings.Join(tipsCoverage([]config.Tunable{tunable("a_key")}, c.tips, c.g, map[string]bool{"video.ok": true}), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: want a problem containing %q, got:\n%s", c.name, c.want, got)
		}
	}
	if got := tipsCoverage([]config.Tunable{tunable("a_key")}, map[string]tip{"a_key": good}, g, nil); len(got) != 0 {
		t.Errorf("a clean tip must pass, got %v", got)
	}
	// The word of the key being explained is not undefined jargon.
	if got := undefinedTipJargon("The delta here.", "delta_exit", map[string]bool{}); len(got) != 0 {
		t.Errorf("own-key word flagged: %v", got)
	}
}
