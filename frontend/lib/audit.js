// Pure logic of the Audit page (#308, #249): the known audit actions, the
// query rules of GET /api/admin/audit and how one stored line is shown. The
// lines themselves are written by lib/auth/service.js, lib/analytics/* and the
// close / rebalance routes; this file only reads their shape.

/** Every action written to audit.jsonl (frontend/docs/sign-in.md, the Audit part). */
export const AUDIT_ACTIONS = [
  "signed_in",
  "sign_in_failed",
  "locked",
  "signed_out",
  "password_changed",
  "password_change_failed",
  "user_created",
  "role_changed",
  "password_reset",
  "reset_link_issued",
  "password_reset_by_link",
  "user_deleted",
  "account_deleted",
  "signed_out_everywhere",
  "manual_close",
  "manual_rebalance",
  "events_pruned",
  "events_prune_failed",
  "events_deleted",
  "events_delete_failed",
  "events_exported",
  "analytics_consent_changed",
  "survey_answers_deleted",
  "survey_answers_delete_failed",
  "survey_answered",
  "nps_survey_changed",
  "answers_survey_shown",
];

/** Lines a page lists unless `limit` says otherwise, and the most it may say. */
export const AUDIT_PAGE = 100;
export const AUDIT_MAX = 1000;

const DAY = /^\d{4}-\d{2}-\d{2}$/;
/** True for a real calendar day YYYY-MM-DD (2026-02-30 is not). */
function validDay(s) {
  if (!DAY.test(s)) return false;
  const d = new Date(`${s}T00:00:00Z`);
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === s;
}

/**
 * The filters of an audit request from its query string (URLSearchParams):
 * user (the person who acted, `username` of the line), action (one of
 * AUDIT_ACTIONS), from / to (UTC days, inclusive), before (the cursor of the
 * previous page) and limit (1–AUDIT_MAX, default AUDIT_PAGE).
 * → { ok: true, query } | { ok: false, error }
 */
export function parseAuditQuery(params) {
  const get = (k) => (params.get(k) ?? "").trim();
  const query = { user: null, action: null, from: null, to: null, before: null, limit: AUDIT_PAGE };

  const user = get("user").toLowerCase();
  if (user.length > 64) return { ok: false, error: "user: at most 64 characters" };
  query.user = user || null;

  const action = get("action");
  if (action && !AUDIT_ACTIONS.includes(action)) return { ok: false, error: `action: one of ${AUDIT_ACTIONS.join(", ")}` };
  query.action = action || null;

  for (const k of ["from", "to"]) {
    const day = get(k);
    if (day && !validDay(day)) return { ok: false, error: `${k}: a UTC day, YYYY-MM-DD` };
    query[k] = day || null;
  }
  if (query.from && query.to && query.from > query.to) return { ok: false, error: "from is after to" };

  const before = get("before");
  if (before) {
    if (!/^\d{1,15}$/.test(before)) return { ok: false, error: "before: the cursor of a previous page" };
    query.before = Number(before);
  }
  const limit = get("limit");
  if (limit) {
    if (!/^\d{1,4}$/.test(limit) || Number(limit) < 1 || Number(limit) > AUDIT_MAX) return { ok: false, error: `limit: 1 to ${AUDIT_MAX}` };
    query.limit = Number(limit);
  }
  return { ok: true, query };
}

/** True when a parsed line passes the filters (days compared on the UTC date of `time`). */
export function auditMatches(entry, { user, action, from, to }) {
  if (user && String(entry.username ?? "").toLowerCase() !== user) return false;
  if (action && entry.action !== action) return false;
  const day = typeof entry.time === "string" ? entry.time.slice(0, 10) : "";
  if (from && !(day >= from)) return false;
  if (to && !(day <= to)) return false;
  return true;
}

// Shown in their own columns; everything else goes to "detail".
const COLUMNS = new Set(["time", "action", "username", "role", "target", "bot"]);

/** The "detail" column of a line: its other fields as stored, `key=value`, in file order. */
export function auditDetail(entry) {
  return Object.entries(entry)
    .filter(([k]) => !COLUMNS.has(k))
    .map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
    .join(" ");
}

/** The query string of an audit request from the page's filters (empty ones left out). */
export function auditQueryString({ user = "", action = "", from = "", to = "", before = null } = {}) {
  const q = new URLSearchParams();
  if (user.trim()) q.set("user", user.trim());
  if (action) q.set("action", action);
  if (from) q.set("from", from);
  if (to) q.set("to", to);
  if (before !== null && before !== undefined) q.set("before", String(before));
  return q.toString();
}
