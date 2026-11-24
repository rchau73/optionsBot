package tests

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The waiting-list texts (#882): every text the fake door sends or shows
// beyond the page, in English and PT-BR, held to the rules of the page copy
// (tests/landing_copy_test.go) and mapped to counsel's questions.

const waitingListTextsPath = "../docs/product/experiments/waiting-list-texts.md"

// wlText is one text of §1: its id and its two language versions.
type wlText struct{ id, en, pt string }

var (
	wlPromiseWord = regexp.MustCompile(`(?i)guarantee|garantid|garantia|\bretorno|rendimento|rentabilidade|\blucro|\bganhos?\b`)
	wlMoney       = regexp.MustCompile(`[$€£]|R\$`)
	wlPlaceholder = regexp.MustCompile(`\[[^\]]*\]`)
	wlSentenceEnd = regexp.MustCompile(`[.!?]\s+`)
	wlNoPrice     = regexp.MustCompile(`(?i)\bno price\b|não há preço`)
	wlPriceWord   = regexp.MustCompile(`(?i)\bprice\b|\bpreço\b`)
)

// quoteAfter returns the blockquote lines that follow marker, joined by
// newlines, up to the next line that is not part of the quote.
func quoteAfter(body, marker string) string {
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	var out []string
	for _, line := range strings.Split(body[i+len(marker):], "\n")[1:] {
		if !strings.HasPrefix(line, ">") {
			if len(out) > 0 {
				break
			}
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(line, ">")))
	}
	return strings.Join(out, "\n")
}

// waitingListTexts parses §1: one "### Tn." heading per text.
func waitingListTexts(t *testing.T, doc string) []wlText {
	t.Helper()
	body := section(t, doc, "1")
	var texts []wlText
	for _, part := range strings.Split(body, "\n### ")[1:] {
		id := strings.SplitN(part, ".", 2)[0]
		texts = append(texts, wlText{id, quoteAfter(part, "**EN**"), quoteAfter(part, "**PT-BR**")})
	}
	return texts
}

// wlViolations returns what breaks the rules in one text of one language.
func wlViolations(id, lang, text string) []string {
	var bad []string
	if strings.TrimSpace(text) == "" {
		return []string{id + " " + lang + ": no text"}
	}
	if m := returnWord.FindString(text); m != "" {
		bad = append(bad, id+" "+lang+": promises or quotes a return ("+m+")")
	}
	if m := wlPromiseWord.FindString(text); m != "" {
		bad = append(bad, id+" "+lang+": promise word "+m)
	}
	if m := wlMoney.FindString(text); m != "" {
		bad = append(bad, id+" "+lang+": an amount ("+m+")")
	}
	for _, d := range digit.FindAllString(text, -1) {
		if d != "18" {
			bad = append(bad, id+" "+lang+": a figure "+d)
		}
	}
	if wlPriceWord.MatchString(wlNoPrice.ReplaceAllString(text, "")) {
		bad = append(bad, id+" "+lang+": names a price")
	}
	plain := wlPlaceholder.ReplaceAllString(text, "X")
	for _, line := range strings.Split(plain, "\n") {
		for _, s := range wlSentenceEnd.Split(line+" ", -1) {
			if n := len(strings.Fields(s)); n > 25 {
				bad = append(bad, id+" "+lang+": a sentence of "+strconv.Itoa(n)+" words")
			}
		}
	}
	return bad
}

// Scenario: every text exists in both languages.
func TestWaitingListTexts_BothLanguages(t *testing.T) {
	doc := readDoc(t, waitingListTextsPath)
	if head := doc[:min(len(doc), 400)]; !strings.Contains(head, "**Needs legal review: not to be sent or published.**") {
		t.Error("header does not say the texts need legal review")
	}
	texts := waitingListTexts(t, doc)
	if len(texts) != 5 {
		t.Fatalf("%d texts, want 5", len(texts))
	}
	for i, tx := range texts {
		if want := "T" + string(rune('1'+i)); tx.id != want {
			t.Errorf("text %d is %q, want %q", i+1, tx.id, want)
		}
		if strings.TrimSpace(tx.en) == "" || strings.TrimSpace(tx.pt) == "" {
			t.Errorf("%s lacks its English or PT-BR version", tx.id)
		}
	}
	// A person fills the controller and the contact; none is written in.
	for _, tx := range texts {
		if (tx.id == "T1" || tx.id == "T3" || tx.id == "T4") && (!strings.Contains(tx.en, "Controller: [FILL]") || !strings.Contains(tx.pt, "Controlador: [FILL]")) {
			t.Errorf("%s does not leave the controller as [FILL]", tx.id)
		}
	}
}

