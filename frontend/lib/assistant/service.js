// Server-side only: one question through the assistant (#191). The help
// passages that match it and, when a bot is named, that bot's latest journal
// facts go to the model; its answer passes the guard before anyone sees it,
// and the question, the sources, the model's text and what was shown are
// logged for review. An answer with no citation becomes "I don't know"; any
// other rejected answer becomes the fallback reply.
import { REASON_NO_CITATION, check, isCanonical } from "./guard";
import { appendLine, newAnswerId } from "./log";
import { AI_NOTICE, DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "./phrases";
import { REQUEST_ANSWERED, REQUEST_DRAFT, REQUEST_NONE, checkRequest, emptyChecklist, looksLikeReport } from "./request";
import { noAnswer } from "./search";
import { DRAFT_TEXT, NEEDS_INFO_TEXT } from "@/lib/assistantRequests";

export const MAX_QUESTION = 500;
// Passages of the help sent with a question: enough for an answer, few enough to keep the call small.
export const HELP_PASSAGES = 6;
// Journal facts sent with a question about a bot.
export const JOURNAL_EVENTS = 20;

// Outcomes, logged and returned.
export const OUTCOME_ANSWERED = "answered";
export const OUTCOME_DONT_KNOW = "dont_know"; // the model said it, or no passage matched (no model call)
export const OUTCOME_REFUSED = "refused";
export const OUTCOME_NO_CITATION = "no_citation"; // replaced by "I don't know"
export const OUTCOME_REJECTED = "rejected"; // replaced by the fallback reply
export const OUTCOME_MODEL_ERROR = "model_error";
// The request check's outcomes (#409); its "answered" is OUTCOME_ANSWERED with a checklist.
export const OUTCOME_NEEDS_INFO = "needs_info";
export const OUTCOME_DRAFT = "draft";

/** The question of a request body, or an error naming what is wrong. */
export function validQuestion(body) {
  const q = typeof body?.question === "string" ? body.question.trim() : "";
  if (!q) return { error: 'body must be {"question": "…", "bot": "btc"?}' };
  if (q.length > MAX_QUESTION) return { error: `question longer than ${MAX_QUESTION} characters` };
  return { question: q };
}

/**
 * A bot's journal events (/api/events, newest last) as citable passages: id
 * "journal:<bot>/<seq>", the plain-words sentence and its time — the facts
 * the journal recorded, nothing recomputed.
 */
export function journalPassages(bot, events) {
  return (events ?? [])
    .filter((e) => e && typeof e.plain === "string" && e.plain && e.seq != null)
    .slice(-JOURNAL_EVENTS)
    .map((e) => ({
      id: `journal:${bot}/${e.seq}`,
      source: `journal:${bot}`,
      title: `${bot} journal: ${e.event} at ${e.at}`,
      text: e.plain,
    }));
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
 * journalPassages. [] for no explanation.
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
  const own = { id: `journal:${bot}/${e.seq}`, source: `journal:${bot}`, title: `${bot} journal: ${e.event} at ${e.at}`, text };
  return [own, ...journalPassages(bot, explanation.related)];
}

// P&L history points sent with a report about a bot (#409).
export const PNL_POINTS = 24;

const sig = (v) => (Number.isFinite(v) ? Number(v.toPrecision(6)) : null);

/**
 * A bot's P&L history (/api/pnl/history points, oldest first) as citable
 * passages for the request check (#409): id "pnl:<bot>/<time>", the
 * recorded totals in words. At most `max`: the two points around the
 * largest step of the total (a spike), then points evenly spaced, oldest first.
 */
export function pnlPassages(bot, points, max = PNL_POINTS) {
  const list = (points ?? []).filter((p) => p && typeof p.t === "string" && Number.isFinite(p.total));
  if (!list.length) return [];
  const keep = new Set();
  let jump = 0;
  let size = -1;
  for (let i = 1; i < list.length; i++) {
    const d = Math.abs(list[i].total - list[i - 1].total);
    if (d > size) [jump, size] = [i, d];
  }
  if (jump > 0) keep.add(jump - 1).add(jump);
  const step = Math.max(1, Math.ceil(list.length / Math.max(1, max - keep.size)));
  for (let i = list.length - 1; i >= 0 && keep.size < max; i -= step) keep.add(i);
  return [...keep]
    .sort((a, b) => a - b)
    .map((i) => {
      const p = list[i];
      const parts = [`total ${sig(p.total)}`, `realised ${sig(p.realised)}`, `unrealised ${sig(p.unrealised)}`];
      if (Number.isFinite(p.total_usd)) parts.push(`total USD ${Math.round(p.total_usd * 100) / 100}`);
      if (Number.isFinite(p.spot)) parts.push(`spot ${sig(p.spot)}`);
      const at = `${p.t.slice(0, 10)} ${p.t.slice(11, 16)} UTC`;
      return { id: `pnl:${bot}/${p.t}`, source: `pnl:${bot}`, title: `${bot} P&L history at ${p.t}`, text: `P&L recorded on ${at}: ${parts.join(", ")}.` };
    });
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

/**
 * Fills the log entry from a request check's verdict (#409): resolves its
 * citations, then request.js decides the outcome. Not a report → the verdict's
 * answer as any other; answered → its answer through the guard, with the
 * checklist (and an enhancement draft when one may be offered); needs_info →
 * the checklist; draft → the card, citing its evidence. The draft is logged
 * with the answer (every AI output is reviewable), never queued here: only the
 * person's Send (POST /api/assistant/requests) puts it in the private queue.
 */
function applyRequestCheck(entry, verdict, passages) {
  const resolve = (c) => resolveCitation(c, passages);
  const v = {
    ...verdict,
    citations: verdict.citations.map(resolve),
    explained_by: verdict.explained_by.map(resolve),
    evidence: verdict.evidence.map((e) => ({ ...e, citation: e.citation ? resolve(e.citation) : "" })),
  };
  const logged = { ...v };
  delete logged.stop;
  entry.model_answer = JSON.stringify(logged);
  const r = checkRequest(v, passages);

  if (r.outcome === REQUEST_NONE || r.outcome === REQUEST_ANSWERED) {
    const cites = [...new Set([...v.citations, ...r.explained_by])];
    const g = check({ text: v.text, citations: cites }, passages);
    entry.citations = cites;
    entry.verdict = [...g.details, ...r.details];
    [entry.outcome, entry.answer] = outcomeOf({ text: v.text }, g);
    if (r.outcome === REQUEST_ANSWERED && entry.outcome === OUTCOME_ANSWERED) entry.request = { checklist: r.checklist, draft: r.draft };
    return;
  }
  entry.verdict = r.details;
  entry.request = { checklist: r.checklist, draft: r.draft };
  if (r.outcome === REQUEST_DRAFT) {
    entry.outcome = OUTCOME_DRAFT;
    entry.answer = DRAFT_TEXT;
    entry.citations = [...new Set(r.draft.evidence.map((e) => e.citation))];
  } else {
    entry.outcome = OUTCOME_NEEDS_INFO;
    entry.answer = NEEDS_INFO_TEXT;
  }
}

/**
 * Answers one question. model: { model, ask({question, passages}) → {text,
 * citations, report}, checkRequest?({question, passages}) → a verdict };
 * index: a HelpIndex; journal: passages from journalPassages; reportPassages:
 * async () → more passages for a report (the bot's P&L history, #409); file:
 * the review log. A question that reads as a report (looksLikeReport, or the
 * model's report flag on its answer) runs the request check instead of the
 * plain answer. → { status, body } for the route: 200 with { id, answer,
 * citations, outcome, checklist?, draft?, ai_generated, notice }, 502 when
 * the model failed, 500 when the log could not be written (nothing is shown unlogged).
 */
export async function answerQuestion({ question, bot = null, journal = [], reportPassages = async () => [], user, model, index, file, nowMs = Date.now() }) {
  const hits = index.search(question, HELP_PASSAGES);
  let passages = [...hits.map((h) => h.passage), ...journal];
  const canCheck = typeof model.checkRequest === "function";
  let report = canCheck && looksLikeReport(question);
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
  let verdict = null;
  if (noAnswer(hits) && journal.length === 0) {
    // Nothing to answer from: no model call. A report gets the checklist
    // (nothing to check it against), any other question the contract's reply.
    entry.model = null;
    if (report) {
      entry.outcome = OUTCOME_NEEDS_INFO;
      entry.answer = NEEDS_INFO_TEXT;
      entry.request = { checklist: emptyChecklist(), draft: null };
    } else {
      entry.outcome = OUTCOME_DONT_KNOW;
      entry.answer = DONT_KNOW;
    }
  } else {
    try {
      if (!report) {
        reply = await model.ask({ question, passages });
        // The model's intent flag: a report the words did not show.
        if (reply.report && canCheck) [report, reply] = [true, null];
      }
      if (report) {
        passages = mergePassages(passages, await reportPassages());
        entry.sources = passages.map((p) => p.id);
        verdict = await model.checkRequest({ question, passages });
      }
    } catch (err) {
      entry.outcome = OUTCOME_MODEL_ERROR;
      entry.verdict = [err.message];
    }
    if (verdict) {
      applyRequestCheck(entry, verdict, passages);
    } else if (reply) {
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
  const shown = entry.outcome === OUTCOME_ANSWERED || entry.outcome === OUTCOME_DRAFT ? entry.citations : [];
  const citations = [...new Set(shown)].map((c) => {
    const p = passages.find((x) => x.id === c || x.source === c);
    return { id: c, source: p.source, title: p.title };
  });
  const body = { id: entry.id, answer: entry.answer, citations, outcome: entry.outcome, ai_generated: true, notice: AI_NOTICE };
  if (entry.request) {
    body.checklist = entry.request.checklist;
    if (entry.request.draft) body.draft = entry.request.draft;
  }
  return { status: 200, body };
}
