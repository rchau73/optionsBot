import { MISSING, isNumber } from "./format";
// Pure helpers for the investor summary (/api/summary, #246). Every number
// keeps its sign: a loss reads "−", a gain "+", in the same type as each other.

const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 });
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** Signed USD, e.g. "+$1,235", "−$40", "$0"; "—" when missing. */
export function formatSignedUSD(v) {
  if (!isNumber(v)) return MISSING;
  const rounded = Math.round(v);
  const sign = rounded > 0 ? "+" : rounded < 0 ? "−" : "";
  return `${sign}${usd.format(Math.abs(rounded))}`;
}

/** Signed percentage, e.g. "+1.2%", "−3.4%". */
export function formatSignedPct(v, digits = 1) {
  if (!isNumber(v)) return MISSING;
  if (Number(v.toFixed(digits)) === 0) return `${(0).toFixed(digits)}%`;
  return `${v > 0 ? "+" : "−"}${Math.abs(v).toFixed(digits)}%`;
}

/** "2026-10" → "Oct 2026"; anything else is shown as given. */
export function monthLabel(month) {
  const m = /^(\d{4})-(\d{2})$/.exec(month ?? "");
  if (!m || Number(m[2]) < 1 || Number(m[2]) > 12) return month ?? MISSING;
  return `${MONTHS[Number(m[2]) - 1]} ${m[1]}`;
}

/** profit | loss | flat, for the colour that goes with the sign (never instead of it). */
export function toneOf(v) {
  if (!isNumber(v) || v === 0) return "flat";
  return v > 0 ? "profit" : "loss";
}

/**
 * Months to show, newest first, the current one flagged. A month without
 * closes says so instead of showing zeros.
 */
export function incomeRows(income) {
  const months = income?.months ?? [];
  const last = months.length - 1;
  return months
    .map((m, i) => ({
      month: m.month,
      label: monthLabel(m.month),
      current: i === last,
      closes: m.closes ?? 0,
      coin: m.realised_coin,
      usd: m.realised_usd,
    }))
    .reverse();
}

/** True once the journal has at least one close in the months shown. */
export function hasIncome(income) {
  return (income?.months ?? []).some((m) => (m.closes ?? 0) > 0);
}

/**
 * The modelled one-day move each way as P&L lines: the API's loss turned into
 * a signed result (a loss of 0.05 BTC reads −0.05 BTC). pct is null when the
 * equity is unknown.
 */
export function worstCaseRows(wc) {
  if (!wc?.available) return [];
  const row = (key, label, shock) => ({
    key,
    label,
    spot: shock?.spot,
    coin: isNumber(shock?.loss_coin) ? -shock.loss_coin : null,
    usd: isNumber(shock?.loss_usd) ? -shock.loss_usd : null,
    pct: isNumber(shock?.pct_of_equity) ? -shock.pct_of_equity : null,
  });
  return [row("down", "falls", wc.down), row("up", "rises", wc.up)];
}

/** "2 strangles · 4 legs (2 calls, 2 puts)". */
export function holdingLine(h) {
  if (!h) return MISSING;
  const plural = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
  return `${plural(h.strangles ?? 0, "strangle")} · ${plural(h.legs ?? 0, "leg")} (${plural(h.calls ?? 0, "call")}, ${plural(h.puts ?? 0, "put")})`;
}
