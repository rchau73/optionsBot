package tests

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"optionsbot/internal/assistant/guard"
	"optionsbot/internal/helpkb"
)

// The assistant's answer guard (#260): citation, grounded numbers, no advice,
// no forecast, checked in code before any answer could be shown.

const (
	srcIdea    = "OptionStrategy.md#1-the-idea-in-one-paragraph"
	srcParams  = "OptionStrategy.md#10-the-parameters-in-plain-words"
	srcRegimes = "OptionStrategy.md#9-market-regimes-and-what-the-bot-does"
	srcLossB   = "OptionStrategy.md#loss-b-gap-through-the-stop"
)

func helpPassages(t *testing.T) []helpkb.Passage {
	t.Helper()
	return buildHelpIndex(t, helpSources(t)).Chunks
}

func checkAnswer(t *testing.T, text string, citations ...string) guard.Verdict {
	t.Helper()
	return guard.Check(guard.Answer{Text: text, Citations: citations}, helpPassages(t))
}

// Scenario: a grounded, cited answer passes.
func TestAssistantGuard_GroundedCitedAnswerPasses(t *testing.T) {
	v := checkAnswer(t, "A leg is closed early when its delta reaches 0.30, bought back at 2× its premium loss, "+
		"and a strangle is rolled with 15 days left.", srcParams)
	if !v.OK {
		t.Fatalf("verdict %+v, want OK", v)
	}
}

// Scenario: an invented number is rejected.
func TestAssistantGuard_InventedNumberIsRejected(t *testing.T) {
	v := checkAnswer(t, "The take-profit buys a leg back once 65 % of its premium is earned.", srcParams)
	if v.OK || !slices.Contains(v.Reasons, guard.ReasonUngroundedNumber) {
		t.Fatalf("verdict %+v, want ungrounded_number", v)
	}
	if !slices.Contains(v.Details, "ungrounded_number: 65") {
		t.Errorf("details %v do not name 65", v.Details)
	}
}

// Scenario: advice and forecasts are rejected, and Fallback returns the
// canonical safe reply.
func TestAssistantGuard_AdviceAndForecastsAreRejected(t *testing.T) {
	for _, c := range []struct{ text, reason string }{
		{"You should sell more puts.", guard.ReasonAdvice},
		{"With these settings you can expect 2 % a month.", guard.ReasonForecast},
	} {
		v := checkAnswer(t, c.text, srcParams)
		if v.OK || !slices.Contains(v.Reasons, c.reason) {
			t.Errorf("%q: verdict %+v, want %s", c.text, v, c.reason)
		}
		if got := guard.Fallback(v); got != guard.FallbackReply {
			t.Errorf("Fallback = %q", got)
		}
	}
	if v := checkAnswer(t, guard.FallbackReply); !v.OK {
		t.Errorf("the fallback reply itself is rejected: %+v", v)
	}
}

// Scenario (#534): no canned reply names a document, and a model answer
// "see the guide, OptionStrategy.md" is rejected as points_to_document.
func TestAssistantGuard_NeverPointsToADocument(t *testing.T) {
	for _, reply := range []string{guard.DontKnow, guard.Refusal, guard.FallbackReply} {
		for _, word := range []string{"see ", "read ", "guide", ".md", "section", "document"} {
			if strings.Contains(strings.ToLower(reply), word) {
				t.Errorf("canned reply %q names %q", reply, word)
			}
		}
	}
	v := checkAnswer(t, "see the guide, OptionStrategy.md", srcIdea)
	if v.OK || !slices.Contains(v.Reasons, guard.ReasonPointsToDocument) {
		t.Fatalf("verdict %+v, want points_to_document", v)
	}
	if !slices.Contains(v.Details, "points_to_document: see the guide") {
		t.Errorf("details %v do not name the pointer", v.Details)
	}
}

// Scenario: describing the strategy is not advice.
func TestAssistantGuard_DescribingTheStrategyIsNotAdvice(t *testing.T) {
	if v := checkAnswer(t, "The bot sells a call and a put.", srcIdea); !v.OK {
		t.Fatalf("verdict %+v, want OK", v)
	}
}

