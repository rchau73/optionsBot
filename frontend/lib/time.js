// Display time zone. Everything the bot stores and serves is UTC; only the
// monitor converts, here, so changing the zone is one edit. Brazil has had no
// daylight saving since 2019, so BRT is always UTC−3.
export const DISPLAY_TZ = "America/Sao_Paulo";
export const DISPLAY_TZ_LABEL = "BRT";

const parts = new Intl.DateTimeFormat("en-CA", {
  timeZone: DISPLAY_TZ,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

/** { year, month, day, hour, minute, second } of t (ms, ISO or Date) in the display zone, or null. */
export function localParts(t) {
  const d = t instanceof Date ? t : new Date(t);
  if (Number.isNaN(d.getTime())) return null;
  return Object.fromEntries(parts.formatToParts(d).filter((p) => p.type !== "literal").map((p) => [p.type, p.value]));
}

/** "09:34:56" in the display zone (no label), or null. */
export function localClock(t) {
  const p = localParts(t);
  return p ? `${p.hour}:${p.minute}:${p.second}` : null;
}

/** "2026-10-02 09:34:56 BRT", or null. */
export function localDateTime(t) {
  const p = localParts(t);
  return p ? `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second} ${DISPLAY_TZ_LABEL}` : null;
}
