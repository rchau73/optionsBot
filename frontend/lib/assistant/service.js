// Server-side only: one question through the assistant (#191). The help
// passages that match it and, when a bot is named, that bot's journal facts of
// the last JOURNAL_DAYS (#417: every open, close and fill, plus the other
// facts most relevant to the question, within a budget) go to the model; its answer passes the guard before anyone sees it,
// and the question, the sources, the model's text and what was shown are
// logged for review. An answer with no citation becomes "I don't know"; any
// other rejected answer becomes the fallback reply.
import { REASON_NO_CITATION, check, isCanonical } from "./guard";
import { appendLine, newAnswerId } from "./log";
import { AI_NOTICE, DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "./phrases";
import { HelpIndex, noAnswer } from "./search";

export const MAX_QUESTION = 500;
// Passages of the help sent with a question: enough for an answer, few enough to keep the call small.
export const HELP_PASSAGES = 6;
// The bot's journal read for a question about it: its last days (/api/journal,
// repeats collapsed by the bot), read once a minute per bot.
export const JOURNAL_DAYS = 7;
export const JOURNAL_CACHE_MS = 60_000;
// Journal facts sent with one question: at most this many passages and about
// this many tokens (≈ 4 characters each) — with the help, ≲ 8k input tokens.
export const JOURNAL_PASSAGES = 60;
export const JOURNAL_TOKENS = 6000;
// Events that change the book: every one of the window goes to the model.
const TRADE_EVENTS = new Set(["filled", "closed"]);
// A decision this close to a trade sent is sent with it, budget allowing.
const AROUND_MS = 30 * 60_000;

// Outcomes, logged and returned.
export const OUTCOME_ANSWERED = "answered";
export const OUTCOME_DONT_KNOW = "dont_know"; // the model said it, or no passage matched (no model call)
export const OUTCOME_REFUSED = "refused";
export const OUTCOME_NO_CITATION = "no_citation"; // replaced by "I don't know"
export const OUTCOME_REJECTED = "rejected"; // replaced by the fallback reply
export const OUTCOME_MODEL_ERROR = "model_error";

/** The question of a request body, or an error naming what is wrong. */
export function validQuestion(body) {
  const q = typeof body?.question === "string" ? body.question.trim() : "";
  if (!q) return { error: 'body must be {"question": "…", "bot": "btc"?}' };
  if (q.length > MAX_QUESTION) return { error: `question longer than ${MAX_QUESTION} characters` };
  return { question: q };
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n) => String(n).padStart(2, "0");

/** "20 Oct 08:00" (UTC) for an ISO time; the text as given when it is not one. */
export function shortTime(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso);
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}

/**
 * A bot's journal items (/api/journal, oldest first) as citable passages: id
 * "journal:<bot>/<seq>" (the last occurrence's seq), the plain-words sentence
 * and its time — the facts the journal recorded, nothing recomputed. A
 * collapsed item reads "×42, 20 Oct 08:00 → 21 Oct 14:30: <plain>". `trade`
 * marks an open, close or fill; `reason` is the rule word, for the search.
 */
export function journalPassages(bot, items) {
  return (items ?? [])
    .filter((e) => e && typeof e.plain === "string" && e.plain && e.seq != null)
    .map((e) => ({
      id: `journal:${bot}/${e.seq}`,
      source: `journal:${bot}`,
      title: `${bot} journal: ${e.event} at ${e.at}`,
      text: e.count > 1 ? `×${e.count}, ${shortTime(e.first)} → ${shortTime(e.last)}: ${e.plain}` : e.plain,
      trade: TRADE_EVENTS.has(e.event),
      reason: typeof e.reason === "string" ? e.reason : "",
      seq: Number(e.seq),
      atMs: Date.parse(e.at),
      firstMs: Date.parse(e.first ?? e.at),
    }));
}

/** About how many tokens a passage costs in the prompt (≈ 4 characters a token). */
export function passageTokens(p) {
  return Math.ceil((p.id.length + p.title.length + p.text.length + 4) / 4);
}

