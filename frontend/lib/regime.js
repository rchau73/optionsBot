// The overview's regime chart and badges (#394): pure helpers over one bot's
// /api/status (`sma.candles`, `sma`, `market`, `flip_buffer_pct`,
// `median_flip`, `gex_as_of`, `risk.status`, `held_legs`) and its book
// (`/api/positions`). Missing data is "unknown: <reason>", never 0.
import { flipBand } from "./monitor";
import { ageSeconds, formatAge, formatPct, formatUSD, isNumber } from "./format";

/** The bot's GEX snapshot is stale past this age (the bot's own GEXStaleAfter, 5 min). */
export const GEX_STALE_SEC = 5 * 60;

const positive = (v) => isNumber(v) && v > 0;
const regimeWord = (label) => (typeof label === "string" && label.startsWith("NEGATIVE") ? "NEGATIVE" : typeof label === "string" && label.startsWith("POSITIVE") ? "POSITIVE" : null);

/**
 * The regime badge: { word: POSITIVE | NEGATIVE | null, basis: confirmed | live | null,
 * confirmed (the margin policy's confirmed word or null), shed: ["put"|"call"], unknown }.
 * The live GEX regime is shown; "confirmed" when the margin policy has confirmed
 * the same regime on its daily closes, "live" otherwise.
 */
export function regimeBadge(status) {
  const live = regimeWord(status?.market?.gex_regime);
  const r = status?.risk?.status;
  const confirmed = r?.regime_known ? (r.regime_negative ? "NEGATIVE" : "POSITIVE") : null;
  const shed = [
    ...new Set((status?.held_legs ?? []).filter((h) => h.rule === "gex_shed" || h.rule === "gex_shedding").map((h) => h.missing)),
  ].filter(Boolean);
  if (!live && !confirmed) return { word: null, basis: null, confirmed: null, shed, unknown: "unknown: no GEX snapshot yet" };
  const word = live ?? confirmed;
  return { word, basis: confirmed === word ? "confirmed" : "live", confirmed, shed, unknown: null };
}

/**
 * The trend badge: { trend: bull | bear | neutral | null, reason, source }.
 * The direction the rules use (`trend`) with the condition that set it: the
 * SMA trend's reason (`SMATrendOf`), or the GEX bias under gex_direction bias.
 */
export function trendBadge(status) {
  const trend = status?.trend || null;
  if (!trend) return { trend: null, reason: "unknown: no trend reported yet", source: null };
  if (status.direction === "bias") {
    return { trend, reason: `GEX bias ${status.gex_bias ?? "unknown"} (SMA trend ${status.sma_trend ?? "unknown"}: ${status.sma?.reason || "no reason"})`, source: "bias" };
  }
  return { trend, reason: status.sma?.reason || "no reason reported", source: "sma" };
}

/** The GEX snapshot's age: { ageSec, text, stale }; text says "unknown: …" without one. */
export function gexFreshness(status, now = Date.now()) {
  const ageSec = ageSeconds(status?.gex_as_of ?? undefined, now);
  if (ageSec == null) return { ageSec: null, text: "GEX unknown: no snapshot yet", stale: true };
  return { ageSec, text: `GEX ${formatAge(ageSec)}`, stale: ageSec > GEX_STALE_SEC };
}

/** Rolling mean of the n closes before each candle (the bot's SMA reads completed days only). */
function smaBefore(candles, i, n) {
  if (i < n) return null;
  let sum = 0;
  for (let j = i - n; j < i; j++) sum += candles[j].close;
  return sum / n;
}

/**
 * The chart's data: one row per daily candle, oldest first —
 * { date, open, high, low, close, range: [low, high], up, sma9, sma21 } —
 * where sma9/sma21 are the means of the 9/21 completed closes before that day
 * (so today's equal the bot's SMA9/SMA21), null before enough days. Candles
 * without a positive close are left out.
 */
export function candleRows(status) {
  const raw = Array.isArray(status?.sma?.candles) ? status.sma.candles : [];
  const candles = raw.filter((c) => positive(c?.close) && typeof c.date === "string");
  return candles.map((c, i) => {
    const open = positive(c.open) ? c.open : c.close;
    const high = Math.max(positive(c.high) ? c.high : c.close, open, c.close);
    const low = Math.min(positive(c.low) ? c.low : c.close, open, c.close);
    return { date: c.date, open, high, low, close: c.close, range: [low, high], up: c.close >= open, sma9: smaBefore(candles, i, 9), sma21: smaBefore(candles, i, 21) };
  });
}

