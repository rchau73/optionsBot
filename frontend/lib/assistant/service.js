// Server-side only: one question through the assistant (#191). The help
// passages that match it and, when a bot is named, that bot's latest journal
// facts go to the model; its answer passes the guard before anyone sees it,
// and the question, the sources, the model's text and what was shown are
// logged for review. An answer with no citation becomes "I don't know"; any
// other rejected answer becomes the fallback reply.
import { REASON_NO_CITATION, check, isCanonical } from "./guard";
import { appendLine, newAnswerId } from "./log";
import { AI_NOTICE, DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "./phrases";
import { noAnswer } from "./search";

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
 * citations} }; index: a HelpIndex; journal: passages from journalPassages;
 * file: the review log. → { status, body } for the route: 200 with { id,
 * answer, citations, outcome, ai_generated, notice }, 502 when the model
 * failed, 500 when the log could not be written (nothing is shown unlogged).
 */
export async function answerQuestion({ question, bot = null, journal = [], user, model, index, file, nowMs = Date.now() }) {
  const hits = index.search(question, HELP_PASSAGES);
  const passages = [...hits.map((h) => h.passage), ...journal];
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
  if (noAnswer(hits) && journal.length === 0) {
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