/**
 * The journal passages one question sends (#417): every open, close and fill
 * (newest first if they alone fill the budget), then the other facts the
 * question matches best — the help's BM25 over each one's sentence and rule
 * word — and the decisions within 30 min of a trade sent, while the budget
 * lasts. A pinned passage (the facts of the trade a "Why?" asks about,
 * explainPassages) always goes first, outside the budget. Oldest first, as
 * the journal reads.
 */
export function selectJournal(question, passages, { maxPassages = JOURNAL_PASSAGES, maxTokens = JOURNAL_TOKENS } = {}) {
  const picked = [];
  let tokens = 0;
  const take = (p) => {
    const cost = passageTokens(p);
    if (picked.length >= maxPassages || tokens + cost > maxTokens) return false;
    picked.push(p);
    tokens += cost;
    return true;
  };
  const pinned = (passages ?? []).filter((x) => x.pinned);
  for (const p of pinned) {
    picked.push(p);
    tokens += passageTokens(p);
  }
  const all = (passages ?? []).filter((x) => !x.pinned);
  for (const p of all.filter((x) => x.trade).sort((a, b) => b.seq - a.seq)) {
    if (!take(p)) break;
  }
  const others = all.filter((x) => !x.trade);
  if (others.length > 0) {
    const byId = new Map(others.map((p) => [p.id, p]));
    const index = new HelpIndex(others.map((p) => ({ ...p, text: `${p.text} ${(p.reason ?? "").replaceAll("_", " ")}` })));
    for (const hit of index.search(question, others.length)) {
      if (!take(byId.get(hit.passage.id))) break;
    }
    // Then the decisions around each trade taken (±30 min, as /api/explain's related).
    const tradeTimes = picked.filter((p) => p.trade).map((p) => p.atMs);
    const chosen = new Set(picked);
    for (const p of others) {
      if (chosen.has(p) || !tradeTimes.some((t) => p.firstMs - AROUND_MS <= t && t <= p.atMs + AROUND_MS)) continue;
      if (!take(p)) break;
    }
  }
  return picked.sort((a, b) => a.seq - b.seq);
}

/** "spot 67,000, DVOL 52.1, regime negative" — the market an explanation recorded, as words. */
function marketWords(m) {
  if (!m) return "";
  const parts = [
    ["spot", m.spot],
    ["DVOL", m.dvol],
    ["IV percentile", m.iv_percentile],
    ["GEX regime", m.gex_regime],
    ["gamma flip", m.gamma_flip],
    ["flip buffer %", m.flip_buffer_pct],
    ["trend", m.trend],
    ["delta", m.delta],
    ["bid", m.bid],
    ["ask", m.ask],
    ["mid", m.mid],
  ].filter(([, v]) => v !== undefined && v !== null && v !== "" && v !== 0);
  return parts.map(([k, v]) => `${k} ${v}`).join(", ");
}

/**
 * The facts of one journal event (/api/explain, #314) as citable passages:
 * the event itself — its plain sentence, rule, detail and recorded market —
 * then its related events, each with the same "journal:<bot>/<seq>" id as
 * journalPassages, all pinned. [] for no explanation.
 */
export function explainPassages(bot, explanation) {
  const e = explanation?.event;
  if (!e || e.seq == null) return [];
  const market = marketWords(explanation.market);
  const text = [
    e.plain,
    explanation.rule ? `Rule: ${explanation.rule}.` : "",
    explanation.detail ? `Detail: ${explanation.detail}.` : "",
    market ? `Market then: ${market}.` : "",
  ]
    .filter(Boolean)
    .join(" ");
  const own = {
    id: `journal:${bot}/${e.seq}`,
    source: `journal:${bot}`,
    title: `${bot} journal: ${e.event} at ${e.at}`,
    text,
    trade: TRADE_EVENTS.has(e.event),
    reason: typeof explanation.rule === "string" ? explanation.rule : "",
    seq: Number(e.seq),
    atMs: Date.parse(e.at),
    firstMs: Date.parse(e.at),
  };
  // Pinned (#417): selectJournal always sends the facts of the trade asked about.
  return [own, ...journalPassages(bot, explanation.related)].map((p) => ({ ...p, pinned: true }));
}

/** The passages of both lists, each id once: the first list's copy wins. */
export function mergePassages(first, second) {
  const ids = new Set(first.map((p) => p.id));
  return [...first, ...second.filter((p) => !ids.has(p.id))];
}

