// Pure formatting helpers. Every number on screen carries its unit.

const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 });
const usd2 = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 2 });

export function isNumber(v) {
  return typeof v === "number" && Number.isFinite(v);
}

/** "—" for missing data, never a silent zero. */
export const MISSING = "—";

export function formatUSD(v, { cents = false } = {}) {
  if (!isNumber(v)) return MISSING;
  return (cents ? usd2 : usd).format(v);
}

/** Signed coin amount, e.g. "+0.00125 BTC". */
export function formatCoin(v, unit = "", digits = 5) {
  if (!isNumber(v)) return MISSING;
  const sign = v > 0 ? "+" : v < 0 ? "−" : "";
  return `${sign}${Math.abs(v).toFixed(digits)}${unit ? ` ${unit}` : ""}`;
}

export function formatPrice(v, digits = 4) {
  return isNumber(v) ? v.toFixed(digits) : MISSING;
}

export function formatPct(v, digits = 1, { signed = false } = {}) {
  if (!isNumber(v)) return MISSING;
  const sign = signed && v > 0 ? "+" : "";
  return `${sign}${v.toFixed(digits)}%`;
}

export function formatNumber(v, digits = 2) {
  return isNumber(v) ? v.toLocaleString("en-US", { maximumFractionDigits: digits, minimumFractionDigits: digits }) : MISSING;
}

/** Seconds since an ISO timestamp, or null when unknown. */
export function ageSeconds(iso, now = Date.now()) {
  if (!iso || iso.startsWith("0001-")) return null;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : Math.max(0, (now - t) / 1000);
}

export function formatAge(seconds) {
  if (seconds == null) return "never";
  if (seconds < 60) return `${Math.floor(seconds)}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  return `${Math.floor(seconds / 3600)}h ago`;
}

export function formatTime(iso) {
  if (!iso) return MISSING;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? MISSING : d.toISOString().slice(11, 19) + " UTC";
}

export function slotLabel(slot) {
  if (!slot) return "—";
  return `${slot.dte}d · ${Number(slot.delta).toFixed(2)}Δ`;
}
