// "Since you last looked" (#618, epic E3): what changed for each bot between
// this browser's last visit and now — the closes with their reasons, their
// result net of fees, the futures hedge's lines (#679), the worst case then vs
// now and a warning that began meanwhile. Pure: every function takes `now` and
// the stored visit. The visit is kept only in this browser (localStorage);
// nothing here is sent anywhere.
import { formatAge, isNumber } from "./format";
import { hedgeResult, hedgeSentence, isHedgeRisk } from "./hedgeWords";
import { coinOf, worstCaseLine } from "./overview";
import { closeLabel } from "./trades";

/** localStorage key of one bot's last visit. */
export const visitKey = (bot) => `monitor.lastVisit.${bot}`;
/** localStorage key of the visit whose card was hidden ("until the next visit"). */
export const DISMISS_KEY = "monitor.lastVisit.dismissed";
/** A visit counts once the page has been open and visible this long (a quick reload keeps the summary). */
export const VISIT_AFTER_MS = 10_000;
/** While the page stays visible, the stored visit moves forward at most this often. */
export const VISIT_REFRESH_MS = 60_000;
/** The window never reaches further back than this. */
export const MAX_WINDOW_DAYS = 30;
/** Closes listed on the card; the rest are on the Activity page. */
export const MAX_ROWS = 5;
/** Worst cases within this share of each other are "about the same". */
export const SAME_WORST_RATIO = 0.1;

const DAY_MS = 86_400_000;

// A close's reason in plain words (the journal's close_reason labels).
const REASON_WORDS = {
  roi_target: "take profit",
  dte_rollout: "time roll (expiry near)",
  delta_drift: "roll (delta drifted)",
  delta_exit: "exit line (delta reached its limit)",
  stop_loss: "stop-loss",
};

/** "take profit", "time roll (expiry near)", …; other reasons as the trades table names them. */
export const closeWords = (reason) => REASON_WORDS[reason] ?? closeLabel(reason).toLowerCase();

// The header's warnings and what each means, in plain words.
const WARNING_WORDS = {
  HALTED: "the kill switch fired and trading is halted",
  OFFLINE: "the bot does not answer",
  STALE: "the bot's figures stopped updating",
  "IM over limit": "the margin used is over its limit",
};

/** The header warnings a bot shows now (Header's chips): HALTED, OFFLINE, STALE, IM over limit. */
export function warningsOf(bot) {
  const out = [];
  if (bot?.status?.halted) out.push("HALTED");
  if (bot?.error) out.push("OFFLINE");
  else if (bot?.stale) out.push("STALE");
  if (bot?.status?.risk?.im_over_limit) out.push("IM over limit");
  return out;
}

/** The worst case now, as positive losses: { coin, usd }, { none } when nothing is held, or { unknown }. */
export function worstNow(bot) {
  const failure = bot?.summaryError ?? bot?.error;
  if (failure) return { unknown: failure };
  const s = bot?.summary;
  if (!s) return { unknown: "the summary is still loading" };
  if (s.holding?.legs === 0) return { none: true };
  const w = worstCaseLine(s.worst_case);
  if (w.unknown) return { unknown: w.unknown };
  if (!isNumber(w.lossCoin)) return { unknown: "not computed yet" };
  return { coin: Math.abs(w.lossCoin), usd: isNumber(w.lossUSD) ? Math.abs(w.lossUSD) : null };
}

/**
 * What to store for a bot at `now`: { at, worst, warnings }; null until its
 * summary is in (a visit records what the person saw).
 */
export function visitRecord(bot, now) {
  if (!bot?.summary) return null;
  const w = worstNow(bot);
  return { at: new Date(now).toISOString(), worst: w.unknown ? null : w, warnings: warningsOf(bot) };
}

/** A stored visit (JSON text) as { at (ms), worst, warnings }; null when absent or unreadable. */
export function parseVisit(raw) {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw);
    const at = Date.parse(v?.at);
    if (!Number.isFinite(at)) return null;
    return { at, worst: v.worst && typeof v.worst === "object" ? v.worst : null, warnings: Array.isArray(v.warnings) ? v.warnings : [] };
  } catch {
    return null;
  }
}

/**
 * The worst case then vs now: { verdict: "larger" | "smaller" | "about the same", then, now },
 * { none } when nothing was held either time, or { unknown: reason }.
 */
