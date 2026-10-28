// The Activity page (#385, E3 prototype screen 4): what the bot did, and why,
// day by day. Pure helpers: journal events (/api/events) and trades
// (/api/trades) become entries led by the bot's plain-words sentence (#201),
// sorted into the page's four filters and grouped by day in the display zone.
import { describeEvent } from "./monitor";
import { isNumber } from "./format";
import { DISPLAY_TZ, localDate } from "./time";

/** The filter buttons, in order: [key, label]. */
export const ACTIVITY_FILTERS = [
  ["all", "Everything"],
  ["money", "Money in or out"],
  ["protection", "Protection"],
  ["waiting", "Waiting"],
];

/** Days shown at first, and added by each "Show earlier days". */
export const DAYS_PER_PAGE = 3;

// Close reasons (the journal's close_reason labels) that protect the account.
const PROTECTION_CLOSES = new Set(["stop_loss", "delta_exit", "gamma_regime", "margin_mm_limit", "kill_switch", "manual_close"]);
// A working sell cancelled to protect the account (its trigger_reason).
const PROTECTION_CANCELS = new Set(["risk_frozen", "regime_side", "gamma_close", "kill_switch"]);
// Margin-policy changes that protect the account: new risk frozen, MM limit reached.
const PROTECTION_RISK = new Set(["frozen", "mm_breach"]);

// Order mechanics: a sell sent or re-priced. Its outcome (filled, cancelled)
// is shown; the steps stay under Details (pro), or they would flood the page.
const HIDDEN_EVENTS = new Set(["submitted", "amended", "pnl"]);

/** "bob (manager)" for a journal actor, null without one. */
export function actorLabel(actor) {
  if (!actor?.user) return null;
  return actor.role ? `${actor.user} (${actor.role})` : actor.user;
}

/**
 * The plain sentence split into a bold first line (what happened) and the
 * rest (why, what follows): at the first ": " or ". ", whichever comes first.
 */
export function splitPlain(plain) {
  if (!plain) return { headline: "", why: "" };
  const cuts = [plain.indexOf(": "), plain.indexOf(". ")].filter((i) => i > 0);
  if (!cuts.length) return { headline: plain.replace(/\.$/, ""), why: "" };
  const cut = Math.min(...cuts);
  const rest = plain.slice(cut + 2).trim();
  return { headline: plain.slice(0, cut), why: rest ? rest[0].toUpperCase() + rest.slice(1) : "" };
}

/** The word a skip reason starts with ("repair_held: top-up held" → "repair_held"). */
const skipWord = (reason) => (reason ?? "").split(/[:\s]/)[0];

/**
 * Which filters an entry belongs to besides Everything, from its journal
 * fields: { money, protection, waiting, person }. A value the mapping does
 * not know belongs to none (Everything only, never dropped).
 */
export function activityGroups({ event, closeReason, trigger, change, skipReason, actor }) {
  const person = Boolean(actor?.user) || closeReason === "manual_close" || change === "rebalance_manual";
  const money = event === "filled" || event === "closed";
  const protection =
    person ||
    (event === "closed" && PROTECTION_CLOSES.has(closeReason)) ||
    (event === "cancelled" && PROTECTION_CANCELS.has(trigger)) ||
    (event === "risk_limit" && PROTECTION_RISK.has(change));
  const waiting = event === "skipped" && Boolean(skipWord(skipReason));
  return { money, protection, waiting, person };
}

/** The round badge's icon: person, in (money in), out (bought back), shield, clock, or dot. */
function iconOf(event, groups) {
  if (groups.person) return "person";
  if (event === "filled") return "in";
  if (event === "closed") return "out";
  if (groups.protection) return "shield";
  if (groups.waiting) return "clock";
  return "dot";
}

/**
 * The entry's amount: "Paid to you" for an open, "Result" for a close (both
 * in coin, signed); value null when the journal has no figure (never a 0).
 */
function amountOf(event, { premium, pnl }) {
  if (event === "filled") return { label: "Paid to you", value: isNumber(premium) ? premium : null };
  if (event === "closed") return { label: "Result", value: isNumber(pnl) ? pnl : null };
  return null;
}

