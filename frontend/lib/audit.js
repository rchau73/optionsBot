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
  "account_link_added",
  "account_link_removed",
  "signed_out_everywhere",
  "session_ended",
  "my_data_exported",
  "manual_close",
  "manual_rebalance",
  "futures_hedge_switched",
  "audit_exported",
  "parameter_change_previewed",
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

/** The most lines one CSV export holds (#702): the newest ones. */
export const AUDIT_EXPORT_MAX = 50000;

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

// Shown in their own columns, or the hash chain's own fields (#619,
// lib/auth/auditchain.js CHAIN_FIELDS: server-only, so named again here);
// everything else goes to "detail".
const NOT_DETAIL = new Set(["time", "action", "username", "role", "target", "bot", "prev", "line_hash"]);

/** The "detail" column of a line: its other fields as stored, `key=value`, in file order. */
export function auditDetail(entry) {
  return Object.entries(entry)
    .filter(([k]) => !NOT_DETAIL.has(k))
    .map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`)
    .join(" ");
}

/** The columns of the CSV export (#702): the page's, the hash chain's fields left out as there. */
export const AUDIT_CSV_COLUMNS = ["time", "person", "role", "action", "target", "detail"];

// A spreadsheet runs a cell starting with one of these as a formula; tab and
// carriage return count too, because a spreadsheet may strip them first.
const FORMULA_START = /^[=+\-@\t\r]/;
const AS_TEXT = "'"; // spreadsheets read a cell starting with an apostrophe as text

function quoted(text) {
  return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

/** One CSV cell: missing is empty, a formula is prefixed so it is read as text, quoted when it must be. */
export function auditCsvCell(value) {
  let text = "";
  if (typeof value === "string") text = value;
  else if (value !== null && value !== undefined) text = JSON.stringify(value);
  return quoted(FORMULA_START.test(text) ? AS_TEXT + text : text);
}

/**
 * The detail cell: the fields as one JSON object. It starts with "{", which no
 * spreadsheet runs, but a value starting like a formula prefixes the cell too:
 * whoever splits the JSON into columns later gets the warning with it.
 */
function detailCell(fields) {
  const json = JSON.stringify(fields);
  const formula = Object.values(fields).some((v) => typeof v === "string" && FORMULA_START.test(v));
  return quoted(formula ? AS_TEXT + json : json);
}

/** One line of the CSV file with its line end; `prev` and `line_hash` left out, as on the page. */
export function auditCsvRow(entry) {
  const detail = Object.fromEntries(Object.entries(entry).filter(([k]) => !NOT_DETAIL.has(k)));
  // The target column shows the bot when there is no target, as the page does; both kept when there are both.
  if (entry.target != null && entry.bot != null) detail.bot = entry.bot;
  const cells = [entry.time, entry.username, entry.role, entry.action, entry.target ?? entry.bot].map(auditCsvCell);
  return `${[...cells, detailCell(detail)].join(",")}\r\n`;
}

/** What the Audit page says after an export → { cut (true when lines were left out), text }. */
export function auditExportSummary({ lines, cut, skipped }) {
  const n = (k, one, many) => `${k.toLocaleString("en-US")} ${k === 1 ? one : many}`;
  const said = cut
    ? `The export was cut: the file holds the newest ${n(lines, "line", "lines")}. Narrow the filters to export the older ones.`
    : `Exported ${n(lines, "line", "lines")}.`;
  const damaged = skipped > 0 ? ` ${n(skipped, "damaged line was", "damaged lines were")} skipped.` : "";
  return { cut, text: said + damaged };
}

// What each break reason of GET /api/admin/audit/verify means, in words.
const BREAK_REASONS = {
  link: "a line before it was removed, reordered or edited",
  edited: "this line was edited",
  damaged: "this line is not a readable audit line",
  unchained: "this line has no link to the line before",
};

/**
 * The Audit page's chain line (#619) from the verify route's answer →
 * { broken, text, legacy (the legacy sentence or null) }; null when the trail is off.
 */
export function auditChainSummary(result, formatTime = (t) => t) {
  if (!result || result.off) return null;
  const n = (k, one, many) => `${k.toLocaleString("en-US")} ${k === 1 ? one : many}`;
  let legacy = null;
  if (result.legacy > 0) {
    legacy = result.legacy_before
      ? `${n(result.legacy, "legacy line", "legacy lines")} before ${formatTime(result.legacy_before)} ${result.legacy === 1 ? "is" : "are"} not covered.`
      : `${n(result.legacy, "legacy line is", "legacy lines are")} not covered: no line is chained yet.`;
  }
  if (result.ok) return { broken: false, text: `Chain intact — ${n(result.checked, "line", "lines")} checked.`, legacy };
  const b = result.first_break;
  const where = `line ${b.line.toLocaleString("en-US")}${b.archive ? ` of ${b.file}` : ""}`;
  const when = b.time ? ` (${formatTime(b.time)})` : "";
  const why = BREAK_REASONS[b.reason] ?? b.reason;
  return { broken: true, text: `Chain broken at ${where}${when}: ${why}. ${n(result.checked, "line", "lines")} before it checked intact.`, legacy };
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
