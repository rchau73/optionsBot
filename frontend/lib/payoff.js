// Pure helpers for the payoff charts (#391): one chart per strangle from
// /api/strangles/payoff (internal/api/payoff.go). The bot does the math —
// curves, break-evens, exit lines, stop levels, deltas — and these only
// word and place it. Missing data says "unknown: <reason>", never 0.
import { MISSING, formatCoin, formatDate, formatNumber, isNumber } from "./format";
import { formatSignedUSD } from "./summary";

/** Coin amounts on the charts: 4 decimals, signed ("+0.0058 BTC"). */
export const coin4 = (v, coin) => formatCoin(v, coin, 4);

/** The coin's price without cents: "82,400". */
export const price0 = (v) => formatNumber(v, 0);

/** The strangles of a payoff answer (already ordered by slot DTE), [] when none. */
export function payoffStrangles(payoff) {
  return Array.isArray(payoff?.strangles) ? payoff.strangles : [];
}

/** The chart of strangleId in a payoff answer, or null. */
export function findStrangle(payoff, strangleId) {
  return payoffStrangles(payoff).find((s) => s.strangle_id === strangleId) ?? null;
}

/** "BTC · 25 d slot · 2026-11-27". */
export function cardTitle(coin, sp) {
  return `${coin} · ${sp?.slot?.dte ?? MISSING} d slot · ${formatDate(sp?.expiry)}`;
}

/** "62 % of the premium earned · +0.0058 of +0.0094 BTC"; with no premium, unknown. */
export function earnedLine(sp, coin) {
  if (!isNumber(sp?.earned_pct)) return "Premium earned: unknown: no premium recorded";
  const pct = Math.round(sp.earned_pct);
  return `${pct < 0 ? "−" : ""}${Math.abs(pct)} % of the premium earned · ${coin4(sp.unrealised_coin, "")} of ${coin4(sp.premium_coin, coin)}`;
}

/** The earned share for the bar, clamped to 0–100 (a loss shows an empty bar and its sign in the words). */
export function earnedBar(sp) {
  return isNumber(sp?.earned_pct) ? Math.min(Math.max(sp.earned_pct, 0), 100) : 0;
}

/** "now 82,400 · +0.0058 BTC (+$478)". */
export function nowLabel(sp, coin) {
  if (!isNumber(sp?.spot) || sp.spot <= 0) return "now: unknown: no spot price";
  return `now ${price0(sp.spot)} · ${coin4(sp.unrealised_coin, coin)} (${formatSignedUSD(sp.unrealised_usd)})`;
}

/** Signed delta in coin: "−0.12 BTC". */
const delta = (v, coin) => formatCoin(v, coin, 2);

/** "Δ −0.12 BTC · −$99 per +1 %". */
export function deltaLine(coinDelta, usdPer1, coin) {
  if (!isNumber(coinDelta)) return "Δ unknown: no delta reported";
  return `Δ ${delta(coinDelta, coin)} · ${formatSignedUSD(usdPer1)} per +1 %`;
}

/** Each leg's delta: "put Δ +0.15 BTC · +$124 per +1 %". */
export function legDeltaLines(sp, coin) {
  return (sp?.legs ?? []).map((l) => ({
    key: l.position_id || l.instrument,
    type: l.option_type,
    text: `${l.option_type} ${deltaLine(l.position_delta, l.position_delta_usd_per_1pct, coin)}`,
  }));
}

/** "no call held: repair waiting (stopped out; cooldown until …)"; a side repair may sell now says so. */
export function missingLine(m) {
  if (!m) return null;
  if (!m.rule) return `no ${m.option_type} held: repair may sell it now${m.reason ? ` (${m.reason})` : ""}`;
  return `no ${m.option_type} held: repair waiting (${m.reason || m.rule})`;
}

/** The kinds of line a chart draws, in legend order. */
export const LINE_KINDS = [
  { kind: "strike", label: "Strikes" },
  { kind: "breakeven", label: "Break-evens at expiry" },
  { kind: "exit", label: "Exit lines (Δ)" },
  { kind: "stop", label: "Stop-loss levels" },
];

/**
 * Every vertical line of a chart: { key, kind, price, label, type }.
 * Strikes are "floor" (put) and "ceiling" (call).
 */
