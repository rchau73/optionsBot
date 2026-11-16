package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The outreach kit (docs/product/discovery/outreach.md, #799) holds one
// invitation per channel, in English and PT-BR. These tests keep it sendable
// after counsel's review: nothing promises or implies returns, every channel
// code is one survey_tally.py reads, and every text links to the consent text.

var outreachChannels = []string{"community", "direct-message", "referral", "waiting-list"}

// A phrase is a claim, a promise, pressure or an incentive. Negations such as
// "promises no returns" are fine, so bare "returns" is not listed.
var outreachBanned = []string{
	// performance or yield claims
	"earn up to", "a year", "per year", "per month", "a month", "annual", "apy", "apr",
	"high returns", "great returns", "returns of", "profit", "passive income", "monthly income",
	"ao ano", "por ano", "ao mês", "por mês", "ganhe", "lucro", "renda passiva", "retorno de", "altos retornos",
	// promises
	"guarantee", "risk-free", "no risk", "sure thing", "garantid", "garantia", "sem risco", "certeza de",
	// urgency pressure
	"limited", "last chance", "hurry", "don't miss", "act now", "only today", "spots left",
	"vagas limitadas", "última chance", "corra", "não perca", "só hoje", "agora mesmo",
	// incentives phrased as a return
	"bonus", "reward", "free", "discount", "gift", "prize", "raffle",
	"bônus", "recompensa", "grátis", "desconto", "brinde", "prêmio", "sorteio",
}

type outreachText struct{ channel, lang, body string }

// outreachTexts splits the file into its invitations: "## n. channel", then "### English" / "### Português (PT-BR)".
func outreachTexts(doc string) []outreachText {
	var out []outreachText
	channel, lang := "", ""
	var buf []string
	flush := func() {
		if channel != "" && lang != "" {
			out = append(out, outreachText{channel, lang, strings.Join(buf, "\n")})
		}
		buf = nil
	}
	num := regexp.MustCompile(`^## \d+\.\s*`)
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			channel, lang = strings.TrimSpace(num.ReplaceAllString(line, "")), ""
		case strings.HasPrefix(line, "### "):
			flush()
			lang = strings.TrimSpace(strings.TrimPrefix(line, "### "))
		default:
			buf = append(buf, line)
		}
	}
	flush()
	return out
}

// outreachViolation returns the first banned phrase in s, or "".
func outreachViolation(s string) string {
	low := strings.ToLower(s)
	for _, p := range outreachBanned {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(p)).MatchString(low) {
			return p
		}
	}
	return regexp.MustCompile(`\d\s?%`).FindString(low) // any percentage reads as a figure
}

// Scenario: the kit says first it is a draft awaiting counsel.
func TestOutreachKit_FirstLineSaysDraftAwaitingCounsel(t *testing.T) {
	first := strings.ToLower(strings.SplitN(readDiscovery(t, "outreach.md"), "\n", 2)[0])
	for _, w := range []string{"draft", "counsel", "do not send"} {
		if !strings.Contains(first, w) {
			t.Errorf("outreach.md first line lacks %q: %s", w, first)
		}
	}
}

// Scenario: every channel has an invitation in English and in PT-BR, with the
// consent link, its own channel code and the survey's minutes.
func TestOutreachKit_EveryChannelHasBothLanguagesAndConsentLink(t *testing.T) {
	if !strings.Contains(readDiscovery(t, "survey.md"), "about 6 minutes") {
		t.Fatal("survey.md no longer says about 6 minutes: update the outreach texts")
	}
	texts := outreachTexts(readDiscovery(t, "outreach.md"))
	for _, ch := range outreachChannels {
		for _, lang := range []string{"English", "Português (PT-BR)"} {
			found := false
			for _, x := range texts {
				if x.channel != ch || x.lang != lang {
					continue
				}
				found = true
				if !strings.Contains(x.body, "(consent.md)") {
					t.Errorf("outreach.md: %s / %s has no link to consent.md", ch, lang)
				}
				if !strings.Contains(x.body, "?channel="+ch) {
					t.Errorf("outreach.md: %s / %s does not link the survey with its own channel code", ch, lang)
				}
				if !strings.Contains(x.body, "6 minut") {
					t.Errorf("outreach.md: %s / %s does not say the form takes 6 minutes", ch, lang)
				}
			}
			if !found {
				t.Errorf("outreach.md: no %s invitation for channel %s", lang, ch)
			}
		}
	}
}

// Scenario: every channel code in the table is one the survey tally reads.
func TestOutreachKit_ChannelCodesAreAcceptedByTheTally(t *testing.T) {
	py, err := os.ReadFile("../scripts/product/survey_tally.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`CHANNEL_LABEL = re\.compile\(r"([^"]+)"\)`).FindStringSubmatch(string(py))
	if m == nil {
		t.Fatal("CHANNEL_LABEL not found in survey_tally.py")
	}
	label := regexp.MustCompile(`^(?:` + m[1] + `)$`)
	rows := regexp.MustCompile("(?m)^\\| `([^`]+)` \\| `\\?channel=([^`]+)` \\|").FindAllStringSubmatch(readDiscovery(t, "outreach.md"), -1)
	if len(rows) != len(outreachChannels) {
		t.Fatalf("code table has %d rows, want %d", len(rows), len(outreachChannels))
	}
	for i, r := range rows {
		if r[1] != r[2] {
			t.Errorf("code %q and its link suffix %q differ", r[1], r[2])
		}
		if !label.MatchString(r[1]) {
			t.Errorf("channel code %q is not accepted by survey_tally.py (%s)", r[1], m[1])
		}
		if r[1] != outreachChannels[i] {
			t.Errorf("code table row %d is %q, want %q (alphabetical)", i, r[1], outreachChannels[i])
		}
	}
}

// Scenario: no text promises or implies returns; a failure names the phrase and the file.
func TestOutreachKit_NoClaimsPromisesPressureOrIncentives(t *testing.T) {
	for _, x := range outreachTexts(readDiscovery(t, "outreach.md")) {
		if p := outreachViolation(x.body); p != "" {
			t.Errorf("outreach.md: %s / %s contains the banned phrase %q", x.channel, x.lang, p)
		}
	}
}

func TestOutreachKit_BannedPhraseIsCaught(t *testing.T) {
	for _, bad := range []string{"Earn 20% a year with us", "guaranteed income", "Vagas limitadas, corra", "ganhe um bônus"} {
		if outreachViolation(bad) == "" {
			t.Errorf("%q should be caught", bad)
		}
	}
	if p := outreachViolation("It promises no returns and nothing is for sale yet."); p != "" {
		t.Errorf("the honest no-returns sentence must pass, got %q", p)
	}
}

// Public-safe like the other kit files: no e-mail address, no amount.
func TestOutreachKit_PublicSafe(t *testing.T) {
	doc := readDiscovery(t, "outreach.md")
	if regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`).MatchString(doc) {
		t.Error("outreach.md contains an e-mail address")
	}
	if regexp.MustCompile(`(?i)[$€£]\s?\d|\d\s?(usd|usdc|usdt|brl|eur|btc|eth)\b`).MatchString(doc) {
		t.Error("outreach.md contains an amount")
	}
}
