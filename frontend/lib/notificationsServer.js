// Server-side only: the notification bell's list for one signed-in person
// and the page's bot (#447). Reads what already exists — the NPS due rule on
// the user's row (#446), the person's requests and their review status
// (#409), the bot's recent journal events (/api/events, with their plain
// sentence) and the release notes — and the person's read marks and
// settings in auth.db. Stores no item: the list is rebuilt on every read.
import { authDbPath } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { losingWeek, npsNotDueReason } from "@/lib/analytics/nps";
import { myRequests, readLines, requestsPath, statusPath } from "@/lib/assistant/requests";
import { configuredBots } from "@/lib/bots";
import { botEventItem, buildNotifications, releaseItem, requestItem, settingsOf, surveyItem } from "@/lib/notifications";
import { readReleaseNotes } from "@/lib/releases";

const TIMEOUT_MS = 2000;
/** The bot keeps its last 500 journal events in memory (orders.RecentEvents); all are read. */
const EVENT_LIMIT = 500;

/** GET <bot url>/api/<endpoint> as JSON; throws on a failure or a non-2xx answer. */
async function botJson(bot, endpoint) {
  const res = await fetch(`${bot.url}/api/${endpoint}`, { cache: "no-store", signal: AbortSignal.timeout(TIMEOUT_MS) });
  if (!res.ok) throw new Error(`${res.status}`);
  return res.json();
}

/**
 * Whether the survey item is listed: #446's rule on the user's row, then the
 * losing-week rule on the sign of each bot's last 7 days (read here, never
 * stored) — the same two rules the Ask button's badge uses.
 */
async function surveyDue(state, nowMs, getJson) {
  if (!state || npsNotDueReason(state, nowMs) !== null) return false;
  const performances = await Promise.all(configuredBots().map((b) => getJson(b, "performance?range=7d").catch(() => null)));
  return !losingWeek(performances);
}

/** The page's bot's journal events → { items, error } (error: why they could not be read, the rest still listed). */
async function botItems(botName, getJson) {
  const bot = configuredBots().find((b) => b.name === botName);
  if (!bot) return { items: [], error: null };
  try {
    const { events } = await getJson(bot, `events?since=0&limit=${EVENT_LIMIT}`);
    return { items: (Array.isArray(events) ? events : []).map((ev) => botEventItem(bot.name, ev)), error: null };
  } catch (e) {
    authLog("WARN", "notifications: bot events unreadable", { component: "notifications", bot: bot.name, error: e.message });
    return { items: [], error: `bot ${bot.name} is unreachable` };
  }
}

/**
 * Every item of the person's list before their read marks and settings
 * apply → { items, errors }. bot: the page's bot (null: no bot updates).
 */
export async function collectItems({ store, user, bot = null, nowMs, getJson = botJson }) {
  const errors = {};
  const items = [];

  const state = store.npsState(user.id);
  if (await surveyDue(state, nowMs, getJson)) items.push(surveyItem(state.lastShownSec, nowMs));

  const file = requestsPath(authDbPath());
  if (file) items.push(...myRequests(readLines(file), readLines(statusPath(file)), user.username).map(requestItem));

  const events = await botItems(bot, getJson);
  items.push(...events.items);
  if (events.error) errors.bot_events = events.error;

  items.push(...readReleaseNotes().map(releaseItem));
  return { items: items.filter(Boolean), errors };
}

/**
 * The person's view: { items, needs_you, settings, errors } — read marks and
 * settings from auth.db, so every device of theirs shows the same.
 */
export async function notificationsView({ store, user, bot = null, nowMs, getJson = botJson }) {
  const { items, errors } = await collectItems({ store, user, bot, nowMs, getJson });
  const stored = store.notificationSettings(user.id);
  const view = buildNotifications(items, { readIds: store.notificationReads(user.id), settings: stored }, nowMs);
  return { ...view, settings: settingsOf(stored), errors };
}
