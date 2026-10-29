// The notification bell (#447, E13; prototype approved on #164): what needs
// the person, counted on the bell's badge, and the updates — bot events that
// change the book, the status of the person's requests, what's new per
// release — listed but never counted. Pure: the server (lib/notificationsServer.js)
// builds the list with it, the bell and the /notifications page show it.
// In-app only: no e-mail, no push, nothing promotional.

import { formatDateTime } from "./format";

const DAY_MS = 24 * 3600 * 1000;

/** Items are kept this long; older ones are not listed (and their read marks are pruned). */
export const RETENTION_DAYS = 90;

/** The two groups, in the order the list shows them. Only "needs_you" is counted. */
export const GROUPS = [
  ["needs_you", "Needs you"],
  ["updates", "Updates"],
];

/**
 * Every kind of item → its group and its setting's label. A kind of
 * "needs_you" cannot be turned off (the survey has its own "Don't ask me
 * again"); each "updates" kind can.
 */
export const KINDS = {
  survey: { group: "needs_you", label: "A survey is waiting" },
  request_needs_info: { group: "needs_you", label: "Your request needs more" },
  bot_event: { group: "updates", label: "What the bot did to your book" },
  request_status: { group: "updates", label: "The status of your requests" },
  whats_new: { group: "updates", label: "What's new in a release" },
};

/** The kinds a person may turn off, in the settings' order. */
export const OPTIONAL_KINDS = Object.keys(KINDS).filter((k) => KINDS[k].group === "updates");

/** The most read marks one request may set (a page shows fewer items). */
export const MAX_READ_IDS = 200;

/** An item id as the server writes them: "<kind prefix>:<letters, digits, . _ : ->", at most 128 characters. */
export const ITEM_ID = /^[a-z_]+:[A-Za-z0-9._:-]{1,120}$/;

// The journal events that become bot updates (never fills or skips): a
// close by stop-loss, delta exit or the kill switch, a working sell
// cancelled because new risk is frozen, and IM over the limit.
const CLOSE_TITLES = { stop_loss: "Stop-loss", delta_exit: "Delta exit", kill_switch: "Kill switch" };

/** The title of a journal event that is a bot update, or null for any other event. */
export function botEventTitle(ev) {
  const d = ev?.data ?? {};
  if (ev?.event === "closed" && Object.hasOwn(CLOSE_TITLES, d.close_reason)) return CLOSE_TITLES[d.close_reason];
  if (ev?.event === "cancelled" && d.trigger_reason === "risk_frozen") return "New risk frozen";
  if (ev?.event === "risk_limit" && d.change === "im_over_limit") return "Margin over the limit";
  return null;
}

/** The question Why? asks about a bot update that is not a close. */
function whyQuestionOf(bot, title, at) {
  return `Why did ${String(bot).toUpperCase()} record "${title}" on ${formatDateTime(at)}?`;
}

/**
 * A journal event (/api/events) as an item of the page's bot, or null when it
 * is not a bot update. Its text is the event's plain sentence; why carries
 * what the assistant's Why? needs (/api/explain by sequence number).
 */
export function botEventItem(bot, ev) {
  const title = botEventTitle(ev);
  if (!title || !Number.isInteger(ev.seq) || typeof ev.at !== "string") return null;
  const d = ev.data ?? {};
  const close = ev.event === "closed";
  return {
    id: `bot:${bot}:${ev.seq}`,
    kind: "bot_event",
    ts: ev.at,
    title: `${String(bot).toUpperCase()} · ${title}`,
    text: typeof ev.plain === "string" && ev.plain.trim() ? ev.plain : title,
    why: {
      bot,
      seq: ev.seq,
      kind: "close",
      instrument: d.instrument ?? null,
      at: ev.at,
      // A close asks the assistant's usual question; another event names itself.
      question: close ? null : whyQuestionOf(bot, title, ev.at),
    },
  };
}

