// Pure logic of the Details (pro) page (#405): coin-first numbers, short leg
// names, the inline bars' geometry and the counts line. No React, no
// fetching — everything here is unit-tested.
import { formatCoin, formatDate, formatTime, isNumber, MISSING } from "./format";
import { formatSignedUSD } from "./summary";

/** Fixed decimals of each coin's P&L: BTC 5, ETH 4; another coin 5. */
export const COIN_DIGITS = { BTC: 5, ETH: 4 };

export const coinDigits = (unit) => COIN_DIGITS[String(unit ?? "").toUpperCase()] ?? 5;

/** P&L in its own coin with that coin's fixed decimals: "+0.00125 BTC", "−0.0040 ETH". */
export function formatCoinFirst(v, unit) {
  return formatCoin(v, unit, coinDigits(unit));
}

/** The small USD beside a coin amount: "≈ +$12", "≈ −$1,250"; "≈ —" when unknown. */
export function approxUSD(v) {
  return `≈ ${formatSignedUSD(v)}`;
}

const MONTHS = { JAN: "Jan", FEB: "Feb", MAR: "Mar", APR: "Apr", MAY: "May", JUN: "Jun", JUL: "Jul", AUG: "Aug", SEP: "Sep", OCT: "Oct", NOV: "Nov", DEC: "Dec" };

/** "27NOV26" → "27 Nov" (daily expiries have one-digit days: "4OCT26" → "4 Oct"); null otherwise. */
export function expiryLabel(code) {
  const m = /^(\d{1,2})([A-Z]{3})(\d{2})$/.exec(String(code ?? ""));
  return m && MONTHS[m[2]] ? `${Number(m[1])} ${MONTHS[m[2]]}` : null;
}

/** "BTC-27NOV26-2850-C" → "27 Nov 2,850 C"; anything else as given. XRP's "2d5" strikes read 2.5. */
export function shortLeg(instrument) {
  const parts = String(instrument ?? "").split("-");
  if (parts.length < 4) return instrument ?? MISSING;
  const expiry = expiryLabel(parts[1]);
  const strike = Number(parts[2].replace("d", "."));
  if (!expiry || !Number.isFinite(strike)) return instrument;
  return `${expiry} ${strike.toLocaleString("en-US")} ${parts[3]}`;
}

/** |Δ| ÷ the delta exit, or null; ≥ 0.8 is shown amber (the exit is near). */
export function deltaExitRatio(delta, threshold) {
  if (!isNumber(delta) || !isNumber(threshold) || threshold <= 0) return null;
  return Math.abs(delta) / threshold;
}

export const DELTA_EXIT_WARN = 0.8;

/** Group label of one strangle: "BTC · 25-day slot · expires 13 Nov · 16 days". */
export function strangleLabel(row) {
  const parts = [row.bot.toUpperCase()];
  parts.push(isNumber(row.dteTarget) ? `${row.dteTarget}-day slot` : "slot unknown");
  const expiry = expiryLabel(String(row.instrument ?? "").split("-")[1]);
  parts.push(expiry ? `expires ${expiry}` : "expiry unknown");
  if (isNumber(row.dte)) {
    const days = Math.floor(row.dte);
    parts.push(`${days} day${days === 1 ? "" : "s"}`);
  }
  return parts.join(" · ");
}

/** The title line's counts: "2/2 bots online · 4 orders sent · 2 filled · 1 close · 3 entries skipped · since 2026-10-02 · updated 09:34:56 BRT". */
export function countsLine(kpi, polledAt) {
  const plural = (n, one, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
  return [
    `${kpi.online}/${kpi.bots} bots online`,
    `${plural(kpi.submitted, "order")} sent · ${kpi.filled} filled`,
    plural(kpi.closed, "close"),
    `${plural(kpi.skipped, "entry", "entries")} skipped`,
    kpi.since ? `since ${formatDate(kpi.since)}` : "since start",
    polledAt ? `updated ${formatTime(new Date(polledAt).toISOString())}` : "not updated yet",
  ].join(" · ");
}

/** Working orders past their fill timeout (the bot cancels them on its next cycle; one still shown is stuck). */
export function stuckOrders(bots, now = Date.now()) {
  let n = 0;
  for (const b of bots ?? []) {
    for (const p of b.orders?.pending ?? []) {
      const t = Date.parse(p.timeout_at ?? "");
      if (Number.isFinite(t) && t < now) n++;
    }
  }
  return n;
}

/** Each working order leg across bots. */
export const workingOrderCount = (bots) => (bots ?? []).reduce((a, b) => a + (b.orders?.pending ?? []).reduce((s, p) => s + (p.legs?.length ?? 0), 0), 0);

/** The first sentence of a plain-words explanation (up to the first ". "), the whole text when it has one; null when empty. */
export function firstSentence(text) {
  if (typeof text !== "string" || !text.trim()) return null;
  const m = /^(.+?\.)\s/.exec(text.trim());
  return m ? m[1] : text.trim();
}

/**
 * Geometry of the 120 px "spot vs flip + buffer" bar of a missing leg, in % of
 * the bar: the shaded band flip × (1 ± buffer), the flip's tick and spot's dot.
 * The scale spans the band and spot with a margin. Null without spot or flip.
 */
export function flipBarGeometry(spot, flip, bufferPct) {
  if (!isNumber(spot) || !isNumber(flip) || spot <= 0 || flip <= 0) return null;
  const buf = isNumber(bufferPct) && bufferPct > 0 ? bufferPct / 100 : 0;
  const low = flip * (1 - buf);
  const high = flip * (1 + buf);
  const lo = Math.min(low, spot);
  const hi = Math.max(high, spot);
  const pad = Math.max((hi - lo) * 0.15, flip * 0.005);
  const min = lo - pad;
  const span = hi + pad - min;
  const at = (v) => ((v - min) / span) * 100;
  return { bandLeft: at(low), bandWidth: at(high) - at(low), flip: at(flip), spot: at(spot), low, high, above: spot > high, below: spot < low };
}

/** Section counts of the sticky bar, and whether each needs attention (amber). */
export function sectionCounts({ legs, held, trades, orders, stuck, others }) {
  return [
    { id: "positions", label: "Positions", n: legs, attention: false },
    { id: "held", label: "Missing legs", n: held, attention: held > 0 },
    { id: "trades", label: "Trades", n: trades, attention: false },
    { id: "orders", label: "Working orders", n: orders, attention: stuck > 0 },
    { id: "others", label: "Not managed", n: others, attention: others > 0 },
  ];
}
