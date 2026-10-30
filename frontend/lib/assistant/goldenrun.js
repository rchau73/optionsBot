// The live golden-set run (#536): every question of
// docs/product/assistant/golden.jsonl through the real assistant
// (answerQuestion, the model the monitor uses), each answer written as one
// line of the answers.jsonl that scripts/product/assistant_grade.py reads.
//
// A test run, never a person's question:
// - it does not go through POST /api/assistant, so the per-person limit
//   (QUESTIONS_PER_HOUR) neither counts nor stops it; it paces itself instead
//   (PAUSE_MS between questions) and asks each question at most MAX_TRIES times;
// - its review log is a file of its own, never data/monitor/assistant.jsonl,
//   and every line carries `test_run: true` (cmd/assistantreview skips those);
// - it sends no analytics event and never writes the request queue
//   (answerQuestion queues nothing: only a person's Send does).
// The bot data a question needs comes from the question's own fixture
// (`bot`, `request`): invented figures, never a live account's.
import path from "node:path";
import { answerQuestion, explainPassages, journalPassages, livePassages, mergePassages } from "./service";

// The fixtures name one bot (their ids read "live:btc/…", "journal:btc/…").
export const GOLDEN_BOT = "btc";
// Between two questions: a run of the whole set (≈ 115 questions) takes a few
// minutes and never bursts the model API.
export const PAUSE_MS = 2000;
// A question whose model call failed is asked once more, then recorded as a model error.
export const MAX_TRIES = 2;
// Who asks, in the run's own log: no person.
export const TEST_RUN_USER = { username: "golden-run", role: "test_run" };
// The people's review log and the private request queue: the run writes neither.
const PROTECTED_FILES = ["assistant.jsonl", "requests.jsonl", "requests_status.jsonl"];

/** Why a file cannot be the run's log, or null when it can. */
export function runLogError(file) {
  if (!file) return "a log file for the run is required (--log)";
  if (PROTECTED_FILES.includes(path.basename(file))) {
    return `${path.basename(file)} is the monitor's own file: the run writes its own log (for example data/monitor/assistant_runs/<time>/run.jsonl)`;
  }
  return null;
}

/** A request fixture's journal passages as /api/journal items (the route reads them from there). */
function journalItems(passages) {
  return passages
    .filter((p) => p.source.startsWith("journal:"))
    .map((p) => {
      const [, rest = ""] = p.title.split(": ");
      const [event, at] = rest.split(" at ");
      return { seq: Number(p.id.split("/")[1]), event, at, plain: p.text };
    });
}

/**
 * What the monitor would hand answerQuestion for one golden question, built
 * from its fixture: `bot` (#427: the bot's routes' answers → live and journal
 * passages, plus /api/explain's facts for a Why?, #534) or `request` (#409:
 * journal facts and P&L history points); none → a question with no bot.
 */
export function goldenInputs(q) {
  if (q.bot) {
    const b = q.bot;
    let journal = journalPassages(GOLDEN_BOT, b.journal?.items, b.journal?.held);
    if (b.explain) journal = mergePassages(explainPassages(GOLDEN_BOT, b.explain), journal);
    return { bot: GOLDEN_BOT, live: livePassages(GOLDEN_BOT, b), journal, reportPassages: async () => [] };
  }
  const passages = q.request?.passages ?? [];
  if (passages.length) {
    const pnl = passages.filter((p) => p.source.startsWith("pnl:"));
    return { bot: GOLDEN_BOT, live: [], journal: journalPassages(GOLDEN_BOT, journalItems(passages)), reportPassages: async () => pnl };
  }
  return { bot: null, live: [], journal: [], reportPassages: async () => [] };
}

/** One answers.jsonl line, as assistant_grade.py reads it: { id, text, citations, outcome }. */
export function answerLine(id, { status, body }) {
  if (status !== 200) return { id, text: "", citations: [], outcome: "model_error" };
  return { id, text: body.answer, citations: body.citations.map((c) => c.id), outcome: body.outcome };
}

/**
 * Asks every golden question in turn and returns { answers, modelErrors }:
 * one answer per question, in the set's order — a question the model failed
 * twice is still a line (outcome model_error, which the grader fails), so
 * none is lost. Throws when the run's log cannot be written (nothing is
 * answered unlogged). sleep(ms) and onAnswer(line, n, total) are for the CLI
 * and the tests.
 */
export async function runGolden({ golden, model, index, file, pauseMs = PAUSE_MS, sleep, onAnswer = () => {} }) {
  const err = runLogError(file);
  if (err) throw new Error(err);
  const answers = [];
  let modelErrors = 0;
  for (const [n, q] of golden.entries()) {
    if (n > 0) await sleep(pauseMs);
    const inputs = goldenInputs(q);
    let out;
    for (let tries = 1; tries <= MAX_TRIES; tries++) {
      if (tries > 1) await sleep(pauseMs);
      out = await answerQuestion({ question: q.question, ...inputs, user: TEST_RUN_USER, model, index, file, testRun: true });
      if (out.status === 500) throw new Error(`${q.id}: ${out.body.error}`);
      if (out.status === 200) break;
    }
    if (out.status !== 200) modelErrors++;
    const line = answerLine(q.id, out);
    answers.push(line);
    onAnswer(line, n + 1, golden.length);
  }
  return { answers, modelErrors };
}
