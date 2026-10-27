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
import { JOURNAL_EVENTS, journalPassages } from "./service";

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
