// The role matrix (#490): every monitor route that changes state, who may call
// it and the audit line it leaves. The route handlers enforce it (requireRole,
// adminRequest, proxy.js); __tests__/role-matrix.test.js calls each route as
// every caller below and fails on a route that is missing here, so a new
// state-changing route cannot ship unguarded.

/** Who may call a route: the three roles, plus "anonymous" (no session) for sign-in itself. */
export const ROLES = ["admin", "manager", "user", "anonymous"];

/** Every way a request reaches the monitor; what the matrix test tries on each route. */
export const CALLERS = ["admin", "manager", "user", "anonymous", "service_token", "expired_session"];

/** HTTP methods that change state; a route file exporting one of them must be listed below. */
export const STATE_METHODS = ["POST", "PUT", "PATCH", "DELETE"];

const SIGNED_IN = ["admin", "manager", "user"];
const MANAGERS = ["admin", "manager"];
const ADMINS = ["admin"];
const ANYONE = [...SIGNED_IN, "anonymous"];

/**
 * route: "METHOD /api/path" as the file sits under app/api ([bot], [id] kept).
 * roles: who may call it (ROLES). audit: the audit.jsonl action an allowed
 * call writes with the person's username, or null with `why` it writes none.
 * bot: the bot API route it forwards to (internal/api/routes_actions.go).
 * linked: the route targets a bot, so a manager linked to other accounts is
 * refused (#760, lib/auth/accountLinks.js); every [bot] route must say so.
 */
export const STATE_CHANGING_ROUTES = [
  { route: "DELETE /api/admin/users/[id]", roles: ADMINS, audit: "user_deleted" },
  { route: "DELETE /api/auth/account", roles: SIGNED_IN, audit: "account_deleted" },
  { route: "DELETE /api/auth/sessions/[id]", roles: SIGNED_IN, audit: "session_ended" },
  { route: "PATCH /api/admin/users/[id]", roles: ADMINS, audit: "role_changed" },
  { route: "POST /api/admin/users", roles: ADMINS, audit: "user_created" },
  { route: "POST /api/admin/users/[id]/password", roles: ADMINS, audit: "password_reset" },
  { route: "POST /api/admin/users/[id]/reset-link", roles: ADMINS, audit: "reset_link_issued" },
  { route: "POST /api/admin/users/[id]/signout", roles: ADMINS, audit: "signed_out_everywhere" },
  { route: "POST /api/assistant", roles: SIGNED_IN, audit: null, why: "each question is logged with the person in assistant.jsonl" },
  { route: "POST /api/assistant/feedback", roles: SIGNED_IN, audit: null, why: "the person who asked says whether the answer helped; logged with the person in assistant.jsonl" },
  { route: "POST /api/assistant/flag", roles: SIGNED_IN, audit: null, why: "the flag is logged with the person in assistant.jsonl" },
  { route: "POST /api/assistant/requests", roles: SIGNED_IN, audit: null, why: "the request is queued with the person in requests.jsonl" },
  { route: "POST /api/auth/login", roles: ANYONE, audit: "signed_in" },
  { route: "POST /api/auth/logout", roles: SIGNED_IN, audit: "signed_out" },
  { route: "POST /api/auth/password", roles: SIGNED_IN, audit: "password_changed" },
  { route: "POST /api/auth/refresh", roles: ANYONE, audit: null, why: "renews the caller's own session; changes nothing a person decides" },
  { route: "POST /api/auth/reset", roles: ANYONE, audit: "password_reset_by_link" },
  { route: "POST /api/bots/[bot]/close", roles: MANAGERS, audit: "manual_close", bot: "POST /api/positions/close", linked: true },
  { route: "POST /api/bots/[bot]/hedge", roles: ADMINS, audit: "futures_hedge_switched", bot: "POST /api/hedge", linked: true },
  { route: "POST /api/bots/[bot]/rebalance", roles: MANAGERS, audit: "manual_rebalance", bot: "POST /api/rebalance", linked: true },
  { route: "POST /api/consent", roles: SIGNED_IN, audit: "analytics_consent_changed" },
  { route: "POST /api/events", roles: SIGNED_IN, audit: null, why: "usage events, many a minute, stored pseudonymous in events.jsonl" },
  { route: "POST /api/lab/requests", roles: MANAGERS, audit: "lab_request" },
  { route: "POST /api/lab/validate", roles: MANAGERS, audit: "lab_validate" },
  { route: "POST /api/notifications", roles: SIGNED_IN, audit: null, why: "the person's own read marks and settings" },
  { route: "POST /api/survey", roles: SIGNED_IN, audit: "survey_answered" },
  { route: "POST /api/survey/answers", roles: SIGNED_IN, audit: "answers_survey_shown" },
  { route: "POST /api/survey/nps", roles: SIGNED_IN, audit: "nps_survey_changed" },
  { route: "PUT /api/admin/users/[id]/accounts", roles: ADMINS, audit: "account_link_added" },
];

