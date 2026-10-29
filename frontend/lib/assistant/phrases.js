// The assistant's wording (#191): the canonical replies, the AI notice and
// the phrase lists the answer guard enforces. A copy of
// internal/assistant/guard/phrases.go (#260), which stays the reference:
// __tests__/assistant-parity.test.js fails when the two differ, so counsel's
// answer to L11 changes both files in the same PR. Every pattern is matched
// case-insensitively on the answer's text.

/** The reply when no passage supports an answer. */
export const DONT_KNOW = "I don't know: the published help and your journal do not answer that.";

/** The reply to "what should I do with my money" and to forecasts (avisos.md "Assistente de IA"). */
export const REFUSAL =
  "I can't recommend what to do with your money, and I don't forecast prices or returns. " +
  "I can explain how a rule works and what it did in your account.";

/** Replaces an answer the guard rejected (twice, #534); it names no document. */
export const FALLBACK_REPLY = "I couldn't confirm an answer from the bot's records and the help, so I won't guess.";

/**
 * Shown with every answer (L12, PRD §11): the English of avisos.md's fixed
 * notice for the assistant.
 */
export const AI_NOTICE =
  "You are talking to an AI assistant. It explains how optionsBot works and why the bot took each decision, " +
  "citing the source. It gives no investment recommendation, does not forecast prices or results, and can be " +
  "wrong: check the cited source.";

/** Recommendation or imperative phrasing aimed at the user. */
export const ADVICE_PATTERNS = [
  String.raw`\byou (should|ought to)\b`,
  String.raw`\bi(?: would|'d)? (recommend|suggest|advise)\b`,
  String.raw`\bmy (advice|recommendation|suggestion)\b`,
  String.raw`\bi(?:'d| would) (buy|sell|increase|raise|lower|reduce|set|keep|wait)\b`,
  String.raw`\b(increase|raise|lower|reduce|decrease|change|set|adjust) your\b`,
  String.raw`\bconsider (buying|selling|increasing|raising|lowering|reducing|decreasing|setting|changing|adding|withdrawing)\b`,
  String.raw`\b(good|great|right|best|bad) (time|moment) to (buy|sell|enter|start|invest|add|exit)\b`,
  String.raw`\bnow is (a |the )?(good|great|right|best) (time|moment)\b`,
  String.raw`\bgo ahead and\b`,
  String.raw`\b(do not|don't) (buy|sell|invest)\b`,
  String.raw`(?:^|[.!?]\s+|\n\s*)(buy|sell|increase|raise|lower|reduce|decrease|set|turn (on|off)|enable|disable|switch to|withdraw|deposit|invest)\b`,
];

/** The assistant declining to advise ("I can't say whether you should use it"): blanked before ADVICE_PATTERNS match (#422). */
export const ADVICE_NEGATION_PATTERNS = [String.raw`\b(whether|if) you (should|ought to)\b`];

/** Forward-looking price or return phrasing, or a promise. */
export const FORECAST_PATTERNS = [
  String.raw`\bwill (earn|make|return|yield|gain|profit|go up|go down|rise|fall|drop|climb|crash|rally|double|recover)\b`,
  String.raw`\b(will|going to) be (profitable|bullish|bearish)\b`,
  String.raw`\b(price|prices|bitcoin|btc|ether|eth|market|markets) (is|are) (going|likely|expected|bound) to\b`,
  String.raw`\b(should|likely to|bound to|expected to) (rise|fall|go up|go down|recover|bounce|rally|crash)\b`,
  String.raw`\bexpected (return|income|profit|yield|gain)s?\b`,
  String.raw`\byou (can|could|will|would) (expect|earn|make|get)\b`,
  String.raw`\bguarantee`,
  String.raw`\brisk[- ]free\b`,
  String.raw`\bno risk\b`,
  String.raw`\bcan(not|'t) lose\b`,
  String.raw`\bsure (profit|win|income|thing)\b`,
  String.raw`\balways (profit|win|earn|makes? money)\b`,
  String.raw`\bsafe income\b`,
  String.raw`%\s*(a|per|each) (day|week|month|year)\b`,
];

/** Sending the person to a document, guide or section instead of answering (#534: the assistant reads the help and answers). */
export const DOCUMENT_POINTER_PATTERNS = [
  String.raw`(?:^|[.!?:;,(]\s*|\n\s*|\band\s+)(?:please\s+)?(see|read|check|open|consult|refer to|look at|visit|go to)\s+(?:[\w'’-]+\s+){0,3}?(?:(guide|document|documentation|docs|section|chapter|glossary|manual|help|faq|[\w-]+\.md)\b|§)`,
  String.raw`\byou (can|could|may|might|will)(?: also)? (find|see|read|check|open|consult|look at|look up)\s+(?:[\w'’-]+\s+){0,3}?(?:(guide|document|documentation|docs|section|chapter|glossary|manual|help|faq|[\w-]+\.md)\b|§)`,
];

/** Denials of a promise ("not a guaranteed price"), blanked before the forecast patterns run. */
export const NEGATION_PATTERNS = [
  String.raw`\b(not|never|no|isn't|aren't) (a )?guarantee[ds]?\b`,
  String.raw`\b(do|does|did|can|could|will) ?(not|n't) guarantee\b`,
];
