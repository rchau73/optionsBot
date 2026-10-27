package tests

import (
	"fmt"
	"strings"
	"testing"

	"optionsbot/internal/aianalysis"
)

// #257: the AI analysis contract. Fixed sample analyses only; no model call.

// comparisonJSON is the shape of internal/compare's report.json, reduced to
// what the checker reads. trades is the trade count of both configs' backtest.
func comparisonJSON(trades int, extra string) []byte {
	return fmt.Appendf(nil, `{
  "disclaimer": "Backtested/replayed, not a promise",
  "underlying": "BTC",
  "base": "config_btc.yaml", "proposed": "my.yaml",
  "from": "2026-10-12T00:00:00Z", "to": "2026-10-18T00:00:00Z",
  "diffs": [{"key": "gamma_flip_buffer_sd", "base": "1.5", "proposed": "1"}],
  "backtest": {"ran": true,
    "base": {"worst_week_pct": -3.1, "return_pct": 1.8, "fees_usd": 412.5, "trades": %d, "stops": 2},
    "proposed": {"worst_week_pct": -2.4, "return_pct": 2.2, "fees_usd": 430.25, "trades": %d, "stops": 1}},
  "replay": {"ran": true,
    "base": {"net": 0.0123, "max_drawdown": 0.004, "trades": 42, "sheds": 3},
    "proposed": {"net": 0.0151, "max_drawdown": 0.005, "trades": 44, "sheds": 5}},
  "stress": {"ran": false}%s
}`, trades, trades, extra)
}

const goodAnalysis = aianalysisLabel + `
## What changed
The setting gamma_flip_buffer_sd moves from 1.5 to 1.
## Where the proposal is better or worse
In the backtest the worst week is -3.1 % now and -2.4 % proposed. Net return is 1.8 % now and 2.2 % proposed.
In the replay the sheds go from 3 to 5.
## Where it is fragile
Fees are 412.5 now and 430.25 proposed.
## What to test next
Run the same window as a daily replay (window 2026-10-12 to 2026-10-18).`

const aianalysisLabel = "AI-generated analysis of backtested and simulated results; not advice; past and simulated results do not predict future results"

func kinds(vs []aianalysis.Violation) string {
	var k []string
	for _, v := range vs {
		k = append(k, v.Kind)
	}
	return strings.Join(k, ",")
}

func TestAIAnalysis_GroundedAnalysisPasses(t *testing.T) {
	if got := aianalysis.Check(comparisonJSON(30, ""), goodAnalysis); len(got) != 0 {
		t.Fatalf("violations: %v", got)
	}
}

func TestAIAnalysis_InventedNumberIsCaught(t *testing.T) {
	bad := strings.Replace(goodAnalysis, "-2.4 % proposed", "-4.2 % proposed", 1)
	got := aianalysis.Check(comparisonJSON(30, ""), bad)
	if len(got) != 1 || got[0].Kind != aianalysis.KindNumber || got[0].Text != "4.2" {
		t.Fatalf("violations: %v", got)
	}
	if !strings.Contains(got[0].Sentence, "worst week") {
		t.Fatalf("sentence not reported: %q", got[0].Sentence)
	}
}

func TestAIAnalysis_NumbersMatchWithinRounding(t *testing.T) {
	// 412.5 as 413 and 0.0123 as 0.01; a sign is not part of the match.
	text := aianalysisLabel + "\nFees are about 413 and the replay net is 0.01 while the drawdown is 0.0040. The worst week is 3.1 % deep."
	if got := aianalysis.Check(comparisonJSON(30, ""), text); len(got) != 0 {
		t.Fatalf("violations: %v", got)
	}
	off := aianalysisLabel + "\nThe backtest has 31 trades."
	if got := aianalysis.Check(comparisonJSON(30, ""), off); kinds(got) != "number" {
		t.Fatalf("violations: %v", got)
	}
}

