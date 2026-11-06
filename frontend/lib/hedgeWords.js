// The futures hedge's journal lines in plain words (#679, epic E3): one
// sentence per change of orders.HedgeRecord, with its side, size and coin
// delta, read from the journal's own fields. The bot's `plain` catalogue says
// what kind of line it is; the numbers are here because the API's sentences
// stay as they are. A change this file does not know returns null, so the
// caller falls back to the bot's sentence: __tests__/hedge-words.test.jsx
// reads the Go constants and fails on one without a sentence here.
import { formatCoin, isNumber } from "./format";

const SHED_HEDGED = "shed_hedged"; // strategy.SkipShedHedged

const USD = (v) => `${Math.round(v).toLocaleString("en-US")} USD`;
const DONE = { buy: "Bought", sell: "Sold" };
const TO_DO = { buy: "buy", sell: "sell" };
const ORDER = { buy: "A purchase", sell: "A sale" };

/** A coin delta with its sign; a value that rounds to nothing never reads "−0.0000". */
function coinDelta(v, coin) {
  const shown = Number(v.toFixed(4));
  return formatCoin(shown === 0 ? 0 : shown, coin, 4);
}

/** "A sale of 300 USD of the perpetual": the order a line talks about. */
function orderWords(d) {
  const size = d.amount_usd > 0 ? ` of ${USD(d.amount_usd)}` : "";
  return `${ORDER[d.side] ?? "A trade"}${size} of the perpetual`;
}

/** The fill changed the perpetual's delta by its size at its price; null when the line has no price. */
function deltaAfter(d, before) {
  const price = d.price > 0 ? d.price : d.ledger_price;
  if (!(price > 0) || !(d.filled_usd > 0) || !TO_DO[d.side]) return null;
  return before + ((d.side === "buy" ? 1 : -1) * d.filled_usd) / price;
}

/** " (it was +0.4200 BTC, about +0.0100 BTC after; the band is ±0.3000 BTC)": what the line knows of it. */
function deltaFacts(d, coin) {
  const facts = [];
  if (isNumber(d.options_delta) && isNumber(d.perp_delta)) {
    const before = d.options_delta + d.perp_delta;
    const after = deltaAfter(d, before);
    facts.push(`it was ${coinDelta(before, coin)}${after == null ? "" : `, about ${coinDelta(after, coin)} after`}`);
  }
  if (d.band_coin > 0) facts.push(`the band is ±${d.band_coin.toFixed(4)} ${coin}`);
  return facts.length ? ` (${facts.join("; ")})` : "";
}

/**
 * "Sold 300 USD of the perpetual <why>", or how much of it filled. An
 * immediate order can fill in part or not at all, and is journaled either way.
 */
function tradeWords(d, coin, why) {
  const facts = deltaFacts(d, coin);
  if (!(d.filled_usd > 0)) {
    const size = d.amount_usd > 0 ? `${USD(d.amount_usd)} of the perpetual` : "the perpetual";
    return `Tried to ${TO_DO[d.side] ?? "trade"} ${size} ${why}, but nothing filled${facts}. The next cycle reads the position again.`;
  }
  const partOf = d.amount_usd > d.filled_usd ? ` out of ${USD(d.amount_usd)} due` : "";
  return `${DONE[d.side] ?? "Traded"} ${USD(d.filled_usd)} of the perpetual${partOf} ${why}${facts}.`;
}

function modeWords(d) {
  const who = d.actor?.user ?? "A person";
  if (d.enabled === true) return `${who} switched the futures hedge experiment on from the monitor: the GEX shed keeps its legs and the perpetual offsets their direction.`;
  if (d.enabled === false) return `${who} switched the futures hedge experiment off from the monitor: the GEX shed buys the threatened side back again.`;
  return `${who} switched the futures hedge experiment from the monitor.`;
}

