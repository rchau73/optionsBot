package tests

import (
	"regexp"
	"strings"
	"testing"
)

// The pricing proposal (docs/product/pricing-proposal.md, #762) is public: it
// states no price, and every untested belief says how to verify it.

const pricingProposalPath = "../docs/product/pricing-proposal.md"

var (
	currencyAmount = regexp.MustCompile(`(?i)[$€£¥]\s?\d|R\$|\b(usd|brl|eur|btc)\s?\d|\d\s?(usd|brl|eur|reais|dollars)\b`)
	percentFigure  = regexp.MustCompile(`\d\s?(%|percent|per cent|bps)`)
)

func TestPricingProposal_PublicPageHoldsNoPrice(t *testing.T) {
	page := readDoc(t, pricingProposalPath)
	for i, line := range strings.Split(page, "\n") {
		if m := currencyAmount.FindString(line); m != "" {
			t.Errorf("line %d: currency amount %q", i+1, m)
		}
		if m := percentFigure.FindString(line); m != "" {
			t.Errorf("line %d: a percentage %q (no fee as a share of capital)", i+1, m)
		}
	}
}

func TestPricingProposal_EveryAssumptionNamesItsVerification(t *testing.T) {
	page := readDoc(t, pricingProposalPath)
	marked := 0
	for i, line := range strings.Split(page, "\n") {
		if !strings.HasPrefix(line, "|") || !strings.Contains(line, "**assumption**") {
			continue
		}
		marked++
		if !strings.Contains(line, "Verify:") {
			t.Errorf("line %d: an assumption row without a Verify: step", i+1)
		}
	}
	if marked == 0 {
		t.Error("no row is marked assumption")
	}
}

func TestPricingProposal_LegalGatesAreVisible(t *testing.T) {
	page := readDoc(t, pricingProposalPath)
	for _, want := range []string{
		"**L8: needs legal review.**",
		"**Any fee that scales with capital: needs legal review**",
		"No price is published before counsel answers L8",
		"No profit share",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestPricingProposal_LinkedFromOptionsAndScorecard(t *testing.T) {
	for _, path := range []string{"../docs/product/pricing-options.md", "../docs/product/go-no-go.md"} {
		if !strings.Contains(readDoc(t, path), "pricing-proposal.md") {
			t.Errorf("%s does not link pricing-proposal.md", path)
		}
	}
}
