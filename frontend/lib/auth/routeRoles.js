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
 */
export const STATE_CHANGING_ROUTES = [
  { route: "DELETE /api/admin/users/[id]", roles: ADMINS, audit: "user_deleted" },
  { route: "PATCH /api/admin/users/[id]", roles: ADMINS, audit: "role_changed" },
  { route: "POST /api/admin/users", roles: ADMINS, audit: "user_created" },
  { route: "POST /api/admin/users/[id]/password", roles: ADMINS, audit: "password_reset" },
  { route: "POST /api/admin/users/[id]/reset-link", roles: ADMINS, audit: "reset_link_issued" },
  { route: "POST /api/admin/users/[id]/signout", roles: ADMINS, audit: "signed_out_everywhere" },
  { route: "POST /api/assistant", roles: SIGNED_IN, audit: null, why: "each question is logged with the person in assistant.jsonl" },
  { route: "POST /api/assistant/flag", roles: SIGNED_IN, audit: null, why: "the flag is logged with the person in assistant.jsonl" },
  { route: "POST /api/assistant/requests", roles: SIGNED_IN, audit: null, why: "the request is queued with the person in requests.jsonl" },
  { route: "POST /api/auth/login", roles: ANYONE, audit: "signed_in" },
  { route: "POST /api/auth/logout", roles: SIGNED_IN, audit: "signed_out" },
  { route: "POST /api/auth/password", roles: SIGNED_IN, audit: "password_changed" },
  { route: "POST /api/auth/refresh", roles: ANYONE, audit: null, why: "renews the caller's own session; changes nothing a person decides" },
  { route: "POST /api/auth/reset", roles: ANYONE, audit: "password_reset_by_link" },
  { route: "POST /api/bots/[bot]/close", roles: MANAGERS, audit: "manual_close", bot: "POST /api/positions/close" },
  { route: "POST /api/bots/[bot]/rebalance", roles: MANAGERS, audit: "manual_rebalance", bot: "POST /api/rebalance" },
  { route: "POST /api/consent", roles: SIGNED_IN, audit: "analytics_consent_changed" },
  { route: "POST /api/events", roles: SIGNED_IN, audit: null, why: "usage events, many a minute, stored pseudonymous in events.jsonl" },
  { route: "POST /api/notifications", roles: SIGNED_IN, audit: null, why: "the person's own read marks and settings" },
  { route: "POST /api/survey", roles: SIGNED_IN, audit: "survey_answered" },
  { route: "POST /api/survey/answers", roles: SIGNED_IN, audit: "answers_survey_shown" },
  { route: "POST /api/survey/nps", roles: SIGNED_IN, audit: "nps_survey_changed" },
];

/**
 * Read-only routes that only some roles may call (#539): the matrix test
 * calls each as every caller too, and checks the route file exports GET only.
 */
export const ROLE_GUARDED_READS = [
  { route: "GET /api/lab/runs", roles: MANAGERS },
  { route: "GET /api/lab/runs/[id]", roles: MANAGERS },
];

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