// One positive and one negative case per rule, and the false positives that
// must pass.
func TestAssistantGuard_Rules(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		citations []string
		reason    string // "" = OK
	}{
		{"no citation", "The bot sells options.", nil, guard.ReasonNoCitation},
		{"I don't know needs no citation", guard.DontKnow, nil, ""},
		{"the refusal needs no citation", guard.Refusal, nil, ""},
		{"a refusal with more text needs a citation", guard.Refusal + " The stop-loss is a rule.", nil, guard.ReasonNoCitation},
		// #427: a mixed question — the facts cited, the advice part declined with the refusal.
		{"cited facts with the refusal pass", "A leg is closed early when its delta reaches 0.30. " + guard.Refusal, []string{srcParams}, ""},
		{"I don't know plus the refusal needs no citation", guard.DontKnow + " " + guard.Refusal, nil, ""},
		{"unknown citation", "The bot sells options.", []string{"glossary:no-such-term"}, guard.ReasonUnknownCitation},
		{"a passage id is a citation", "The bot sells options.", []string{srcIdea + "/1"}, ""},
		{"ungrounded number", "The stop-loss fires at 3× the premium.", []string{"glossary:stop-loss"}, guard.ReasonUngroundedNumber},
		{"0.30 = 30 %", "A leg is closed early when its delta reaches 30 %.", []string{"glossary:delta_exit_threshold"}, ""},
		{"50 % = 0.50", "The take-profit setting is 0.50.", []string{"glossary:take-profit"}, ""},
		{"thousands separator", "The illustration has BTC at $100,000.", []string{"OptionStrategy.md#the-payoff-of-a-short-strangle-at-expiry"}, ""},
		{"grounded date", "On 2026-10-15 ETH sat frozen.", []string{srcRegimes}, ""},
		{"ungrounded date", "On 2026-01-01 ETH sat frozen.", []string{srcRegimes}, guard.ReasonUngroundedNumber},
		{"advice: recommend", "I recommend the 25-day slot.", []string{srcParams}, guard.ReasonAdvice},
		{"advice: imperative", "Sell the put now.", []string{srcIdea}, guard.ReasonAdvice},
		{"advice: your setting", "Increase your margin limit.", []string{srcParams}, guard.ReasonAdvice},
		{"advice: good time", "It is a good time to sell options.", []string{srcIdea}, guard.ReasonAdvice},
		{"not advice: the bot does not sell", "In a down-trend the bot does not sell puts.", []string{srcRegimes}, ""},
		{"not advice: declining to advise (#422)", "I can explain how a manual close works, but I can't say whether you should use it.", []string{srcIdea}, ""},
		{"advice: you should", "You should sell more puts.", []string{srcParams}, guard.ReasonAdvice},
		{"not advice: puts gain", "Puts gain value when the price falls below the strike.", []string{"glossary:put"}, ""},
		{"forecast: price", "Bitcoin will go up after the expiry.", []string{srcIdea}, guard.ReasonForecast},
		{"forecast: return", "The expected return of the strategy is high.", []string{srcIdea}, guard.ReasonForecast},
		{"forecast: promise", "Selling strangles is risk-free.", []string{srcIdea}, guard.ReasonForecast},
		{"not a forecast: a denied guarantee", "Stop-loss levels are triggers, not guaranteed prices.", []string{srcLossB}, ""},
		{"not a forecast: the bot will buy back", "The bot will buy back the leg at market.", []string{srcIdea}, ""},
		// #534: the assistant answers, never sends the person to a document.
		{"points to a document: see the guide", "The bot sells a call and a put: see the guide, OptionStrategy.md.", []string{srcIdea}, guard.ReasonPointsToDocument},
		{"points to a document: read a file", "Read OptionStrategy.md for the details.", []string{srcIdea}, guard.ReasonPointsToDocument},
		{"points to a document: check a section", "The bot sells a call and a put. Check section 9.", []string{srcIdea}, guard.ReasonPointsToDocument},
		{"points to a document: you can find", "You can find more in the glossary.", []string{srcIdea}, guard.ReasonPointsToDocument},
		{"not a pointer: the bot checks the help's rule", "The bot checks the spread before a stop-loss.", []string{srcIdea}, ""},
		{"not a pointer: the help explains", "The help explains that the bot sells a call and a put.", []string{srcIdea}, ""},
	}
	for _, c := range cases {
		v := checkAnswer(t, c.text, c.citations...)
		switch {
		case c.reason == "" && !v.OK:
			t.Errorf("%s: %q rejected: %v", c.name, c.text, v.Details)
		case c.reason != "" && (v.OK || !slices.Contains(v.Reasons, c.reason)):
			t.Errorf("%s: %q verdict %+v, want %s", c.name, c.text, v, c.reason)
		}
	}
}

// Scenario (#313): citations must exist — an answer citing an id not in
// help-index.json fails with the unknown id named.
func TestAssistantGuard_UnknownCitationIsNamed(t *testing.T) {
	v := checkAnswer(t, "The bot sells options.", srcIdea, "OptionStrategy.md#99-nowhere/1")
	if v.OK || !slices.Contains(v.Details, "unknown_citation: OptionStrategy.md#99-nowhere/1") {
		t.Fatalf("verdict %+v, want the unknown id named", v)
	}
}

func TestAssistantGuard_Numbers(t *testing.T) {
	var keys []string
	for _, n := range guard.Numbers("SMA9 rollout_19dte 1,454 lots, 0,30 and 0.30, 72,000 on 2026-10-12, 88k") {
		keys = append(keys, n.Key)
	}
	want := []string{"n:1454", "n:0.3", "n:0.3", "n:72000", "date:2026-10-12", "n:88"}
	if !slices.Equal(keys, want) {
		t.Errorf("Numbers = %v, want %v", keys, want)
	}
}

