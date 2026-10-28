package guard

import "regexp"

// The wording the guard enforces (#260). These are the legal brief's fallback
// rules for L11/L12 (docs/product/legal-brief.md; docs/legal/drafts/avisos.md
// "Assistente de IA"): answers only from the published help and the journal's
// recorded reasons, no "should I" answer, no forecast. Counsel's answer to
// L11 changes these lists and replies, not the design. Every pattern is
// matched case-insensitively on the answer's text.

// The canonical replies. An answer that is exactly one of them needs no
// citation. docs/product/assistant/contract.md quotes them and
// scripts/product/assistant_grade.py recognises them
// (tests/assistant_guard_test.go keeps the three equal).
const (
	// DontKnow is the reply when no passage supports an answer.
	DontKnow = "I don't know: the published help and your journal do not answer that."
	// Refusal is the reply to a question asking what to do with one's money
	// or what prices or returns will be (avisos.md: "Não posso recomendar o
	// que fazer com o seu dinheiro…").
	Refusal = "I can't recommend what to do with your money, and I don't forecast prices or returns. " +
		"I can explain how a rule works and what it did in your account."
	// FallbackReply replaces an answer the guard rejected.
	FallbackReply = "I can't answer that. The help explains how the bot works and why it acted: see the guide, OptionStrategy.md."
)

// advicePhrases tell the user what to do: a recommendation, an imperative
// about money or settings, a "good time to". Describing what the bot does
// ("the bot sells a call and a put") is not advice: the imperatives count
// only at the start of a sentence.
var advicePhrases = compile(
	`\byou (should|ought to)\b`,
	`\bi(?: would|'d)? (recommend|suggest|advise)\b`,
	`\bmy (advice|recommendation|suggestion)\b`,
	`\bi(?:'d| would) (buy|sell|increase|raise|lower|reduce|set|keep|wait)\b`,
	`\b(increase|raise|lower|reduce|decrease|change|set|adjust) your\b`,
	`\bconsider (buying|selling|increasing|raising|lowering|reducing|decreasing|setting|changing|adding|withdrawing)\b`,
	`\b(good|great|right|best|bad) (time|moment) to (buy|sell|enter|start|invest|add|exit)\b`,
	`\bnow is (a |the )?(good|great|right|best) (time|moment)\b`,
	`\bgo ahead and\b`,
	`\b(do not|don't) (buy|sell|invest)\b`,
	// Imperatives at the start of the text, a sentence or a line.
	`(?:^|[.!?]\s+|\n\s*)(buy|sell|increase|raise|lower|reduce|decrease|set|turn (on|off)|enable|disable|switch to|withdraw|deposit|invest)\b`,
)

// forecastPhrases say what prices or returns will be, or promise a result
// (the glossary's no-return-promises words included).
var forecastPhrases = compile(
	`\bwill (earn|make|return|yield|gain|profit|go up|go down|rise|fall|drop|climb|crash|rally|double|recover)\b`,
	`\b(will|going to) be (profitable|bullish|bearish)\b`,
	`\b(price|prices|bitcoin|btc|ether|eth|market|markets) (is|are) (going|likely|expected|bound) to\b`,
	`\b(should|likely to|bound to|expected to) (rise|fall|go up|go down|recover|bounce|rally|crash)\b`,
	`\bexpected (return|income|profit|yield|gain)s?\b`,
	`\byou (can|could|will|would) (expect|earn|make|get)\b`,
	`\bguarantee`,
	`\brisk[- ]free\b`,
	`\bno risk\b`,
	`\bcan(not|'t) lose\b`,
	`\bsure (profit|win|income|thing)\b`,
	`\balways (profit|win|earn|makes? money)\b`,
	`\bsafe income\b`,
	`%\s*(a|per|each) (day|week|month|year)\b`,
)

// adviceNegations are the assistant declining to advise ("I can't say whether
// you should use it"): blanked before advicePhrases are matched (#422: a good
// answer ending so was replaced by the fallback).
var adviceNegations = compile(
	`\b(whether|if) you (should|ought to)\b`,
)

// negations deny a promise ("a stop is a trigger, not a guaranteed price"):
// they are blanked before forecastPhrases are matched.
var negations = compile(
	`\b(not|never|no|isn't|aren't) (a )?guarantee[ds]?\b`,
	`\b(do|does|did|can|could|will) ?(not|n't) guarantee\b`,
)

func compile(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(`(?i)` + p)
	}
	return out
}