/**
 * The read matrix (#782): every app/api route file that exports GET, who may
 * read it and why. __tests__/read-matrix.test.js calls each as every caller
 * (CALLERS) and fails on a GET route missing here, so a new read cannot show
 * admin-only data to a manager or a user by accident.
 * roles: who may read it (ROLES). token: the read-only MONITOR_API_TOKEN may
 * read it too (proxy.js serviceTokenOk: /api/bots only). why: one line.
 * audit: the audit.jsonl action an allowed read writes (#682), when it writes one;
 * a refused read writes nothing.
 */
export const READ_ROUTES = [
  { route: "GET /api/admin/analytics", roles: ADMINS, why: "weekly usage reports and the consent counts of every user" },
  { route: "GET /api/admin/analytics/delivery", roles: ADMINS, why: "the delivery report of the whole team" },
  { route: "GET /api/admin/audit", roles: ADMINS, why: "the audit trail names every person and what they did" },
  { route: "GET /api/admin/audit/export", roles: ADMINS, why: "the whole audit trail as a file (#702)" },
  { route: "GET /api/admin/audit/verify", roles: ADMINS, why: "the audit trail's hash chain (#619)" },
  { route: "GET /api/admin/users", roles: ADMINS, why: "every account, its role and email" },
  { route: "GET /api/assistant", roles: SIGNED_IN, why: "the Ask panel's state for the person asking" },
  { route: "GET /api/assistant/requests", roles: SIGNED_IN, why: "the person's own requests, never anyone else's" },
  { route: "GET /api/auth/my-data", roles: SIGNED_IN, audit: "my_data_exported", why: "the person's own data (#750)" },
  { route: "GET /api/auth/sessions", roles: SIGNED_IN, why: "the person's own sessions (#584)" },
  { route: "GET /api/bots", roles: SIGNED_IN, token: true, why: "the bots' names and the poll interval; the deploy health check reads it" },
  { route: "GET /api/bots/[bot]/[...path]", roles: SIGNED_IN, token: true, why: "the bot data every page shows (allow-listed endpoints); the health check and the agents read it" },
  { route: "GET /api/bots/[bot]/hedge", roles: SIGNED_IN, token: true, why: "the hedge experiment's state, shown on the How it works page to every role" },
  { route: "GET /api/compare", roles: MANAGERS, why: "the comparison reports, a manager's research (#302)" },
  { route: "GET /api/compare/[id]", roles: MANAGERS, why: "one comparison report (#302)" },
  { route: "GET /api/consent", roles: SIGNED_IN, why: "the person's own analytics consent" },
  { route: "GET /api/help/tips", roles: SIGNED_IN, why: "the settings' tips, the same for every role (#588)" },
  { route: "GET /api/help/videos", roles: SIGNED_IN, why: "the how-to videos, the same for every role (#611)" },
  { route: "GET /api/lab/runs", roles: MANAGERS, why: "the lab's runs, a manager's research" },
  { route: "GET /api/lab/runs/[id]", roles: MANAGERS, why: "one lab run" },
  { route: "GET /api/lab/runs/[id]/preview", roles: MANAGERS, audit: "parameter_change_previewed", why: "a parameter change previewed on an account (#682)" },
  { route: "GET /api/notifications", roles: SIGNED_IN, why: "the person's own notifications and settings" },
  { route: "GET /api/survey/answers", roles: SIGNED_IN, why: "whether the person's Answers card is due" },
  { route: "GET /api/survey/nps", roles: SIGNED_IN, why: "whether the person's NPS card is due" },
];

/**
 * Read-only routes that only some roles (or only a live session, #584) may call (#539): the role matrix test
 * calls each as every caller too, and checks the route file exports GET only. Rows of READ_ROUTES.
 */
const GUARDED = ["GET /api/admin/audit/export", "GET /api/admin/audit/verify", "GET /api/auth/my-data", "GET /api/auth/sessions", "GET /api/lab/runs", "GET /api/lab/runs/[id]", "GET /api/lab/runs/[id]/preview"];
export const ROLE_GUARDED_READS = READ_ROUTES.filter((r) => GUARDED.includes(r.route));

/** The table's route for an app/api route file and method: "app/api/consent/route.js", "POST" → "POST /api/consent". */
export function routeKey(file, method) {
  const dir = file.replace(/\\/g, "/").replace(/^.*?app\/api/, "/api").replace(/\/route\.js$/, "");
  return `${method} ${dir}`;
}

/** The state-changing routes found that the table does not list, by name (empty = every route is guarded). */
export function unlistedRoutes(found, table = STATE_CHANGING_ROUTES) {
  const listed = new Set(table.map((r) => r.route));
  return found.filter((r) => !listed.has(r)).sort();
}

/** True when `caller` (CALLERS) may call `entry`: a role it lists, or anyone when it lists "anonymous". */
export function callerAllowed(entry, caller) {
  if (entry.roles.includes("anonymous")) return true;
  return entry.roles.includes(caller);
}

/** True when `caller` may read the READ_ROUTES `entry`: callerAllowed, or the service token where the row says `token`. */
export function readAllowed(entry, caller) {
  if (caller === "service_token" && entry.token) return true;
  return callerAllowed(entry, caller);
}
