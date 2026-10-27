package tests

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Exchange feasibility briefs (docs/exchanges, #327): the template has the
// fixed sections, and every factual line of a brief is sourced or labelled.

var briefHeading = regexp.MustCompile(`(?m)^## (\d+)\. (.+)$`)
var briefDate = regexp.MustCompile(`20\d\d-\d\d-\d\d`)

func readBrief(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../docs/exchanges/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

func briefHeadings(doc string) []string {
	var out []string
	for _, m := range briefHeading.FindAllStringSubmatch(doc, -1) {
		out = append(out, m[1]+". "+m[2])
	}
	return out
}

// Scenario: the template is complete
func TestExchangeBriefs_TemplateIsComplete(t *testing.T) {
	doc := readBrief(t, "TEMPLATE.md")
	want := []string{
		"1. Option products offered", "2. Liquidity", "3. Margin model", "4. Order types",
		"5. Public market data and open interest for GEX", "6. Rate limits", "7. Testnet",
		"8. API key scopes", "9. Terms of use for automated trading", "10. Regional restrictions",
		"11. Adapter capabilities missing and verdict",
	}
	got := briefHeadings(doc)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("template sections = %v, want %v", got, want)
	}
	if n := strings.Count(doc, "\nAnswer:\n"); n != len(want) {
		t.Errorf("empty answer lines = %d, want %d", n, len(want))
	}
	if !strings.Contains(doc, "Needs legal review") {
		t.Error("regional restrictions must say it needs legal review")
	}
}

// Scenario: the Bybit brief is sourced
func TestExchangeBriefs_BybitEverythingSourcedAndEndsWithVerdict(t *testing.T) {
	doc := readBrief(t, "bybit.md")
	tmpl := briefHeadings(readBrief(t, "TEMPLATE.md"))
	if got := briefHeadings(doc); strings.Join(got, "|") != strings.Join(tmpl, "|") {
		t.Fatalf("bybit sections = %v, want the template's %v", got, tmpl)
	}
	body := doc[strings.Index(doc, "\n## 1."):]
	facts := 0
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		facts++
		sourced := strings.Contains(line, "](http") && briefDate.MatchString(line)
		labelled := strings.Contains(line, "assumption — verify:") && briefDate.MatchString(line)
		if !sourced && !labelled {
			t.Errorf("fact without source+date or labelled assumption: %s", line)
		}
	}
	if facts < 11 {
		t.Errorf("only %d fact lines", facts)
	}
	last := strings.TrimSpace(doc[strings.LastIndex(doc, "\n## 11."):])
	if !strings.Contains(last, "- `MarginSimulator`: missing") || !strings.Contains(last, "**Verdict: ") {
		t.Error("section 11 must list the missing capabilities and end with a verdict")
	}
	if !strings.HasPrefix(last[strings.LastIndex(last, "\n")+1:], "**Verdict: ") {
		t.Error("the brief must end with the verdict")
	}
}

// Scenario: no account is touched
func TestExchangeBriefs_NoKeyTokenOrPrivateEndpoint(t *testing.T) {
	for _, name := range []string{"TEMPLATE.md", "bybit.md"} {
		doc := strings.ToLower(readBrief(t, name))
		for _, bad := range []string{"api_secret", "apikey=", "bearer ", "/v5/account/wallet", "/v5/asset/", "x-bapi-api-key"} {
			if strings.Contains(doc, bad) {
				t.Errorf("%s contains %q", name, bad)
			}
		}
	}
}
