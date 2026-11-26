// Server-side only: the assistant's "Hedge now" passage (#877). A question
// about the futures hedge, with a bot, gets one passage, live:<bot>/hedge,
// from the bot's GET /api/hedge and the hedge lines of the journal already
// read for the question: whether the hedge is on, off or cannot trade, the
// perpetual's side and size, the coin delta against its band, the newest hedge
// order and the trial's days. Percentages and counts only — no amount, no
// account detail: a bot may run live and the review log is a file.
import { hedgeOrderSentence, HEDGE_ORDER_CHANGES } from "@/lib/hedgeWords";
import { PERP_FLAT_USD } from "@/lib/futuresHedge";

// A question about the futures hedge or the perpetual: only these pay for the hedge passage.
const HEDGE_PATTERNS = [/\bhedg(e|es|ed|ing)\b/i, /\bperp(etual)?s?\b/i, /\bfutures?\b/i];

/** Whether a question asks about the futures hedge or the perpetual (#877). */
export function asksAboutHedge(question) {
  const q = String(question ?? "");
  return HEDGE_PATTERNS.some((re) => re.test(q));
}

// docs/hedge-trial.md "Minimum span": no figure, and the verdict "needs more days", under it.
export const HEDGE_TRIAL_MIN_DAYS = 7;
// The journal read for a question (service.js JOURNAL_DAYS): repeated here to avoid a cycle.
const JOURNAL_WINDOW_DAYS = 7;
const DAY_MS = 86_400_000;
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n) => String(n).padStart(2, "0");
const isNum = (v) => typeof v === "number" && Number.isFinite(v);
const pct = (v) => v.toLocaleString("en-US", { maximumFractionDigits: 1 });
const signed = (v) => `${v > 0 ? "+" : v < 0 ? "−" : ""}${pct(Math.abs(v))}`;

/** A time as ms, or null for none and for Go's zero time (year 1). */
function timeMs(iso) {
  const t = Date.parse(iso ?? "");
  return Number.isNaN(t) || new Date(t).getUTCFullYear() < 2000 ? null : t;
}

/** "22 Nov 10:00" (UTC). */
function shortTime(ms) {
  const d = new Date(ms);
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}

/** "27 Nov 2026" (UTC). */
function day(ms) {
  const d = new Date(ms);
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}

/** The margin balance in coin the band stands for (band = band_pct % of it); null when unknown. */
const balanceCoin = (h) => (h.band_coin > 0 && h.band_pct > 0 ? h.band_coin / (h.band_pct / 100) : null);

/** On, off or unable to trade, and why, in the monitor's words. */
function stateLine(h, route) {
  const live = h.testnet === false ? ` This is a live account: ${route?.switch_off_reason || "the experiment is testnet-only"}, so it cannot be switched on here.` : " The experiment runs on testnet only.";
  if (!h.available) return `State: unavailable: ${h.unavailable || "this bot cannot run the futures hedge"}. The GEX shed protects the book as usual.${live}`;
  if (!h.enabled) return `State: off: the GEX shed buys the threatened side back as usual, and the perpetual holds nothing for the book.${live}`;
  if (h.kill_switch) return `State: on, but the kill switch fired: the bot trades nothing until a person restarts it.${live}`;
  if (h.halted) return `State: on, but it cannot trade now: ${h.halt_reason || "the exchange refuses its orders"}. The GEX shed protects the book meanwhile; the bot looks again by itself.${live}`;
  if (h.stood_down) return `State: on, but standing down: ${h.stood_down}. The GEX shed protects the book meanwhile.${live}`;
  if (h.not_holding) return `State: on, but not holding the delta: ${h.not_holding}. The GEX shed is back on until the hedge can trade again.${live}`;
  return `State: on: the GEX shed keeps its legs and the perpetual holds the book's coin delta inside its band.${live}`;
}

/** The perpetual's side and size as % of the margin balance. */
function perpLine(h) {
  if (!isNum(h.perp_usd) || Math.abs(h.perp_usd) < PERP_FLAT_USD) return "Perpetual: none open.";
  const side = h.perp_usd < 0 ? "short" : "long";
  const left = h.enabled ? "" : " (left open with the hedge off)";
  const bal = balanceCoin(h);
  if (bal == null || !isNum(h.perp_delta_coin)) return `Perpetual: ${side}${left}; its size as a share of the margin balance is unknown: the margin balance was not read this cycle.`;
  return `Perpetual: ${side}${left}, ${pct((Math.abs(h.perp_delta_coin) / bal) * 100)} % of the margin balance.`;
}