/** The public issue number of an issue link ("…/issues/123" → 123), or null. */
export function issueNumber(url) {
  const m = /\/issues\/(\d+)(?:[/?#]|$)/.exec(url ?? "");
  return m ? Number(m[1]) : null;
}

const DECLINED = { not_planned: "not planned", not_a_bug: "not a bug", duplicate: "a duplicate" };

/**
 * One of the person's requests (My requests: { id, status, status_ts, note,
 * issue_url, title }) as an item, or null while it is in review: "needs
 * more" needs the person; accepted as public #N, declined with its reason
 * and shipped are updates.
 */
export function requestItem(r) {
  if (!r?.id || !r.status_ts || r.status === "in_review") return null;
  const base = { id: `request:${r.id}:${r.status}:${Date.parse(r.status_ts) || 0}`, ts: r.status_ts, request: r.id };
  const note = r.note ? ` ${r.note}` : "";
  if (r.status === "needs_info") {
    return { ...base, kind: "request_needs_info", title: `Your request ${r.id} needs more`, text: `The reviewer asked for more about "${r.title}".${note}` };
  }
  if (r.status === "filed") {
    const n = issueNumber(r.issue_url);
    return { ...base, kind: "request_status", title: `Your request ${r.id} was accepted${n ? ` as public #${n}` : ""}`, text: `"${r.title}" is now a public issue.${note}`, href: r.issue_url ?? null };
  }
  if (Object.hasOwn(DECLINED, r.status)) {
    return { ...base, kind: "request_status", title: `Your request ${r.id} was declined: ${DECLINED[r.status]}`, text: `"${r.title}".${note}` };
  }
  if (r.status === "fixed") return { ...base, kind: "request_status", title: `Your request ${r.id} has shipped`, text: `"${r.title}" is in the monitor now.${note}` };
  return null;
}

/**
 * The survey item while the monthly NPS card is due (#446's rule); one per
 * due period (keyed by the day it was last shown), so reading it once does
 * not hide next month's.
 */
export function surveyItem(lastShownSec, nowMs) {
  return {
    id: `survey:nps:${lastShownSec ?? "never"}`,
    kind: "survey",
    ts: new Date(nowMs).toISOString(),
    title: "A short question from the team is waiting",
    text: "One optional question in Ask. Answer it, or choose Don't ask me again.",
  };
}

/** A release note ({ tag, date, title, body }) as a What's new item. */
export function releaseItem(note) {
  return { id: `release:${note.tag}`, kind: "whats_new", ts: note.date, title: `What's new in ${note.tag}: ${note.title}`, text: note.body };
}

/**
 * The person's list: the items of the kinds they keep (a Needs you kind is
 * always kept), from the last RETENTION_DAYS, newest first, each with read
 * (from their read marks) and group. needs_you = the unread Needs you items,
 * the bell's badge; updates are never counted.
 * → { items, needs_you }
 */
export function buildNotifications(items, { readIds = new Set(), settings = {} } = {}, nowMs) {
  const since = nowMs - RETENTION_DAYS * DAY_MS;
  const kept = items
    .filter(Boolean)
    .filter((i) => Object.hasOwn(KINDS, i.kind))
    .filter((i) => KINDS[i.kind].group === "needs_you" || settings[i.kind] !== false)
    .filter((i) => {
      const t = Date.parse(i.ts);
      return Number.isFinite(t) && t >= since;
    })
    .map((i) => ({ ...i, group: KINDS[i.kind].group, read: readIds.has(i.id) }))
    .sort((a, b) => Date.parse(b.ts) - Date.parse(a.ts) || (a.id < b.id ? 1 : -1));
  return { items: kept, needs_you: kept.filter((i) => i.group === "needs_you" && !i.read).length };
}

/** The settings of a person's kinds: every optional kind → on (true) unless they turned it off. */
export function settingsOf(stored = {}) {
  return Object.fromEntries(OPTIONAL_KINDS.map((k) => [k, stored[k] !== false]));
}

/** The bell's accessible name: "Notifications, 2 need you" ("1 needs you"; just "Notifications" when none). */
export function bellLabel(count) {
  if (!(count > 0)) return "Notifications";
  return `Notifications, ${count} ${count === 1 ? "needs" : "need"} you`;
}

/** What the badge shows: the count, "9+" above 9, nothing at 0. */
export function badgeText(count) {
  if (!(count > 0)) return null;
  return count > 9 ? "9+" : String(count);
}
