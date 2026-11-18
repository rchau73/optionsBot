// How long a session may go unused before it ends by itself (#821): fixed
// values, not settings. Pure and free of Node, so the login page uses the
// same words as the refresh route.

const HOUR = 3600;

/** The idle limit per role, in hours: 12 h for an admin, 14 days (the session's lifetime) otherwise. */
export const IDLE_LIMIT_HOURS = { admin: 12, manager: 14 * 24, user: 14 * 24 };

/** The idle limit of a role in hours (an unknown role gets the shortest). */
export function idleLimitHours(role) {
  return IDLE_LIMIT_HOURS[role] ?? IDLE_LIMIT_HOURS.admin;
}

/**
 * Whether a session of `role` last used at `lastUsedSec` has been unused for
 * longer than its limit at `nowMs` → { idleHours (whole hours unused), limitHours } or null.
 */
export function idleExpiry(role, lastUsedSec, nowMs) {
  const limitHours = idleLimitHours(role);
  const idleSec = Math.floor(nowMs / 1000) - lastUsedSec;
  if (idleSec <= limitHours * HOUR) return null;
  return { idleHours: Math.floor(idleSec / HOUR), limitHours };
}

/** A limit in plain words: "12 hours", "14 days". */
export function limitWords(hours) {
  if (hours % 24 === 0 && hours >= 48) return `${hours / 24} days`;
  return hours === 1 ? "1 hour" : `${hours} hours`;
}

/**
 * The login page's sentence for a session ended by inactivity, from the
 * `idle` query value (the limit in hours), or null for anything that is not
 * one of the limits (an ordinary expiry, a sign-out, a made-up value).
 */
export function idleSignOutMessage(idle) {
  const hours = Number(idle);
  if (!Object.values(IDLE_LIMIT_HOURS).includes(hours)) return null;
  return `You were signed out because the session was not used for ${limitWords(hours)}.`;
}