/**
 * The held strikes as faint lines: one per strike, "floor" (put) or "ceiling"
 * (call) with its DTE slot, e.g. { price: 90000, side: "put", label: "floor 25d" }.
 * Several slots on one strike share its line.
 */
export function strikeLines(positions) {
  const byKey = new Map();
  for (const st of positions?.strangles ?? []) {
    for (const leg of st.legs ?? []) {
      if (!positive(leg.strike) || (leg.option_type !== "put" && leg.option_type !== "call")) continue;
      const key = `${leg.option_type}:${leg.strike}`;
      const slot = st.slot?.dte != null ? `${st.slot.dte}d` : "slot ?";
      const prev = byKey.get(key);
      if (prev) {
        if (!prev.slots.includes(slot)) prev.slots.push(slot);
      } else byKey.set(key, { price: leg.strike, side: leg.option_type, slots: [slot] });
    }
  }
  return [...byKey.values()]
    .map((s) => ({ price: s.price, side: s.side, label: `${s.side === "put" ? "floor" : "ceiling"} ${s.slots.join("/")}` }))
    .sort((a, b) => a.price - b.price);
}

/**
 * Every level drawn on the regime chart: flip (with its shed band low/high),
 * median flip (bias only), SMA9/SMA21 now, the swing walls with their days,
 * spot. Absent levels are null.
 */
export function regimeLevels(status) {
  const band = flipBand(status);
  const sma = status?.sma ?? {};
  const wall = (v, date, label) => (positive(v) ? { price: v, date: date || null, label: `${label}${date ? ` ${date.slice(5)}` : ""}` } : null);
  return {
    spot: positive(status?.market?.spot) ? status.market.spot : null,
    flip: band ? { price: band.flip, low: band.low, high: band.high, bufferPct: band.bufferPct } : null,
    medianFlip: status?.direction === "bias" && positive(status?.median_flip) ? status.median_flip : null,
    sma9: positive(sma.sma9) ? sma.sma9 : null,
    sma21: positive(sma.sma21) ? sma.sma21 : null,
    swingHigh: wall(sma.swing_high, sma.swing_high_date, "swing high"),
    swingLow: wall(sma.swing_low, sma.swing_low_date, "swing low"),
  };
}

/**
 * The y-axis domain [min, max] over the candles and every shown level, with
 * 2 % padding; null without anything to draw.
 */
export function yDomain(rows, prices) {
  const values = [...rows.flatMap((r) => [r.low, r.high]), ...prices.filter(positive)];
  if (!values.length) return null;
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const pad = Math.max((hi - lo) * 0.02, hi * 0.002);
  return [lo - pad, hi + pad];
}

/** "above" / "below" / "at" for a price against a level. */
const side = (price, level) => (price > level ? "above" : price < level ? "below" : "at");

/**
 * The chart's text alternative: regime, trend, spot vs the flip and its band,
 * spot vs SMA9/SMA21 and the swings. Unknown parts say so.
 */
export function regimeText(status, coin) {
  const r = regimeBadge(status);
  const t = trendBadge(status);
  const l = regimeLevels(status);
  const parts = [];
  parts.push(r.word ? `${coin} regime ${r.word} (${r.basis})${r.shed.length ? `, ${r.shed.join(" and ")} side shed` : ""}` : `${coin} regime ${r.unknown}`);
  parts.push(t.trend ? `trend ${t.trend}: ${t.reason}` : `trend ${t.reason}`);
  if (!l.spot) parts.push("spot unknown");
  else {
    if (l.flip) {
      const dist = ((l.spot - l.flip.price) / l.flip.price) * 100;
      const band = l.flip.low && l.flip.high ? `, shed band ${formatUSD(l.flip.low)}–${formatUSD(l.flip.high)}` : "";
      parts.push(`spot ${formatUSD(l.spot)} ${side(l.spot, l.flip.price)} the flip ${formatUSD(l.flip.price)} (${formatPct(dist, 1, { signed: true })})${band}`);
    } else parts.push(`spot ${formatUSD(l.spot)}; flip unknown: none found`);
    for (const [name, v] of [
      ["SMA9", l.sma9],
      ["SMA21", l.sma21],
    ]) {
      parts.push(v ? `${side(l.spot, v)} ${name} ${formatUSD(v)}` : `${name} unknown`);
    }
    if (l.swingHigh) parts.push(`swing high ${formatUSD(l.swingHigh.price)} (${l.swingHigh.date})`);
    if (l.swingLow) parts.push(`swing low ${formatUSD(l.swingLow.price)} (${l.swingLow.date})`);
  }
  return `${parts.join("; ")}.`;
}
