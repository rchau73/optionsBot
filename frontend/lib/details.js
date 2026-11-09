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

/** Decimals of a position Δ in coin (#715): "−0.018 BTC". */
export const POSITION_DELTA_DIGITS = 3;

/** Under this |Δ| (coin) the book reads "flat": it rounds to 0.000. */
export const FLAT_DELTA = 0.0005;

/**
 * A leg's position Δ in its coin (#715): one contract's Δ × the size held,
 * sign flipped for a short (a sold call is negative, a sold put positive); null
 * without greeks or size.
 */
export function positionDelta(row) {
  if (!isNumber(row?.delta) || !isNumber(row?.qty)) return null;
  return (row.side === "buy" ? 1 : -1) * row.delta * row.qty;
}

/** The sum of the rows' position Δ; null when any row has none or there are none (never a partial sum). */
export function netPositionDelta(rows) {
  if (!rows?.length) return null;
  let total = 0;
  for (const r of rows) {
    const d = positionDelta(r);
    if (d == null) return null;
    total += d;
  }
  return total;
}

/** What a +1 % move of the coin does to a delta, in USD (Δ × spot ÷ 100); null without spot. */
export function usdPerPct(delta, spot) {
  return isNumber(delta) && isNumber(spot) && spot > 0 ? (delta * spot) / 100 : null;
}

/** "−0.018 BTC": a position Δ, signed, 3 decimals, in its coin. */
export const formatPositionDelta = (v, unit) => formatCoin(v, unit, POSITION_DELTA_DIGITS);

/** The options' side in words: "long: the book loses if BTC falls", "short: … rises", "flat". */
export function deltaSideWords(delta, unit) {
  if (!isNumber(delta)) return null;
  if (Math.abs(delta) < FLAT_DELTA) return "flat";
  return delta > 0 ? `long: the book loses if ${unit} falls` : `short: the book loses if ${unit} rises`;
}

/** What the perpetual gains on: a short gains if the coin falls, a long if it rises. */
export const hedgeGainWords = (perpDelta, unit) => `gains if ${unit} ${perpDelta < 0 ? "falls" : "rises"}`;

/**
 * The Position Δ header's tooltip, with the page's first sold put as its
 * example when there is one ("BTC −1 % costs about $34").
 */
export function positionDeltaTip(rows) {
  const base =
    "The delta of what you hold: one contract's Δ × the size, negative for a short call, positive for a short put. The legs add up to the strangle; the strangles to the book.";
  const put = (rows ?? []).find((r) => r.type === "put" && r.side !== "buy" && positionDelta(r) != null);
  if (!put) return base;
  const d = positionDelta(put);
  const usd = usdPerPct(d, put.spot);
  const strike = isNumber(put.strike) ? put.strike.toLocaleString("en-US", { maximumFractionDigits: 2 }) : "";
  const cost = usd != null ? `: ${put.unit} −1 % ${usd >= 0 ? "costs" : "gains"} about $${Math.abs(Math.round(usd)).toLocaleString("en-US")}` : "";
  return `${base} Example: the ${strike} put, Δ ${formatCoin(put.delta, "", POSITION_DELTA_DIGITS)}, ${put.qty} sold → ${formatPositionDelta(d, put.unit)}${cost}.`;
}

/**
 * The foot rows of one bot's book (#715): all its options' position Δ, and
 * while a futures hedge is shown its perpetual and the book with it. bot is
 * useMonitor's ({ name, status, hedge, hedgeError }); rows are its leg rows
 * (unfiltered, so the book is whole). hedgeShown is passed in (lib/futuresHedge).
 * Returns { bot, unit, spot, options, hedge: null | { instrument, perpUsd,
 * perpDelta, book, optionsCoin, netCoin, band, inside }, unavailable }.
 */
export function bookDelta(bot, rows, shown) {
  const unit = bot.status?.underlying ?? bot.name.toUpperCase();
  const spot = bot.status?.market?.spot ?? null;
  const options = netPositionDelta(rows);
  const h = bot.hedge;
  let hedge = null;
  if (h && shown(h) && isNumber(h.perp_delta_coin)) {
    hedge = {
      instrument: h.instrument || `${unit}-PERPETUAL`,
      perpUsd: h.perp_usd,
      perpDelta: h.perp_delta_coin,
      book: options != null ? options + h.perp_delta_coin : null,
      optionsCoin: h.options_delta_coin,
      netCoin: h.net_delta_coin,
      band: Math.abs(h.band_coin ?? 0),
      inside: isNumber(h.net_delta_coin) && Math.abs(h.net_delta_coin) <= Math.abs(h.band_coin ?? 0),
    };
  }
  return { bot: bot.name, unit, spot, options, hedge, unavailable: !h && Boolean(bot.hedgeError) };
}

/** The Book row's tooltip: options Δ + the options' own value = coin delta, less the hedge = net; trades again at ±band. */
export function bookTip(options, hedge, unit) {
  const f = (v) => formatCoin(v, "", 4);
  const own = isNumber(hedge.optionsCoin) && isNumber(options) ? hedge.optionsCoin - options : null;
  return (
    `Position Δ is the options' delta. The hedge keeps your ${unit} count flat, so it also counts the options' own value moving with the price: ` +
    `${f(options)} ${own < 0 ? "−" : "+"} ${isNumber(own) ? Math.abs(own).toFixed(4) : MISSING} = ${f(hedge.optionsCoin)} ${unit} in coin, ${hedge.perpDelta < 0 ? "less" : "plus"} the hedge's ${Math.abs(hedge.perpDelta).toFixed(4)} = ${f(hedge.netCoin)} ${unit}. ` +
    `It trades again at ±${hedge.band.toFixed(4)}.`
  );
}

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

/** Section counts of the sticky bar, and whether each needs attention (amber); Futures hedge only with one shown. */
export function sectionCounts({ legs, held, trades, orders, stuck, others, hedges = 0, hedgeAttention = false }) {
  return [
    { id: "positions", label: "Positions", n: legs, attention: false },
    ...(hedges > 0 ? [{ id: "hedge", label: "Futures hedge", n: hedges, attention: hedgeAttention }] : []),
    { id: "held", label: "Missing legs", n: held, attention: held > 0 },
    { id: "trades", label: "Trades", n: trades, attention: false },
    { id: "orders", label: "Working orders", n: orders, attention: stuck > 0 },
    { id: "others", label: "Not managed", n: others, attention: others > 0 },
  ];
}
