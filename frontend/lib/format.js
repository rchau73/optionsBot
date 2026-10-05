import { DISPLAY_TZ_LABEL, localClock, localDate, localDateTime } from "./time";
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

/** Time left until iso: "in 1m 05s", "due" once passed, "—" when unknown. */
export function formatCountdown(iso, now = Date.now()) {
  if (!iso || iso.startsWith("0001-")) return MISSING;
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return MISSING;
  const s = Math.ceil((t - now) / 1000);
  if (s <= 0) return "due";
  if (s < 60) return `in ${s}s`;
  return `in ${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`;
}

/** Clock time in the display zone (BRT): "09:34:56 BRT". */
export function formatTime(iso) {
  if (!iso) return MISSING;
  const clock = localClock(iso);
  return clock ? `${clock} ${DISPLAY_TZ_LABEL}` : MISSING;
}

/** Date in the display zone: "2026-10-02". */
export function formatDate(iso) {
  if (!iso || (typeof iso === "string" && iso.startsWith("0001-"))) return MISSING;
  return localDate(iso) ?? MISSING;
}

/** Full timestamp in the display zone: "2026-10-02 09:34:56 BRT". */
export function formatDateTime(iso) {
  if (!iso || (typeof iso === "string" && iso.startsWith("0001-"))) return MISSING;
  return localDateTime(iso) ?? MISSING;
}

export function slotLabel(slot) {
  if (!slot) return "—";
  return `${slot.dte}d · ${Number(slot.delta).toFixed(2)}Δ`;
}