// Scenario: no text offers or promises; the confirmation e-mail says nothing
// is available and carries the risk line.
func TestWaitingListTexts_NoPromises(t *testing.T) {
	texts := waitingListTexts(t, readDoc(t, waitingListTextsPath))
	for _, tx := range texts {
		for _, v := range append(wlViolations(tx.id, "EN", tx.en), wlViolations(tx.id, "PT-BR", tx.pt)...) {
			t.Error(v)
		}
	}
	for _, tx := range texts {
		if tx.id != "T1" {
			continue
		}
		for _, want := range []string{"Nothing is available yet", "Options can lose more than the premium collected."} {
			if !strings.Contains(tx.en, want) {
				t.Errorf("T1 EN lacks %q", want)
			}
		}
		for _, want := range []string{"Nada está disponível ainda", "Opções podem perder mais do que o prêmio recebido."} {
			if !strings.Contains(tx.pt, want) {
				t.Errorf("T1 PT-BR lacks %q", want)
			}
		}
	}
	// The English risk line is the page footer's (C9).
	if !strings.Contains(readDoc(t, landingCopyPath), "Options can lose more than the premium collected.") {
		t.Error("the page footer no longer carries the risk line T1 repeats")
	}
}

// Scenario: the test guards the file — a promise or an amount fails, naming the text.
func TestWaitingListTexts_RulesCatch(t *testing.T) {
	cases := map[string]string{
		"guarantee":     "Your place is guaranteed.",
		"garantid":      "Seu lugar está garantido.",
		"an amount":     "Join for $20 a month.",
		"a figure":      "Join before 30 days.",
		"a return":      "Expect a good return.",
		"names a price": "The price is low.",
		"a sentence of": strings.Repeat("word ", 26) + "end.",
	}
	for want, text := range cases {
		got := strings.Join(wlViolations("T9", "EN", text), "; ")
		if !strings.Contains(got, want) || !strings.Contains(got, "T9") {
			t.Errorf("%q: violations %q do not name T9 and %q", text, got, want)
		}
	}
	if v := wlViolations("T9", "EN", "There is no price. You must be 18 or older."); len(v) != 0 {
		t.Errorf("a clean text fails: %v", v)
	}
}

// Scenario: mapped to counsel's questions.
func TestWaitingListTexts_HandOffSheet(t *testing.T) {
	doc := readDoc(t, waitingListTextsPath)
	known := map[string]bool{}
	for _, m := range briefLRow.FindAllStringSubmatch(readDoc(t, legalBriefPath), -1) {
		known[m[1]] = true
	}
	for _, id := range lNumber.FindAllString(doc, -1) {
		if !known[id] {
			t.Errorf("the file cites %s, which is not in the legal brief", id)
		}
	}

	// Every element of the page and every new text has a row with an L-number.
	want := map[string]bool{}
	page, values := copyRows(t)
	for _, r := range append(page, values...) {
		want[r[0]] = true
	}
	for _, tx := range waitingListTexts(t, doc) {
		want[tx.id] = true
	}
	rows := planTable(t, doc, "2")
	for _, r := range rows {
		if len(r) < 5 || !want[r[0]] {
			continue
		}
		delete(want, r[0])
		if len(lNumber.FindAllString(r[3], -1)) == 0 {
			t.Errorf("%s names no legal-brief question", r[0])
		}
		if strings.TrimSpace(r[4]) == "" {
			t.Errorf("%s names no fallback", r[0])
		}
	}
	for id := range want {
		t.Errorf("%s is not in the hand-off table", id)
	}

	// The seven open questions of the brief's §8 are each listed once.
	open := doc[strings.Index(doc, "### Open questions"):]
	seen := map[string]int{}
	for _, m := range regexp.MustCompile(`(?m)^\| (\d) \|[^\n]*\| (T\d|C\d)`).FindAllStringSubmatch(open, -1) {
		seen[m[1]]++
	}
	for q := '1'; q <= '7'; q++ {
		if seen[string(q)] != 1 {
			t.Errorf("open question %c listed %d times, want once with a text", q, seen[string(q)])
		}
	}
}