/** The book's coin delta against the band, and the hour's orders, while the hedge is on. */
function deltaLine(h) {
  if (!h.available || !h.enabled) return "Coin delta: the hedge holds none while it is off.";
  const bal = balanceCoin(h);
  if (bal == null || !isNum(h.net_delta_coin)) return "Coin delta against its band: unknown: the margin balance was not read this cycle, so the hedge trades nothing.";
  const net = (h.net_delta_coin / bal) * 100;
  const inside = Math.abs(net) < h.band_pct;
  const next = inside ? "" : " The next cycle that can trade brings it back with the perpetual.";
  const orders = isNum(h.orders_last_hour) && isNum(h.max_orders_per_hour) ? ` Hedge orders in the last hour: ${h.orders_last_hour} of the ${h.max_orders_per_hour} allowed.` : "";
  return `Coin delta: ${signed(net)} % of the margin balance, ${inside ? "inside" : "outside"} its ±${pct(h.band_pct)} % band.${next}${orders}`;
}

/** The newest hedge order of the journal read, in plain words with its reason. */
function orderLine(journal) {
  let newest = null;
  for (const p of journal ?? []) {
    if (p.event !== "hedge" || !HEDGE_ORDER_CHANGES.includes(p.reason)) continue;
    if (!newest || newest.seq < p.seq) newest = p;
  }
  if (!newest) return `Newest hedge order: none in the journal of the last ${JOURNAL_WINDOW_DAYS} days.`;
  const at = timeMs(newest.lastAt);
  return `Newest hedge order (${at == null ? "time unknown" : `${shortTime(at)} UTC`}): ${hedgeOrderSentence(newest.reason, newest.side)}`;
}

/** The trial's days since the ledger began, not started or needing more days (docs/hedge-trial.md), and its risks. */
function trialLine(h, nowMs) {
  const max = isNum(h.max_orders_per_hour) ? h.max_orders_per_hour : "a fixed number of";
  const risks =
    ` Its risks: the perpetual's fees and funding, a hedge that cannot trade (the GEX shed then takes over), and at most ${max} hedge orders an hour;` +
    " the comparison is shadow-based on testnet prices, not a result.";
  const since = timeMs(h.benchmark?.since);
  if (timeMs(h.since) == null || since == null) return `Trial: not started: the hedge has never been switched on.${risks}`;
  const days = Math.max(0, Math.floor((nowMs - since) / DAY_MS));
  const measured = `Trial: ${days} day${days === 1 ? "" : "s"} measured since ${day(since)}`;
  if (days < HEDGE_TRIAL_MIN_DAYS) {
    const missing = HEDGE_TRIAL_MIN_DAYS - days;
    return `${measured}: needs more days, ${missing} still missing of the ${HEDGE_TRIAL_MIN_DAYS} the comparison needs.${risks}`;
  }
  return `${measured}; the weekly evidence report gives its verdict.${risks}`;
}

/**
 * The futures hedge now (#877) as the citable passage "live:<bot>/hedge":
 * whether it is on, off or cannot trade (and why), the perpetual's side and
 * size as % of the margin balance, the coin delta against its band, the
 * newest hedge order of the journal read and the trial's days with its
 * risks. Percentages and counts only. route: GET /api/hedge's JSON
 * ({ hedge, switchable, switch_off_reason }); journal: journalPassages;
 * error: why the route could not be read, which makes the passage "unknown".
 */
export function hedgePassage(bot, { route = null, journal = [], error = "", nowMs = Date.now() } = {}) {
  const base = { id: `live:${bot}/hedge`, source: `live:${bot}`, title: `${bot} futures hedge now`, live: true };
  const h = route?.hedge;
  if (!h) return { ...base, text: `Hedge now: unknown: ${error || "the bot's hedge route did not answer"}. Whether the hedge is on, and why it traded, cannot be said now.` };
  const lines = [`Hedge now (read ${shortTime(nowMs)} UTC):`, stateLine(h, route), perpLine(h), deltaLine(h), orderLine(journal), trialLine(h, nowMs)];
  return { ...base, text: lines.join(" ") };
}
