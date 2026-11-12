// Server-side only: "Download my data" (#750, LGPD art. 18 / GDPR art. 15) —
// the one JSON file of what the monitor holds about the signed-in person,
// built from the personal-data inventory (frontend/docs/personal-data.md).
// Only the caller's own rows: their users row without the password hash,
// their consent, their live sessions as GET /api/auth/sessions shows them,
// their survey answers and analytics events (the admin CLI's export code,
// lib/analytics/userrequest.js) and the audit lines where they are the
// `username`. Lines where they are only an admin's `target` stay out until
// counsel answers L14. A section that cannot be read says `unavailable` with
// its reason; it is never dropped. No hash, token or address leaves here.
import { AUDIT_EXPORT_MAX } from "../audit";
import { pseudonymousId } from "../analytics/config.js";
import { exportEventsOf } from "../analytics/retention.js";
import { surveyAnswersOf } from "../analytics/userrequest.js";
import { trailEntries } from "./auditread";
import { authLog } from "./log";

/** The file's format name and version, so a later reader knows what it holds. */
export const MY_DATA_FORMAT = "optionsbot-my-data";
export const MY_DATA_VERSION = 1;

/** The sections of the file, in order. */
export const MY_DATA_SECTIONS = ["account", "consent", "sessions", "survey_answers", "analytics_events", "audit_lines"];

/** The users columns the file holds; every other column is in WITHHELD_COLUMNS (tests/my-data check both against the schema). */
export const ACCOUNT_COLUMNS = [
  "id",
  "username",
  "role",
  "email",
  "created_at",
  "updated_at",
  "analytics_consent",
  "last_sign_in_at",
  "sign_in_count",
  "nps_last_shown_at",
  "nps_opted_out",
  "answers_last_shown_at",
];

/** The users columns never in the file: secrets. */
export const WITHHELD_COLUMNS = ["password_hash"];

/** Audit fields never in the file: the address, and the hash chain's own fields (lib/auth/auditchain.js). */
export const WITHHELD_AUDIT_FIELDS = ["ip", "prev", "line_hash"];

/** A section that cannot be read, with the reason the file shows. */
class Unavailable extends Error {}

const iso = (sec) => (typeof sec === "number" ? new Date(sec * 1000).toISOString() : sec);

/** The users row, times as ISO; Unix seconds in the database. */
function accountOf(store, userId) {
  const row = store.one(`SELECT ${ACCOUNT_COLUMNS.join(", ")} FROM users WHERE id = ?`, [userId]);
  if (!row) throw new Unavailable("your account row was not found");
  return Object.fromEntries(Object.entries(row).map(([k, v]) => [k, k.endsWith("_at") ? iso(v) : v]));
}

/** The analytics answer in words: yes, no, or not asked yet (NULL). */
function consentOf(store, userId) {
  const v = store.analyticsConsent(userId);
  return { analytics: v === true ? "yes" : v === false ? "no" : "not asked" };
}

/** The person's stored events, oldest first, as stored. */
function eventsOf(file, salt, userId) {
  if (!file) throw new Unavailable("this monitor stores no analytics events (its users are kept in memory)");
  if (!salt) throw new Unavailable("analytics is off on this monitor (ANALYTICS_SALT is not set), so stored events cannot be matched to you");
  return exportEventsOf(file, pseudonymousId(salt, userId)).map((line) => JSON.parse(line));
}

/** The audit lines naming the person as `username`, oldest first, the newest AUDIT_EXPORT_MAX at most. */
async function auditOf(file, username) {
  if (!file) throw new Unavailable("this monitor keeps no audit trail (its users are kept in memory)");
  const lines = [];
  const query = { user: username, action: null, from: null, to: null, before: null };
  for await (const { entry, damaged } of trailEntries(file, query)) {
    if (damaged) continue;
    // auditMatches compares lower-cased; usernames are stored lower-case, so this is the same person's name.
    if (entry.username !== username) continue;
    if (lines.length === AUDIT_EXPORT_MAX) break;
    lines.push(Object.fromEntries(Object.entries(entry).filter(([k]) => !WITHHELD_AUDIT_FIELDS.includes(k))));
  }
  return lines.reverse();
}

/** How many entries a section's data holds (an object counts as 1). */
function countOf(data) {
  return Array.isArray(data) ? data.length : 1;
}

/** Runs one section's read → { status: "ok", count, data } | { status: "unavailable", reason }. */
async function section(name, username, read) {
  try {
    const data = await read();
    return { status: "ok", count: countOf(data), data };
  } catch (err) {
    if (err instanceof Unavailable) return { status: "unavailable", reason: err.message };
    authLog("ERROR", "my data section unreadable", { username, section: name, error: err.message });
    return { status: "unavailable", reason: "it could not be read: an admin can see why in the monitor's logs" };
  }
}

/**
 * The file for `user` ({ id, username }). sessions: auth.listSessions(user, sid)
 * (no token, hash or address); eventsFile, salt, auditFile: as the monitor
 * runs (null = off). → { file, counts (section → count, or "unavailable") }
 */
export async function assembleMyData({ store, user, sessions, eventsFile, salt, auditFile, nowMs }) {
  const reads = {
    account: () => accountOf(store, user.id),
    consent: () => consentOf(store, user.id),
    sessions: () => sessions(),
    survey_answers: () => surveyAnswersOf(store, user.id),
    analytics_events: () => eventsOf(eventsFile, salt, user.id),
    audit_lines: () => auditOf(auditFile, user.username),
  };
  const sections = {};
  for (const name of MY_DATA_SECTIONS) sections[name] = await section(name, user.username, reads[name]);
  const counts = Object.fromEntries(MY_DATA_SECTIONS.map((n) => [n, sections[n].status === "ok" ? sections[n].count : "unavailable"]));
  const file = {
    format: MY_DATA_FORMAT,
    version: MY_DATA_VERSION,
    exported_at: new Date(nowMs).toISOString(),
    username: user.username,
    about:
      "What the monitor stores about you. Keep this file safe: it is yours. " +
      "It holds no password, password hash, token or address. " +
      "Audit lines where you are only the person an admin acted on are not included yet.",
    sections,
  };
  return { file, counts };
}
