// Server-side only: the assistant's shared pieces for its routes (#191) — the
// help index (built once from content/help-index.json), the rate limiter and
// the model client. Kept on globalThis so dev-mode reloads reuse them; tests
// swap the model with setAssistantModel.
import helpIndex from "@/content/help-index.json";
import { authLog } from "@/lib/auth/log";
import { configuredBots } from "@/lib/bots";
import { QuestionLimiter } from "./limit";
import { anthropicClient } from "./model";
import { HelpIndex } from "./search";
import { JOURNAL_CACHE_MS, JOURNAL_DAYS, LIVE_CACHE_MS, explainPassages, journalPassages, livePassages, pnlPassages } from "./service";

const KEY = Symbol.for("optionsbot.monitor.assistant");
// A week of orders.log is read back by the bot: allow it more than a ring read.
const JOURNAL_TIMEOUT_MS = 5000;
// The live routes are built from memory: a slow answer means the bot is in trouble.
const LIVE_TIMEOUT_MS = 3000;

function shared() {
  globalThis[KEY] ??= { index: null, limiter: new QuestionLimiter(), model: undefined, journals: new Map(), lives: new Map() };
  return globalThis[KEY];
}

export function assistantIndex() {
  const s = shared();
  s.index ??= new HelpIndex(helpIndex.chunks);
  return s.index;
}

export function assistantLimiter() {
  return shared().limiter;
}

/** The model client, or null when ANTHROPIC_API_KEY is not set. */
export function assistantModel() {
  const s = shared();
  return s.model === undefined ? anthropicClient() : s.model;
}

/** Tests: a fake model (undefined = back to the real client), a fresh limiter and no cached journal or live data. */
export function setAssistantModel(model) {
  const s = shared();
  s.model = model;
  s.limiter = new QuestionLimiter();
  s.journals = new Map();
  s.lives = new Map();
}

/** GET <bot>/<path> as JSON; throws on a failed read or a non-2xx answer. */
async function readBot(target, path) {
  const res = await fetch(`${target.url}${path}`, { cache: "no-store", signal: AbortSignal.timeout(LIVE_TIMEOUT_MS) });
  if (!res.ok) throw new Error(`${path}: status ${res.status}`);
  return res.json();
}

/**
 * A configured bot's live page data (#427) as passages (livePassages): its
 * /api/status, /api/positions and /api/strangles/payoff, read together and
 * kept LIVE_CACHE_MS per bot. Without the payoff the exit lines read
 * "unknown"; without status or positions the bot is down → [] with a WARN
 * (not cached: the help and the journal still answer). null for a bot that is
 * not configured.
 */
export async function botLive(bot, nowMs = Date.now()) {
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return null;
  const s = shared();
  const cached = s.lives.get(bot);
  if (cached && nowMs - cached.at < LIVE_CACHE_MS) return cached.passages;
  try {
    const [status, positions, payoff] = await Promise.all([
      readBot(target, "/api/status"),
      readBot(target, "/api/positions"),
      readBot(target, "/api/strangles/payoff").catch((err) => {
        authLog("WARN", "assistant: bot payoff unavailable", { component: "assistant", bot, error: err.message });
        return null;
      }),
    ]);
    const passages = livePassages(bot, { status, positions, payoff });
    s.lives.set(bot, { at: nowMs, passages });
    return passages;
  } catch (err) {
    authLog("WARN", "assistant: bot live data unavailable", { component: "assistant", bot, error: err.message });
    return [];
  }
}

/**
 * A configured bot's journal of the last JOURNAL_DAYS and the whole history
 * of the instruments it holds (/api/journal?held=1, #417, #427) as
 * passages, kept JOURNAL_CACHE_MS per bot (one read a minute, not one per
 * question); [] when the bot is unreachable (the question is still answered
 * from the help, WARN logged; not cached, so the next question tries again).
 * null for a bot that is not configured.
 */
export async function botJournal(bot, nowMs = Date.now()) {
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return null;
  const s = shared();
  const cached = s.journals.get(bot);
  if (cached && nowMs - cached.at < JOURNAL_CACHE_MS) return cached.passages;
  try {
    const res = await fetch(`${target.url}/api/journal?days=${JOURNAL_DAYS}&held=1`, {
      cache: "no-store",
      signal: AbortSignal.timeout(JOURNAL_TIMEOUT_MS),
    });
    if (!res.ok) throw new Error(`status ${res.status}`);
    const body = await res.json();
    const passages = journalPassages(bot, body.items, body.held);
    s.journals.set(bot, { at: nowMs, passages });
    return passages;
  } catch (err) {
    authLog("WARN", "assistant: bot journal unavailable", { component: "assistant", bot, error: err.message });
    return [];
  }
}

/**
 * A configured bot's P&L history of the last day (GET /api/pnl/history) as
 * passages for a report's request check (#409); [] when the bot is
 * unreachable (the check runs on the help and the journal, WARN logged).
 */
export async function botPnl(bot) {
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return [];
  try {
    const res = await fetch(`${target.url}/api/pnl/history?range=1d`, {
      cache: "no-store",
      signal: AbortSignal.timeout(JOURNAL_TIMEOUT_MS),
    });
    if (!res.ok) throw new Error(`status ${res.status}`);
    return pnlPassages(bot, (await res.json()).points);
  } catch (err) {
    authLog("WARN", "assistant: bot P&L history unavailable", { component: "assistant", bot, error: err.message });
    return [];
  }
}

/**
 * The facts the journal recorded about one event of a configured bot
 * (GET /api/explain?seq=, #314) as passages — the panel's "Why?" on a trade.
 * [] when the bot is unreachable or the event left its recent history (the
 * question is still answered from the latest journal and the help).
 */
export async function botExplain(bot, seq) {
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return [];
  try {
    const res = await fetch(`${target.url}/api/explain?seq=${seq}`, {
      cache: "no-store",
      signal: AbortSignal.timeout(JOURNAL_TIMEOUT_MS),
    });
    if (!res.ok) throw new Error(`status ${res.status}`);
    return explainPassages(bot, await res.json());
  } catch (err) {
    authLog("WARN", "assistant: event facts unavailable", { component: "assistant", bot, seq, error: err.message });
    return [];
  }
}
