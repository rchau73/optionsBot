// One band per bot on the overview (#404): the price chart with the sold-strike
// corridor and the strangle rows. Pure helpers over the bot's /api/status
// (sma.candles, sma, flip_buffer_pct, market) and /api/strangles/payoff
// (strikes, break-evens, days left). The bot does the math; these only place
// and word it. Missing data is "unknown", never 0.
import { formatPct, isNumber } from "./format";
import { earnedBar, price0 } from "./payoff";
import { candleRows, regimeLevels } from "./regime";

const positive = (v) => isNumber(v) && v > 0;

/** The price chart's layers, each a toggle pill (all on by default); the keys are overview_layer_toggle's. */
export const BAND_LAYERS = [
  { key: "sma", label: "SMA9" },
  { key: "flip", label: "Flip band" },
  { key: "strikes", label: "Sold strikes" },
];

/** Days left to a payoff strangle's expiry (whole days, rounded down as the bot counts DTE); null when unknown. */
export function daysLeft(sp) {
  return isNumber(sp?.dte) && sp.dte >= 0 ? Math.floor(sp.dte) : null;
}

/**
 * Every sold strike of the bot, one per side and price, with the days left of
 * the strangles on it: { side, price, label: "put 74,000 · 25 d" }, by price.
 */
export function soldStrikes(payoff) {
  const byKey = new Map();
  for (const sp of payoff?.strangles ?? []) {
    const days = daysLeft(sp);
    for (const leg of sp.legs ?? []) {
      if (!positive(leg.strike) || (leg.option_type !== "put" && leg.option_type !== "call")) continue;
      const key = `${leg.option_type}:${leg.strike}`;
      const prev = byKey.get(key) ?? { side: leg.option_type, price: leg.strike, days: [] };
      if (days != null && !prev.days.includes(days)) prev.days.push(days);
      byKey.set(key, prev);
    }
  }
  return [...byKey.values()]
    .map((s) => ({ side: s.side, price: s.price, label: `${s.side} ${price0(s.price)}${s.days.length ? ` · ${s.days.sort((a, b) => a - b).join("/")} d` : ""}` }))
    .sort((a, b) => a.price - b.price);
}

/** The shaded corridor between the lowest and the highest sold strike; null with fewer than two prices. */
export function strikeCorridor(strikes) {
  const prices = strikes.map((s) => s.price);
  if (prices.length < 2) return null;
  const low = Math.min(...prices);
  const high = Math.max(...prices);
  return high > low ? { low, high } : null;
}

/**
 * Pushes labels apart so none overlaps: each { y } at least gap px from the
 * next, kept within [top, bottom] where there is room. Returns new objects in
 * the input's order, each with its drawn y as labelY.
 */
export function nudgeLabels(labels, gap, top, bottom) {
  const order = labels.map((l, i) => ({ i, y: l.y })).sort((a, b) => a.y - b.y);
  for (let k = 0; k < order.length; k++) {
    const min = k === 0 ? top : order[k - 1].y + gap;
    order[k].y = Math.max(order[k].y, min);
  }
  for (let k = order.length - 1; k >= 0; k--) {
    const max = k === order.length - 1 ? bottom : order[k + 1].y - gap;
    order[k].y = Math.min(order[k].y, max);
  }
  const out = labels.map((l) => ({ ...l }));
  for (const o of order) out[o.i].labelY = o.y;
  return out;
}

/** The chart box: an 800 × 240 viewBox, labels in the right margin. */
export const BAND_BOX = { width: 800, height: 240, left: 4, right: 150, top: 10, bottom: 22, labelGap: 13 };

/**
 * The price chart's geometry, or null without two candles: the last 60 daily
 * candles (x, wick, body, up), the y scale over the candles and every sold
 * strike (and spot), the strike corridor, the flip and its shed band, SMA9's
 * path and the right-edge labels — only of the layers shown, nudged apart.
 * layers: { sma, flip, strikes } booleans.
 */
