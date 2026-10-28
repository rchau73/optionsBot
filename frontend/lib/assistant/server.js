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
import { JOURNAL_EVENTS, explainPassages, journalPassages, pnlPassages } from "./service";

const KEY = Symbol.for("optionsbot.monitor.assistant");
const JOURNAL_TIMEOUT_MS = 2000;

function shared() {
  globalThis[KEY] ??= { index: null, limiter: new QuestionLimiter(), model: undefined };
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

/** Tests: a fake model (undefined = back to the real client), and a fresh limiter. */
export function setAssistantModel(model) {
  const s = shared();
  s.model = model;
  s.limiter = new QuestionLimiter();
}

/**
 * The latest journal facts of a configured bot as passages; [] when the bot
 * is unreachable (the question is still answered from the help, WARN logged).
 * null for a bot that is not configured.
 */
export async function botJournal(bot) {
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return null;
  try {
    const res = await fetch(`${target.url}/api/events?limit=${JOURNAL_EVENTS}`, {
      cache: "no-store",
      signal: AbortSignal.timeout(JOURNAL_TIMEOUT_MS),
    });
    if (!res.ok) throw new Error(`status ${res.status}`);
    return journalPassages(bot, (await res.json()).events);
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
