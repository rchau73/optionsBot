// The futures hedge on Overview, Details and Positions (#666): which state it
// is in and its figures, coin first. No React, no fetching. `h` is the `hedge`
// object of GET /api/bots/<bot>/hedge.
import { ageSeconds, formatCoin, isNumber } from "./format";
import { formatSignedUSD } from "./summary";

/** Two bot cycles: the perpetual is read once a cycle (about 40 s) while the mode is on. */
export const HEDGE_STALE_SEC = 80;

/** The size under which the perpetual counts as closed: the bot's own test. */
export const PERP_FLAT_USD = 0.5;

/** "on", "paused", "off_open" (a position left with the mode off), "off"; null without a reading. */
export function hedgeState(h) {
  if (!h) return null;
  if (h.enabled) return h.halted ? "paused" : "on";
  return isNumber(h.perp_usd) && Math.abs(h.perp_usd) >= PERP_FLAT_USD ? "off_open" : "off";
}

/** Off and flat shows nothing on any page. */
export const hedgeShown = (h) => ["on", "paused", "off_open"].includes(hedgeState(h));

export const HEDGE_HEADLINE = {
  on: "Futures hedge ON",
  paused: "Futures hedge paused",
  off_open: "Futures hedge OFF · position still open",
};

export const HEDGE_BADGE = {
  on: { tone: "good", text: "ON" },
  paused: { tone: "warn", text: "Paused" },
  off_open: { tone: "warn", text: "OFF · position still open" },
};

/**
 * What a pause means, with its reason; null with none. Only a halt with a
 * reason gives the shed back: without one (the kill switch) the bot is idle.
 */
export function pausedWords(h) {
  return h.halt_reason ? `${h.halt_reason}. The GEX shed protects the book meanwhile.` : null;
}

/** The benchmark's pricing flag; a bot without it is priced at the index on testnet. */
export const pricedAtIndex = (h) => (h.benchmark?.pricing ?? (h.testnet ? "index" : "exchange")) === "index";

/** "−$32,310 · −0.4250 BTC": the perpetual, USD then coin delta. */
export const perpLine = (h, unit) => `${formatSignedUSD(h.perp_usd)} · ${formatCoin(h.perp_delta_coin, unit, 4)}`;

/** The perpetual's whole P&L net of fees, { coin, usd }; usd at its mark. */
export function hedgePnl(h) {
  const coin = h.benchmark?.real_pnl_coin;
  return { coin, usd: isNumber(coin) && isNumber(h.perp_mark_price) ? coin * h.perp_mark_price : null };
}

/** "short · 3,231 contracts"; without the contract size only the side. */
export function positionWords(h) {
  const side = h.perp_usd < 0 ? "short" : "long";
  if (!(h.contract_usd > 0)) return side;
  const n = Math.abs(h.perp_usd) / h.contract_usd;
  return `${side} · ${n.toLocaleString("en-US", { maximumFractionDigits: 0 })} contract${n === 1 ? "" : "s"}`;
}

/** The band as "0.0578 BTC", unsigned (it reads ±). */
export const bandWords = (h, unit) => `${Math.abs(h.band_coin ?? 0).toFixed(4)} ${unit}`;

/** Seconds since the perpetual was read when that is over two cycles, else null. */
export function hedgeStaleAge(h, now) {
  if (!h.enabled) return null;
  const age = ageSeconds(h.perp_as_of, now);
  return age != null && age > HEDGE_STALE_SEC ? age : null;
}

const moveWords = (pct) => `${pct > 0 ? "+" : "−"}${Math.abs(pct)} %`;
export { moveWords as formatBookMove };

/** The chart's points: the four moves and the zero between them. */
export function bookChartPoints(scenarios) {
  const points = (scenarios ?? []).map((s) => ({ move: s.move_pct, book: s.book_usd, options: s.options_usd }));
  return [...points, { move: 0, book: 0, options: 0 }].sort((a, b) => a.move - b.move);
}

/** The chart's text alternative: every move with and without the hedge. */
export function bookChartLabel(scenarios) {
  const rows = (scenarios ?? []).map((s) => `${moveWords(s.move_pct)}: ${formatSignedUSD(s.book_usd)} with the hedge, ${formatSignedUSD(s.options_usd)} options only`);
  return `The book's result for a one-day move. ${rows.join("; ")}.`;
}