// One sentence per orders.Hedge* change: (journal line, coin) → text.
const WORDS = {
  hedge_closed: (d, coin) =>
    tradeWords(d, coin, (d.detail ?? "").startsWith("kill switch") ? "to close the hedge: the kill switch closes what the bot holds" : "to close the hedge: the experiment was switched off"),
  hedge_failed: (d) => `${orderWords(d)} did not go through: the next cycle reads the position from the exchange before trying again.`,
  hedge_fill: (d, coin) => tradeWords(d, coin, "to bring the book's coin delta back inside its band"),
  hedge_halted: () =>
    "The futures hedge paused: the exchange will not take its orders for a reason a retry cannot cure, such as maintenance. The GEX shed protects the book meanwhile; the bot looks again every 10 minutes and resumes by itself.",
  hedge_mode_changed: modeWords,
  hedge_price_away: (d, coin) => `${orderWords(d)} waits: its price is too far from the index, so the trade would be at a poor price${deltaFacts(d, coin)}.`,
  hedge_rate_limited: (d, coin) => `${orderWords(d)} waits: the hourly limit of hedge orders was reached${deltaFacts(d, coin)}. It is tried again next cycle.`,
  hedge_resumed: () => "The futures hedge can trade again and holds the book's coin delta: the GEX shed keeps its legs.",
  hedge_unavailable: () => "The futures hedge cannot trade, so the GEX shed is back on: it buys the threatened side back as usual until the hedge can trade again.",
  perp_left_open: (d) =>
    `The kill switch left ${d.amount_usd > 0 ? USD(d.amount_usd) : "part"} of the perpetual open: it was beyond what the bot itself held, such as one held by hand. It stays on the account until a person closes it.`,
};

/** The change names with a sentence (the test compares them with the Go constants). */
export const HEDGE_CHANGES = Object.keys(WORDS);

// Lines that say the book is protected differently from a moment ago, or that something needs a person.
const RISK_CHANGES = new Set(["hedge_halted", "hedge_unavailable", "perp_left_open"]);

/** "1 × BTC-27NOV26-60000-P kept": the skip's instrument, with the size its detail gives. */
function shedWords(d) {
  const instrument = (d.skip_reason ?? "").split(/\s+/)[1] ?? d.instrument ?? "an option";
  const size = (d.detail ?? "").match(/would have bought ([0-9.]+) at/)?.[1];
  return `Kept ${size ? `${size} × ` : ""}${instrument}: the GEX shed would have bought it back, and the futures hedge offsets the book's direction with the perpetual instead. The stop-loss and the other exits still apply.`;
}

const isShedHedged = (ev) => ev?.event === "skipped" && (ev.data?.skip_reason ?? "").split(/[:\s]/)[0] === SHED_HEDGED;

/** True for a journal line of the futures hedge: its own records and the shed it kept. */
export const isHedgeEvent = (ev) => ev?.event === "hedge" || isShedHedged(ev);

/** A hedge journal line in one plain sentence with its numbers; null for any other line or an unknown change. */
export function hedgeSentence(ev, coin) {
  if (isShedHedged(ev)) return shedWords(ev.data);
  if (ev?.event !== "hedge") return null;
  const words = WORDS[ev.data?.change];
  return words ? words(ev.data, coin) : null;
}

/** True for a line that changes the book's protection (the "Since you last looked" card lists these first). */
export const isHedgeRisk = (ev) => ev?.event === "hedge" && RISK_CHANGES.has(ev.data?.change);

/** The filters of the Activity page a hedge change belongs to, by name. */
export function hedgeFilters(change, filledUSD) {
  const filled = filledUSD > 0;
  if (change === "hedge_fill") return filled ? ["money", "protection"] : ["waiting"];
  if (change === "hedge_closed") return filled ? ["money"] : ["waiting"];
  if (change === "hedge_failed" || change === "hedge_price_away" || change === "hedge_rate_limited") return ["waiting"];
  return RISK_CHANGES.has(change) ? ["protection"] : [];
}

/**
 * A close's result net of its fee, in coin; null for any other line or a close
 * that filled nothing. The journal omits an exact 0, so a missing one is a 0.
 */
export function hedgeResult(d) {
  if (d?.change !== "hedge_closed" || !(d.filled_usd > 0)) return null;
  return d.realised_coin ?? 0;
}