function outcomeOf(reply, verdict) {
  if (verdict.ok) {
    const t = reply.text.trim().split(/\s+/).join(" ");
    if (t === DONT_KNOW) return [OUTCOME_DONT_KNOW, DONT_KNOW];
    if (t === REFUSAL) return [OUTCOME_REFUSED, REFUSAL];
    if (isCanonical(t)) return [OUTCOME_REJECTED, FALLBACK_REPLY];
    return [OUTCOME_ANSWERED, reply.text.trim()];
  }
  if (verdict.reasons.includes(REASON_NO_CITATION)) return [OUTCOME_NO_CITATION, DONT_KNOW];
  return [OUTCOME_REJECTED, FALLBACK_REPLY];
}

/**
 * Answers one question. model: { model, ask({question, passages}) → {text,
 * citations} }; index: a HelpIndex; journal: the bot's week of passages from
 * journalPassages (selectJournal picks what this question sends);
 * file: the review log. → { status, body } for the route: 200 with { id,
 * answer, citations, outcome, ai_generated, notice }, 502 when the model
 * failed, 500 when the log could not be written (nothing is shown unlogged).
 */
/**
 * A citation as the passage id it can only mean: listed as is (an id or a
 * source), else the one listed id — or, failing that, the one source — that
 * ends with it after a ":": a model may drop the id's first part
 * ("closed:delta_exit/1" for "explain:closed:delta_exit/1", #412). Anything else stays, for the guard
 * to reject.
 */
export function resolveCitation(c, passages) {
  if (passages.some((p) => p.id === c || p.source === c)) return c;
  const ids = passages.filter((p) => p.id.endsWith(`:${c}`)).map((p) => p.id);
  if (ids.length === 1) return ids[0];
  const sources = [...new Set(passages.filter((p) => p.source?.endsWith(`:${c}`)).map((p) => p.source))];
  return ids.length === 0 && sources.length === 1 ? sources[0] : c;
}

export async function answerQuestion({ question, bot = null, journal = [], user, model, index, file, nowMs = Date.now() }) {
  const hits = index.search(question, HELP_PASSAGES);
  const facts = selectJournal(question, journal);
  const passages = [...hits.map((h) => h.passage), ...facts];
  const entry = {
    kind: "answer",
    id: newAnswerId(),
    ts: new Date(nowMs).toISOString(),
    user: user.username,
    role: user.role,
    bot,
    question,
    sources: passages.map((p) => p.id),
    model: model.model ?? null,
    model_answer: null,
    citations: [],
    verdict: [],
    outcome: null,
    answer: null,
    flag: false,
  };

  let reply = null;
  if (noAnswer(hits) && facts.length === 0) {
    // Nothing to answer from: no model call, the contract's reply.
    entry.model = null;
    entry.outcome = OUTCOME_DONT_KNOW;
    entry.answer = DONT_KNOW;
  } else {
    try {
      reply = await model.ask({ question, passages });
    } catch (err) {
      entry.outcome = OUTCOME_MODEL_ERROR;
      entry.verdict = [err.message];
    }
    if (reply) {
      reply.citations = reply.citations.map((c) => resolveCitation(c, passages));
      const verdict = check(reply, passages);
      entry.model_answer = reply.text;
      entry.citations = reply.citations;
      entry.verdict = verdict.details;
      [entry.outcome, entry.answer] = outcomeOf(reply, verdict);
    }
  }

  if (!appendLine(file, entry)) {
    return { status: 500, body: { error: "the answer could not be logged, so it is not shown: see the monitor's logs" } };
  }
  if (entry.outcome === OUTCOME_MODEL_ERROR) {
    return { status: 502, body: { id: entry.id, error: "the assistant did not answer: try again later" } };
  }
  const shown = entry.outcome === OUTCOME_ANSWERED ? entry.citations : [];
  const citations = [...new Set(shown)].map((c) => {
    const p = passages.find((x) => x.id === c || x.source === c);
    return { id: c, source: p.source, title: p.title };
  });
  return {
    status: 200,
    body: { id: entry.id, answer: entry.answer, citations, outcome: entry.outcome, ai_generated: true, notice: AI_NOTICE },
  };
}