func TestAIAnalysis_DatesNamesAndThousands(t *testing.T) {
	in := []byte(`{"backtest":{"ran":true,"base":{"trades":12000,"fees_usd":1234.5},"proposed":{"trades":12000}},"from":"2026-10-12T00:00:00Z"}`)
	ok := aianalysisLabel + "\nOn 2026-10-12 the SMA9 trend saw 12,000 trades and fees of 1,234.5 in week 2026-W42."
	if got := aianalysis.Check(in, ok); len(got) != 0 {
		t.Fatalf("violations: %v", got)
	}
	bad := aianalysisLabel + "\nOn 2026-10-13 it saw 12,000 trades."
	if got := aianalysis.Check(in, bad); kinds(got) != "number,number" {
		t.Fatalf("an invented date should leave its year and day unmatched: %v", got)
	}
}

func TestAIAnalysis_AdviceWordingIsCaught(t *testing.T) {
	for _, phrase := range []string{"You should apply this change.", "This is risk-free.", "It will earn more.", "I give no guarantee, but fine."} {
		got := aianalysis.Check(comparisonJSON(30, ""), goodAnalysis+"\n"+phrase)
		if len(got) == 0 || strings.Trim(strings.ReplaceAll(kinds(got), aianalysis.KindBanned, ""), ",") != "" {
			t.Errorf("%q: violations %v", phrase, got)
		}
	}
}

func TestAIAnalysis_MissingLabelIsCaught(t *testing.T) {
	text := strings.Replace(goodAnalysis, aianalysisLabel, "", 1)
	if got := aianalysis.Check(comparisonJSON(30, ""), text); kinds(got) != aianalysis.KindLabel {
		t.Fatalf("violations: %v", got)
	}
	// Line breaks inside the label do not hide it.
	wrapped := strings.Replace(goodAnalysis, "results; not advice;", "results;\nnot advice;", 1)
	if got := aianalysis.Check(comparisonJSON(30, ""), wrapped); len(got) != 0 {
		t.Fatalf("violations: %v", got)
	}
}

func TestAIAnalysis_ThinDataMustBeAdmitted(t *testing.T) {
	got := aianalysis.Check(comparisonJSON(7, ""), goodAnalysis)
	if kinds(got) != aianalysis.KindCaveat || !strings.Contains(got[0].Text, "backtest") {
		t.Fatalf("violations: %v", got)
	}
	// Mentioning the tool is not enough; the same sentence must admit it.
	said := goodAnalysis + "\nThe backtest has few trades, so I can't tell from this run whether the gain is real."
	if got := aianalysis.Check(comparisonJSON(7, ""), said); len(got) != 0 {
		t.Fatalf("violations: %v", got)
	}
	// Words that merely contain "thin" do not admit anything.
	within := goodAnalysis + "\nIn the backtest the result is within range, something to note."
	if got := aianalysis.Check(comparisonJSON(7, ""), within); kinds(got) != aianalysis.KindCaveat {
		t.Fatalf("violations: %v", got)
	}
	// A tool that did not run is not asked for a caveat.
	notRun := []byte(`{"backtest":{"ran":false,"base":{"trades":0},"proposed":{"trades":0}}}`)
	if got := aianalysis.Check(notRun, aianalysisLabel); len(got) != 0 {
		t.Fatalf("violations: %v", got)
	}
}

func TestAIAnalysis_MissingDaysMustBeAdmitted(t *testing.T) {
	in := comparisonJSON(30, `, "coverage": {"missing_days": ["2026-10-14"]}`)
	if got := aianalysis.Check(in, goodAnalysis); kinds(got) != aianalysis.KindCaveat {
		t.Fatalf("violations: %v", got)
	}
	said := goodAnalysis + "\nThe run has a missing day, 2026-10-14, so the replay is uncertain there."
	if got := aianalysis.Check(in, said); len(got) != 0 {
		t.Fatalf("violations: %v", got)
	}
}

func TestAIAnalysis_BadInput(t *testing.T) {
	if got := aianalysis.Check([]byte("not json"), goodAnalysis); kinds(got) != aianalysis.KindBadInput {
		t.Fatalf("violations: %v", got)
	}
}

func TestAIAnalysis_PromptAndModel(t *testing.T) {
	if aianalysis.Model != "claude-opus-5-5" {
		t.Fatalf("model: %s", aianalysis.Model)
	}
	if !strings.Contains(aianalysis.Prompt, aianalysis.Label) {
		t.Fatal("the prompt must carry the mandatory label word for word")
	}
}
