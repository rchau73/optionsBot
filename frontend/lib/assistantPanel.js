// The assistant panel (#408, E10): pure helpers — the questions it offers,
// the "Why?" question for a trade, where each cited source links to, the
// foot line under an answer and the rate-limit wording.
import glossary from "@/content/glossary.json";
import { DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "@/lib/assistant/phrases";
import { QUESTIONS_PER_HOUR } from "@/lib/assistant/limit";
import { clockOf } from "./activity";
import { formatDateTime } from "./format";

export { DONT_KNOW, FALLBACK_REPLY, QUESTIONS_PER_HOUR, REFUSAL };

/**
 * The longest question the route accepts: service.js's MAX_QUESTION (that
 * module reads files, so the browser does not import it; a test keeps the two equal).
 */
export const MAX_QUESTION = 500;

/** The repository the help's documents are read from (public). */
export const DOCS_BASE = "https://github.com/rchau73/optionsBot/blob/main";

/** The empty panel's example questions. */
export const EXAMPLES = ["What is a short strangle?", "When does the bot buy a leg back?", "How does sizing work?"];

/** The follow-ups offered under a refusal: a question, or a page. */
export const REFUSAL_FOLLOW_UPS = [
  { kind: "ask", label: "How does sizing work?" },
  { kind: "page", label: "Open Worst case", path: "/worst-case" },
];

export const OFF_TEXT = "The assistant is off in this environment (no API key)";
export const NOTICE = "Answers can be wrong: check the linked source. Not investment advice.";

// Foot lines under an assistant answer.
export const FOOT_SOURCES = "Answered from the cited sources";
export const FOOT_JOURNAL = "Facts from your journal";
export const FOOT_REFUSED = "Advice and forecasts are refused";

const q = (bot) => (bot ? `?bot=${encodeURIComponent(bot)}` : "");

/** "call" | "put" from an option name ("BTC-27NOV26-80000-C"), else null. */
export function legOf(instrument) {
  const m = /-([CP])$/.exec(instrument ?? "");
  return m ? (m[1] === "C" ? "call" : "put") : null;
}

/**
 * The question Why? asks about a trade or a journal entry: "Why did BTC close
 * the put BTC-27NOV26-60000-P on 27/10/2026, 10:00?". kind: open|close.
 */
export function whyQuestion({ bot, kind, instrument, at }) {
  const leg = legOf(instrument);
  const what = leg ? `the ${leg} ${instrument}` : instrument || "a leg";
  const verb = kind === "open" ? "open" : "close";
  return `Why did ${String(bot ?? "the bot").toUpperCase()} ${verb} ${what} on ${formatDateTime(at)}?`.slice(0, MAX_QUESTION);
}

/** The journal sequence number of a "journal:<bot>/<seq>" citation, else null. */
export function journalSeqOf(id) {
  const m = /^journal:[a-z0-9_-]+\/(\d+)$/i.exec(id ?? "");
  return m ? Number(m[1]) : null;
}

/** A document source ("OptionStrategy.md#anchor", "docs/parameters.md#…") on the repository. */
function docHref(source) {
  const [file, anchor] = source.split("#");
  return `${DOCS_BASE}/${file}${anchor ? `#${anchor}` : ""}`;
}

/**
 * Where a citation's chip links to: an OptionStrategy.md section or another
 * document (the parameters doc) on the repository, a glossary term's own
 * source section, a journal entry's trade in Details (pro), the plain-words
 * catalogue's entries on the Activity page. external: opens in a new tab.
 * null for a source it does not know (the chip is shown without a link).
 */
export function citationHref(citation, bot = null) {
  const source = citation?.source ?? "";
  const id = citation?.id ?? "";
  if (source.startsWith("journal:")) {
    const name = source.slice("journal:".length);
    const seq = journalSeqOf(id);
    // Details (pro) is its own page (#405); the trade's row carries id trade-<bot>-<seq>.
    const base = `/details${q(name)}`;
    return { href: seq ? `${base}#trade-${name}-${seq}` : `${base}#trades`, external: false };
  }
  if (source.startsWith("glossary:")) {
    const term = glossary[source.slice("glossary:".length)];
    return term?.source ? { href: docHref(term.source), external: true } : null;
  }
  if (source.startsWith("explain:")) return { href: `/activity${q(bot)}`, external: false };
  if (/^[\w./-]+\.md(#.*)?$/.test(source)) return { href: docHref(source), external: true };
  return null;
}

/** A chip's text: the passage title, else its source. */
export function citationLabel(citation) {
  return citation?.title || citation?.source || citation?.id || "source";
}

/** The foot line under an answer, by outcome and what it cites. */
export function footLine({ outcome, citations = [] }) {
  if (outcome === "refused" || outcome === "rejected") return FOOT_REFUSED;
  if (outcome === "answered" && citations.length && citations.every((c) => (c.source ?? "").startsWith("journal:"))) return FOOT_JOURNAL;
  return FOOT_SOURCES;
}

/** "n of 30 questions left this hour". */
export function remainingText(remaining) {
  return `${remaining} of ${QUESTIONS_PER_HOUR} questions left this hour`;
}

/** "30 questions an hour: next at 14:02" (display zone) after a 429 with Retry-After seconds. */
export function limitText(retryAfterSec, now = Date.now()) {
  const next = retryAfterSec ? clockOf(now + retryAfterSec * 1000) : null;
  return `${QUESTIONS_PER_HOUR} questions an hour: next at ${next ?? "the top of the hour"}`;
}