function entry({ seq, at, event, instrument, plain, fallback, fields, amount }) {
  const groups = activityGroups({ event, ...fields });
  const { headline, why } = splitPlain(plain || fallback || event);
  return {
    id: seq,
    seq,
    at,
    event,
    instrument: instrument ?? null, // for the assistant's "Why?" (#408)
    icon: iconOf(event, groups),
    headline,
    why,
    actor: actorLabel(fields.actor),
    amount: amountOf(event, amount),
    groups,
  };
}

/** A journal event (/api/events) as an Activity entry; null for order mechanics. */
export function activityFromEvent(ev, unit) {
  if (!ev || HIDDEN_EVENTS.has(ev.event)) return null;
  const d = ev.data ?? {};
  const plain = typeof ev.plain === "string" && ev.plain.trim() ? ev.plain : null;
  return entry({
    seq: ev.seq,
    at: ev.at,
    event: ev.event,
    instrument: d.instrument,
    plain,
    fallback: plain ? null : describeEvent(unit.toLowerCase(), ev, unit)?.text,
    fields: { closeReason: d.close_reason, trigger: d.trigger_reason, change: d.change, skipReason: d.skip_reason, actor: d.actor },
    // A close's pnl is omitted from the journal when it is exactly 0.
    amount: { premium: d.premium_received ?? (isNumber(d.fill_price) && isNumber(d.qty) ? d.fill_price * d.qty : null), pnl: d.pnl ?? 0 },
  });
}

/** A trade (/api/trades: an open or a close of the journal) as an Activity entry. */
export function activityFromTrade(t) {
  if (!t) return null;
  const event = t.kind === "open" ? "filled" : "closed";
  return entry({
    seq: t.seq,
    at: t.at,
    event,
    instrument: t.instrument,
    plain: typeof t.plain === "string" && t.plain.trim() ? t.plain : null,
    fallback: `${t.kind === "open" ? "Sold" : "Bought back"} ${t.qty ?? ""} ${t.instrument ?? ""} (${t.reason ?? "unknown reason"})`,
    fields: { closeReason: t.kind === "close" ? t.reason : undefined, trigger: t.reason, actor: t.actor },
    // A close's pnl is omitted from the journal when it is exactly 0.
    amount: { premium: t.premium, pnl: t.kind === "close" ? (t.pnl ?? 0) : null },
  });
}

/**
 * Every entry from the events and the trades, newest first. Both carry the
 * journal's sequence number: an open or close in both is shown once (the
 * event's copy); the trades add the opens and closes older than the events
 * the bot keeps in memory.
 */
export function buildActivity(events = [], trades = [], unit) {
  const bySeq = new Map();
  for (const t of trades) {
    const e = activityFromTrade(t);
    if (e) bySeq.set(e.seq, e);
  }
  for (const ev of events) {
    const e = activityFromEvent(ev, unit);
    if (e) bySeq.set(e.seq, e);
  }
  return [...bySeq.values()].sort((a, b) => Date.parse(b.at) - Date.parse(a.at) || b.seq - a.seq);
}

/** The entries a filter keeps: Everything keeps all. */
export function filterActivity(entries, filter) {
  if (filter === "all" || !filter) return entries;
  return entries.filter((e) => e.groups[filter]);
}

const dayTitle = new Intl.DateTimeFormat("en-GB", { timeZone: DISPLAY_TZ, weekday: "short", day: "numeric", month: "short", year: "numeric" });

/**
 * Entries in day groups, newest day first, in the display zone: "Today",
 * "Yesterday", then the date ("Sat, 24 Oct 2026"). Each: { day, title, entries }.
 */
export function groupByDay(entries, now = Date.now()) {
  const today = localDate(now);
  const yesterday = localDate(now - 86_400_000);
  const groups = [];
  for (const e of entries) {
    const day = localDate(e.at) ?? "unknown";
    let g = groups[groups.length - 1];
    if (!g || g.day !== day) {
      const title = day === today ? "Today" : day === yesterday ? "Yesterday" : day === "unknown" ? "Unknown date" : dayTitle.format(new Date(e.at));
      g = { day, title, entries: [] };
      groups.push(g);
    }
    g.entries.push(e);
  }
  return groups;
}

/** "14:02" in the display zone, or "—". */
export function clockOf(at) {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return "—";
  return new Intl.DateTimeFormat("en-GB", { timeZone: DISPLAY_TZ, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(d);
}
