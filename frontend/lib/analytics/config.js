// Server-side only: where the analytics events live and how a person's
// pseudonymous id is made (event plan §5). No imports but Node's own, with
// explicit extensions everywhere, so the admin CLI (scripts/analytics-user.mjs)
// loads it with plain `node` as well as the Next.js server.
import crypto from "node:crypto";
import path from "node:path";

export const MIN_SALT = 16;

/** EVENTS_PATH, default events.jsonl beside the user database; null (off) when the database is in memory. */
export function eventsPath(dbPath, raw = process.env.EVENTS_PATH) {
  if (raw) return raw;
  return dbPath === ":memory:" ? null : path.join(path.dirname(dbPath), "events.jsonl");
}

/** ANALYTICS_SALT when it is long enough, else null (collection off). */
export function analyticsSalt(raw = process.env.ANALYTICS_SALT) {
  return raw && raw.length >= MIN_SALT ? raw : null;
}

/** The pseudonymous user id: HMAC-SHA256 of the user's row id with the salt, first 16 hex characters. */
export function pseudonymousId(salt, userRowId) {
  return crypto.createHmac("sha256", salt).update(String(userRowId)).digest("hex").slice(0, 16);
}