export function compareWorst(then, now) {
  if (now.unknown) return { unknown: now.unknown };
  if (!then) return { unknown: "not recorded at your last visit" };
  const a = then.none ? 0 : then.coin;
  const b = now.none ? 0 : now.coin;
  if (!isNumber(a)) return { unknown: "not recorded at your last visit" };
  if (then.none && now.none) return { none: true };
  const verdict = Math.abs(b - a) <= SAME_WORST_RATIO * Math.max(a, b) ? "about the same" : b > a ? "larger" : "smaller";
  return { verdict, then, now };
}

/**
 * The hedge's journal lines after `from` in plain words: { rows, shown, more }.
 * A line that changes the book's protection comes first, so a run of trades
 * never pushes it off the card; then newest first. A row: { seq, at, text,
 * risk, result } (result in coin, a close's only).
 */
export function hedgeSince(events, from, coin) {
  const rows = events
    .filter((ev) => Date.parse(ev.at) > from)
    .map((ev) => ({ seq: ev.seq, at: ev.at, text: hedgeSentence(ev, coin) ?? ev.plain, risk: isHedgeRisk(ev), result: hedgeResult(ev.data) }))
    .filter((r) => r.text)
    .sort((x, y) => y.risk - x.risk || Date.parse(y.at) - Date.parse(x.at) || y.seq - x.seq);
  return { rows, shown: rows.slice(0, MAX_ROWS), more: Math.max(0, rows.length - MAX_ROWS) };
}

/**
 * One bot's summary since its last visit at `now`:
 *   { coin, from, capped, unknown, loading, closes, shown, more, opens, result, worst, pointers, hedge, nothing }.
 * from: the window's start (ms; at most MAX_WINDOW_DAYS back, capped says so); closes newest first;
 * result { coin, usd } net of fees, null without a close (never a 0); loading: the closes are not all in yet;
 * hedge: hedgeSince's, from hedgeEvents (useMonitor's: the journal lines the bot keeps).
 */
export function sinceLastVisit({ bot, trades, tradesComplete = true, hedgeEvents = [], visit, now }) {
  const coin = coinOf(bot);
  const earliest = now - MAX_WINDOW_DAYS * DAY_MS;
  const capped = visit.at < earliest;
  const from = capped ? earliest : visit.at;
  const pointers = warningsOf(bot)
    .filter((w) => !visit.warnings.includes(w))
    .map((w) => ({ chip: w, words: WARNING_WORDS[w] }));
  const hedge = hedgeSince(hedgeEvents, from, coin);
  const base = { coin, from, capped, pointers, hedge, worst: compareWorst(visit.worst, worstNow(bot)) };
  if (!bot.summary && (bot.summaryError ?? bot.error)) return { ...base, unknown: bot.summaryError ?? bot.error };
  if (!Array.isArray(trades) || !tradesComplete) return { ...base, loading: true };
  const after = trades.filter((t) => Date.parse(t.at) > from);
  const closes = after
    .filter((t) => t.kind === "close")
    .map((t) => ({ seq: t.seq, at: t.at, instrument: t.instrument, qty: t.qty, words: closeWords(t.reason), pnlCoin: t.pnl ?? 0, pnlUSD: t.pnl_usd ?? 0 }))
    .sort((x, y) => Date.parse(y.at) - Date.parse(x.at) || y.seq - x.seq);
  const opens = after.filter((t) => t.kind === "open").length;
  // The journal omits a close's pnl when it is exactly 0, so a missing one counts as 0.
  const result = closes.length ? { coin: sum(closes, "pnlCoin"), usd: sum(closes, "pnlUSD") } : null;
  return {
    ...base,
    closes,
    shown: closes.slice(0, MAX_ROWS),
    more: Math.max(0, closes.length - MAX_ROWS),
    opens,
    result,
    nothing: !closes.length && !opens && !pointers.length && !hedge.rows.length,
  };
}

const sum = (rows, key) => rows.reduce((a, r) => a + r[key], 0);

/** "20h ago" for a past time at `now`; days past two days ("3 d ago"). */
export function agoText(at, now) {
  const s = Math.max(0, (now - at) / 1000);
  if (s >= 2 * 86_400) return `${Math.floor(s / 86_400)} d ago`;
  return formatAge(s);
}

/** The stored visits that a dismissal hides: one text per set of visits. */
export const dismissSignature = (previous) =>
  Object.entries(previous ?? {})
    .filter(([, v]) => v)
    .map(([name, v]) => `${name}@${v.at}`)
    .sort()
    .join(",");