// oracleAnswer is a correct answer to a golden question built from the set
// itself: the must_say facts citing the expected sources, or the canonical
// refusal / I-don't-know.
func oracleAnswer(q goldenQuestion) guard.Answer {
	switch q.ExpectedBehaviour {
	case "refuse":
		return guard.Answer{Text: guard.Refusal}
	case "dont_know":
		return guard.Answer{Text: guard.DontKnow}
	}
	return guard.Answer{Text: "From the help: " + strings.Join(q.MustSay, ", ") + ".", Citations: q.ExpectedSources}
}

// Every golden question's correct answer passes the guard, and each of its
// must_not_say phrases, added to an otherwise valid answer, is rejected.
func TestAssistantGuard_GoldenSet(t *testing.T) {
	passages := helpPassages(t)
	valid := guard.Answer{Text: "The bot sells a call and a put.", Citations: []string{srcIdea}}
	for _, q := range loadGolden(t) {
		a := oracleAnswer(q)
		// A request case's answer is checked against its own fixture's
		// passages, by the monitor's request check (#409).
		if q.Category == "request" {
			a = valid
		}
		// A question about a bot's own data is checked against the passages
		// the monitor builds from its fixture (assistant-live.test.js, #427).
		if q.Bot != nil {
			a = valid
		}
		if v := guard.Check(a, passages); !v.OK {
			t.Errorf("%s: the correct answer %q is rejected: %v", q.ID, a.Text, v.Details)
		}
		base := valid
		if q.ExpectedBehaviour == "answer" {
			base = a
		}
		for _, phrase := range q.MustNotSay {
			bad := guard.Answer{Text: base.Text + " " + phrase + ".", Citations: base.Citations}
			if v := guard.Check(bad, passages); v.OK {
				t.Errorf("%s: must_not_say %q passes the guard", q.ID, phrase)
			}
		}
	}
}

// languageCase is a case of tests/testdata/assistant_language_cases.json,
// shared with the JavaScript twin's test (#565).
type languageCase struct {
	Name      string   `json:"name"`
	Text      string   `json:"text"`
	Citations []string `json:"citations"`
	Reason    string   `json:"reason"`
}

func loadLanguageCases(t *testing.T) []languageCase {
	t.Helper()
	data, err := os.ReadFile("testdata/assistant_language_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []languageCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	for i := range file.Cases {
		file.Cases[i].Text = strings.ReplaceAll(file.Cases[i].Text, "{refusal}", guard.Refusal)
	}
	return file.Cases
}

// Scenario (#565): a Portuguese answer with a valid citation is rejected with
// reason "language" — the case file the JavaScript twin's test reads too, so
// both guards agree on the same cases; English answers pass.
func TestAssistantGuard_PortugueseAnswerIsRejected(t *testing.T) {
	cases := loadLanguageCases(t)
	if len(cases) == 0 {
		t.Fatal("no language cases")
	}
	for _, c := range cases {
		v := checkAnswer(t, c.Text, c.Citations...)
		if c.Reason == "" {
			if !v.OK {
				t.Errorf("%s: %q rejected: %v", c.Name, c.Text, v.Details)
			}
			continue
		}
		// Only the language: a citation or number slip would hide what the case pins.
		if !slices.Equal(v.Reasons, []string{c.Reason}) {
			t.Errorf("%s: %q verdict %+v, want only %s", c.Name, c.Text, v, c.Reason)
		}
		if !strings.HasPrefix(v.Details[0], guard.ReasonLanguage+": ") || !strings.HasSuffix(v.Details[0], "words are English") {
			t.Errorf("%s: detail %q does not count the English words", c.Name, v.Details[0])
		}
	}
}

// Scenario (#565): a normal English answer is unaffected — every canonical
// reply and every passage of the English help (the text an answer quotes),
// cited to itself, clears the language check. A fragment of under five words
// with no common English word (a heading alone) fails closed and is asked
// once more: assistant_language_cases.json pins one.
func TestAssistantGuard_EnglishIsNeverLanguage(t *testing.T) {
	for _, reply := range []string{guard.DontKnow, guard.Refusal, guard.FallbackReply, guard.DontKnow + " " + guard.Refusal} {
		if v := checkAnswer(t, reply); !v.OK {
			t.Errorf("canonical reply %q rejected: %v", reply, v.Details)
		}
	}
	for _, p := range helpPassages(t) {
		if _, words := guard.EnglishShare(p.Text); words < 5 {
			continue
		}
		v := guard.Check(guard.Answer{Text: p.Text, Citations: []string{p.ID}}, []helpkb.Passage{p})
		if slices.Contains(v.Reasons, guard.ReasonLanguage) {
			t.Errorf("help passage %s reads as not English: %v", p.ID, v.Details)
		}
	}
}

// The canonical replies are written once in the guard; the contract quotes
// them and the offline grader recognises them.
func TestAssistantGuard_CanonicalRepliesMatchContractAndGrader(t *testing.T) {
	for _, file := range []string{"../docs/product/assistant/contract.md", "../scripts/product/assistant_grade.py"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(strings.Fields(string(data)), " ")
		for _, reply := range []string{guard.DontKnow, guard.Refusal, guard.FallbackReply} {
			if !strings.Contains(text, reply) {
				t.Errorf("%s does not hold the canonical reply %q", file, reply)
			}
		}
	}
}