export function chartLines(sp) {
  const out = [];
  for (const l of sp?.legs ?? []) {
    const word = l.option_type === "put" ? "floor" : "ceiling";
    out.push({ key: `strike-${l.option_type}`, kind: "strike", type: l.option_type, price: l.strike, label: `${word} ${price0(l.strike)}` });
  }
  if (isNumber(sp?.break_even_low)) out.push({ key: "be-low", kind: "breakeven", price: sp.break_even_low, label: `break-even ${price0(sp.break_even_low)}` });
  if (isNumber(sp?.break_even_high)) out.push({ key: "be-high", kind: "breakeven", price: sp.break_even_high, label: `break-even ${price0(sp.break_even_high)}` });
  for (const l of sp?.legs ?? []) {
    if (isNumber(l.exit_line)) out.push({ key: `exit-${l.option_type}`, kind: "exit", type: l.option_type, price: l.exit_line, label: `${l.option_type} exit ${price0(l.exit_line)}` });
  }
  for (const l of sp?.legs ?? []) {
    if (isNumber(l.stop_level)) out.push({ key: `stop-${l.option_type}`, kind: "stop", type: l.option_type, price: l.stop_level, label: `${l.option_type} stop ${price0(l.stop_level)}` });
  }
  return out;
}

/**
 * The chart's geometry in a width × height box: x/y scales, the at-expiry and
 * today paths (SVG "x,y" lists), the today area (between 0 and the today
 * curve) and the zero line's y. Null when there are no points.
 */
export function chartGeometry(sp, { width = 640, height = 300, padX = 8, top = 28, bottom = 26 } = {}) {
  const pts = (sp?.points ?? []).filter((p) => isNumber(p.price) && isNumber(p.expiry_pnl));
  if (pts.length < 2) return null;
  const lo = pts[0].price;
  const hi = pts[pts.length - 1].price;
  const values = pts.flatMap((p) => (isNumber(p.today_pnl) ? [p.expiry_pnl, p.today_pnl] : [p.expiry_pnl]));
  const ymax = Math.max(...values, 0);
  const ymin = Math.min(...values, 0);
  const span = ymax - ymin || 1;
  const yTop = ymax + span * 0.08;
  const yBottom = ymin - span * 0.04;
  const x = (s) => padX + ((s - lo) / (hi - lo)) * (width - 2 * padX);
  const y = (v) => top + ((yTop - v) / (yTop - yBottom)) * (height - top - bottom);
  const path = (key) =>
    pts
      .filter((p) => isNumber(p[key]))
      .map((p) => `${x(p.price).toFixed(1)},${y(p[key]).toFixed(1)}`)
      .join(" ");
  const today = sp.today ? path("today_pnl") : "";
  const zero = y(0);
  const area = today ? `${x(lo).toFixed(1)},${zero.toFixed(1)} ${today} ${x(hi).toFixed(1)},${zero.toFixed(1)}` : "";
  return { lo, hi, x, y, zero, expiry: path("expiry_pnl"), today, area, width, height, top, bottom: height - bottom };
}

/** The today curve's value at spot (the book's unrealised P&L), or null. */
export function todayAtSpot(sp) {
  return sp?.today && isNumber(sp?.unrealised_coin) ? sp.unrealised_coin : null;
}

/**
 * Where the today curve is above / below zero: [{ from, to, gain }] over
 * the chart's prices, for the text alternative.
 */
export function todayZones(sp) {
  const pts = (sp?.points ?? []).filter((p) => isNumber(p.today_pnl));
  const zones = [];
  for (const p of pts) {
    const gain = p.today_pnl >= 0;
    const last = zones[zones.length - 1];
    if (last && last.gain === gain) last.to = p.price;
    else zones.push({ from: p.price, to: p.price, gain });
  }
  return zones;
}

/**
 * The chart in words, for screen readers: the zones of the today curve, the
 * break-evens and the current point.
 */
export function textAlternative(sp, coin) {
  const parts = [`Payoff of the ${cardTitle(coin, sp)} strangle, in ${coin} for each ${coin} price.`];
  parts.push(`At expiry it keeps up to ${coin4(sp?.premium_coin, coin)}${isNumber(sp?.break_even_low) || isNumber(sp?.break_even_high) ? `, zero at the break-evens ${[sp.break_even_low, sp.break_even_high].filter(isNumber).map(price0).join(" and ")}` : ""}, a loss beyond.`);
  if (sp?.today) {
    const zones = todayZones(sp).map((z) => `${z.gain ? "gain" : "loss"} from ${price0(z.from)} to ${price0(z.to)}`);
    if (zones.length) parts.push(`Bought back today: ${zones.join("; ")}.`);
  } else {
    parts.push(`Today's curve: ${sp?.today_reason || "unknown"}.`);
  }
  parts.push(`${nowLabel(sp, coin)}${isNumber(sp?.unrealised_coin) ? (sp.unrealised_coin >= 0 ? ", in the gain area" : ", in the loss area") : ""}.`);
  for (const m of sp?.missing ?? []) parts.push(`${missingLine(m)}.`);
  return parts.join(" ");
}
