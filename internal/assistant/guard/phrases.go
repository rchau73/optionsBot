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
	// FallbackReply replaces an answer the guard rejected (twice, #534). It
	// names no document: the assistant never sends the person to read one.
	FallbackReply = "I couldn't confirm an answer from the bot's records and the help, so I won't guess."
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

// documentPointers send the person to a document, guide or section instead
// of answering ("see the guide", "read OptionStrategy.md", "check section 9"):
// the product owner's rule (2026-10-29, #534) is that the assistant reads the
// help and answers in its own words. A verb at the start of a sentence (or
// after a comma, a colon or "and"), or addressed to "you", then at most three
// words before the document.
var documentPointers = compile(
	`(?:^|[.!?:;,(]\s*|\n\s*|\band\s+)(?:please\s+)?(see|read|check|open|consult|refer to|look at|visit|go to)\s+(?:[\w'’-]+\s+){0,3}?(?:(guide|document|documentation|docs|section|chapter|glossary|manual|help|faq|[\w-]+\.md)\b|§)`,
	`\byou (can|could|may|might|will)(?: also)? (find|see|read|check|open|consult|look at|look up)\s+(?:[\w'’-]+\s+){0,3}?(?:(guide|document|documentation|docs|section|chapter|glossary|manual|help|faq|[\w-]+\.md)\b|§)`,
)

// negations deny a promise ("a stop is a trigger, not a guaranteed price"):
// they are blanked before forecastPhrases are matched.
var negations = compile(
	`\b(not|never|no|isn't|aren't) (a )?guarantee[ds]?\b`,
	`\b(do|does|did|can|could|will) ?(not|n't) guarantee\b`,
)

// englishWords are common English function words, none of them also a common
// Portuguese or Spanish word ("a", "as", "do", "no", "for", "me", "so" left
// out): an answer in which fewer than minEnglishShare of the words are one of
// them is not written in English, and the phrase lists above cannot judge it
// (#565, reason language). Translating the lists is counsel's (L11).
var englishWords = wordSet(
	"the", "of", "and", "to", "in", "is", "are", "was", "were", "it", "its", "that", "this", "these", "those",
	"with", "on", "at", "by", "from", "be", "been", "has", "have", "had", "not", "or", "an", "but", "than",
	"then", "which", "what", "when", "where", "who", "how", "why", "if", "can", "will", "would", "could",
	"does", "did", "you", "your", "they", "their", "there", "each", "only", "also", "into", "after", "before",
	"while", "below", "above", "until", "without", "about", "over", "under", "between", "through", "all", "any",
	"more", "most", "one", "other", "same", "such", "out", "up", "down", "off", "here", "now", "still", "just",
)

// minEnglishShare: the share of englishWords under which an answer is not
// English. English help prose holds about a third of them; a Portuguese
// answer almost none (tests/assistant_guard_test.go pins both sides).
const minEnglishShare = 0.10

func wordSet(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

func compile(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(`(?i)` + p)
	}
	return out
}