export function bandChart(status, payoff, layers = { sma: true, flip: true, strikes: true }, box = BAND_BOX) {
  const rows = candleRows(status).slice(-60);
  if (rows.length < 2) return null;
  const levels = regimeLevels(status);
  const strikes = soldStrikes(payoff);
  const prices = [...rows.flatMap((r) => [r.low, r.high]), ...strikes.map((s) => s.price), ...(levels.spot ? [levels.spot] : [])];
  const lo0 = Math.min(...prices);
  const hi0 = Math.max(...prices);
  const pad = Math.max((hi0 - lo0) * 0.04, hi0 * 0.002);
  const lo = lo0 - pad;
  const hi = hi0 + pad;
  const plotRight = box.width - box.right;
  const plotBottom = box.height - box.bottom;
  const y = (p) => box.top + ((hi - p) / (hi - lo)) * (plotBottom - box.top);
  const inRange = (p) => positive(p) && p >= lo && p <= hi;
  const slot = (plotRight - box.left) / rows.length;
  const xOf = (i) => box.left + slot * (i + 0.5);

  const candles = rows.map((r, i) => {
    const top = y(Math.max(r.open, r.close));
    return { date: r.date, x: xOf(i), w: Math.max(slot * 0.65, 1), high: y(r.high), low: y(r.low), bodyTop: top, bodyHeight: Math.max(y(Math.min(r.open, r.close)) - top, 1), up: r.up };
  });
  const sma = layers.sma
    ? rows
        .map((r, i) => (positive(r.sma9) && inRange(r.sma9) ? `${xOf(i).toFixed(1)},${y(r.sma9).toFixed(1)}` : null))
        .filter(Boolean)
        .join(" ")
    : "";
  const corridorPrices = strikeCorridor(strikes);
  const corridor = layers.strikes && corridorPrices ? { y1: y(corridorPrices.high), y2: y(corridorPrices.low), ...corridorPrices } : null;
  const f = levels.flip;
  const clampY = (p) => y(Math.min(Math.max(p, lo), hi));
  const flip =
    layers.flip && f
      ? { price: f.price, y: inRange(f.price) ? y(f.price) : null, bandTop: f.high ? clampY(f.high) : null, bandBottom: f.low ? clampY(f.low) : null }
      : null;
  const shownStrikes = layers.strikes ? strikes.map((s) => ({ ...s, y: y(s.price) })) : [];

  const labels = [];
  for (const s of shownStrikes) labels.push({ key: `strike-${s.side}-${s.price}`, kind: s.side, y: s.y, text: s.label });
  if (flip?.y != null) labels.push({ key: "flip", kind: "flip", y: flip.y, text: `flip ${price0(f.price)}` });
  const sma9Now = levels.sma9;
  if (layers.sma && inRange(sma9Now)) labels.push({ key: "sma9", kind: "sma", y: y(sma9Now), text: `SMA9 ${price0(sma9Now)}` });
  if (levels.spot) labels.push({ key: "spot", kind: "spot", y: y(levels.spot), text: `spot ${price0(levels.spot)}` });

  return {
    box: { ...box, plotRight, plotBottom },
    domain: [lo, hi],
    candles,
    sma,
    corridor,
    flip,
    strikes: shownStrikes,
    spot: levels.spot ? { y: y(levels.spot), x: candles[candles.length - 1].x } : null,
    labels: nudgeLabels(labels, box.labelGap, box.top + 4, plotBottom),
    first: rows[0].date,
    last: rows[rows.length - 1].date,
  };
}

/** The price chart in words, for its aria-label. */
export function bandChartText(coin, status, payoff) {
  const l = regimeLevels(status);
  const strikes = soldStrikes(payoff);
  const c = strikeCorridor(strikes);
  const parts = [`${coin} daily candles, last ${Math.min(candleRows(status).length, 60)} days`];
  parts.push(l.spot ? `spot ${price0(l.spot)}` : "spot unknown");
  if (c) parts.push(`sold strikes from ${price0(c.low)} to ${price0(c.high)}${l.spot ? (l.spot >= c.low && l.spot <= c.high ? ", spot inside" : ", spot outside") : ""}`);
  else if (strikes.length) parts.push(`sold strike ${strikes.map((s) => s.label).join(", ")}`);
  else parts.push("no strike sold");
  parts.push(l.flip ? `flip ${price0(l.flip.price)}${l.flip.low && l.flip.high ? `, shed band ${price0(l.flip.low)}–${price0(l.flip.high)}` : ""}` : "flip unknown");
  parts.push(l.sma9 ? `SMA9 ${price0(l.sma9)}` : "SMA9 unknown");
  return `${parts.join("; ")}.`;
}

const legOf = (sp, type) => (sp?.legs ?? []).find((l) => l.option_type === type) ?? null;

/** The sides a strangle does not hold: from `missing`, else a side with no leg. */
export function missingSides(sp) {
  const named = (sp?.missing ?? []).map((m) => m.option_type);
  const absent = ["put", "call"].filter((t) => !legOf(sp, t));
  return [...new Set([...named, ...absent])];
}

/**
 * A strangle row's range bar, positions in % of the bar: the break-even
 * corridor (to the bar's edge on a side that cannot lose the premium), the put
 * and call ticks, the spot dot and a stub on each side with no leg; plus the
 * spot's distance to the nearest break-even. null without a spot.
 */
export function rangeStrip(sp) {
  const spot = sp?.spot;
  if (!positive(spot)) return null;
  const put = legOf(sp, "put")?.strike;
  const call = legOf(sp, "call")?.strike;
  const beLow = sp.break_even_low;
  const beHigh = sp.break_even_high;
  const values = [put, call, beLow, beHigh, spot].filter(positive);
  const lo0 = Math.min(...values);
  const hi0 = Math.max(...values);
  const pad = Math.max((hi0 - lo0) * 0.12, spot * 0.01);
  const min = lo0 - pad;
  const span = hi0 + pad - min;
  const at = (v) => ((v - min) / span) * 100;
  const left = positive(beLow) ? at(beLow) : 0;
  const right = positive(beHigh) ? at(beHigh) : 100;
  const missing = missingSides(sp);
  const bes = [beLow, beHigh].filter(positive);
  const inside = (!positive(beLow) || spot >= beLow) && (!positive(beHigh) || spot <= beHigh);
  const pct = bes.length ? Math.min(...bes.map((b) => (Math.abs(spot - b) / spot) * 100)) : null;
  return {
    corridor: { left, width: Math.max(right - left, 0) },
    put: positive(put) ? at(put) : null,
    call: positive(call) ? at(call) : null,
    spot: at(spot),
    stubs: missing.map((side) => (side === "put" ? { side, left: 0, width: 8 } : { side, left: 92, width: 8 })),
    inside,
    distancePct: pct,
    distanceText: pct == null ? "break-evens unknown" : `spot ${formatPct(pct, 1)} ${inside ? "from" : "past"} a break-even`,
  };
}

/** "62 % of the premium kept" and the bar's width (0–100); unknown without a premium. */
export function keptOf(sp) {
  if (!isNumber(sp?.earned_pct)) return { text: "premium kept: unknown: no premium recorded", bar: 0 };
  const pct = Math.round(sp.earned_pct);
  return { text: `${pct < 0 ? "−" : ""}${Math.abs(pct)} % of the premium kept`, bar: earnedBar(sp) };
}
