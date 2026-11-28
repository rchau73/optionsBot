// What the signed-in person's own role may see and do (#911), as citable
// passages for a question about access ("why can't I close this position?",
// "what can I change?", "who can see the Audit page?"). Built from the role
// matrix (lib/auth/routeRoles.js: the write and read tables the routes
// enforce) and the person's own role and account links — never anyone else's:
// no username, email or other person's data enters a passage. The passages
// say what the role allows and, for what it does not, who may and who to ask;
// never a way around it. Roles and routes stay enforced on the server only:
// these passages explain them, they grant nothing.
import { READ_ROUTES, STATE_CHANGING_ROUTES } from "@/lib/auth/routeRoles";

// The three roles a person may hold; anything else (a golden-set run's
// "test_run", a missing role) gets no access passage.
const PERSON_ROLES = ["admin", "manager", "user"];

/**
 * What a person may do in the monitor, each one action in plain words (`short`
 * in the you:role list, `words` in its own passage) and the routes behind it (rows of STATE_CHANGING_ROUTES or READ_ROUTES). Who may do
 * it is read from those rows, so the passages follow the matrix: the routes of
 * one action must allow the same roles (__tests__/assistant-access.test.js).
 * linked: the action targets a bot, so a manager with account links does it
 * only on the bots of the linked accounts (#760, lib/auth/accountLinks.js).
 */
export const ACCESS_ACTIONS = [
  {
    key: "see",
    short: "see the monitor and ask the assistant",
    words: "See the monitor's pages for every bot, ask the assistant, see your own sessions and download your own data",
    routes: ["GET /api/bots/[bot]/[...path]", "POST /api/assistant", "GET /api/auth/sessions", "GET /api/auth/my-data"],
  },
  {
    key: "close",
    short: "close a position (manual close)",
    words: "Close a position: buy back a short leg at market from the Positions page (manual close)",
    routes: ["POST /api/bots/[bot]/close"],
    linked: true,
  },
  {
    key: "rebalance",
    short: "run a manual rebalance",
    words: "Run a manual rebalance, or its dry run",
    routes: ["POST /api/bots/[bot]/rebalance"],
    linked: true,
  },
  {
    key: "hedge",
    short: "switch the futures hedge experiment",
    words: "Switch the futures hedge experiment on or off (Account page; testnet only)",
    routes: ["POST /api/bots/[bot]/hedge"],
    linked: true,
  },
  {
    key: "lab",
    short: "use the Lab",
    words: "Use the Lab: read its comparison runs, check a proposed setting value and request a comparison run (nothing is applied)",
    routes: ["GET /api/lab/runs", "POST /api/lab/validate", "POST /api/lab/requests"],
  },
  {
    key: "lab-confirm",
    short: "confirm or drop a Lab proposal",
    words: "Confirm a Lab run's previewed setting change, which records it and applies nothing, or drop its proposal",
    routes: ["POST /api/lab/runs/[id]/apply", "POST /api/lab/runs/[id]/drop"],
    linked: true,
  },
  {
    key: "users",
    short: "manage users and roles on the Users page",
    words: "Manage users on the Users page: add, edit and delete people, change a role, reset a password or issue a reset link, and link a manager to accounts",
    routes: ["GET /api/admin/users", "POST /api/admin/users", "PATCH /api/admin/users/[id]", "DELETE /api/admin/users/[id]", "POST /api/admin/users/[id]/password", "POST /api/admin/users/[id]/reset-link", "PUT /api/admin/users/[id]/accounts"],
  },
  {
    key: "audit",
    short: "read the audit trail on the Audit page",
    words: "Read the audit trail on the Audit page, check its hash chain and export it",
    routes: ["GET /api/admin/audit", "GET /api/admin/audit/verify", "GET /api/admin/audit/export"],
  },
  {
    key: "analytics",
    short: "read the usage report on the Analytics page",
    words: "Read the weekly usage report on the Analytics page",
    routes: ["GET /api/admin/analytics"],
  },
];

// A question about what the person's role may see or do (#911): only these
// pay for the access passages.
const ACCESS_PATTERNS = [
  /\b(can'?t|cannot|can\s+not|couldn'?t|unable\s+to)\s+(i\s+)?(see|find|open|use|click|press|close|change|edit|rebalance|switch|confirm|apply|drop|request|run|add|delete|create|reset|read|export|link|do)\b/i,
  /\bwhy\s+(can'?t|cannot|can\s+not|am\s+i\s+not\s+allowed\s+to)\s+(i|we)\b/i,
  /\b(can|may)\s+(i|we)\s+(close|change|edit|see|open|use|rebalance|switch|confirm|apply|drop|request|run|add|delete|create|reset|read|export|link)\b/i,
  /\bwhat\s+(can|may)\s+(i|we)\s+(do|see|change|use)\b/i,
  /\bam\s+i\s+(not\s+)?(allowed|able|permitted)\b/i,
  /\bwho\s+(can|may|is\s+allowed\s+to)\b/i,
  /\b(my|your)\s+role\b/i,
  /\bwhat\s+role\b/i,
  /\b(admins?|managers?|users?)\s+(role|can|may)\b/i,
  /\bpermissions?\b/i,
  /\b(not\s+)?allowed\s+to\b/i,
  /\baccess\s+(to|the)\b/i,
  /\b(lab|users|audit|analytics)\s+page\b/i,
  /\bmanual(ly)?\s+clos(e|ing)\b/i,
  /\b(linked|link(ed)?\s+to)\s+(an?\s+|my\s+|the\s+)?accounts?\b/i,
  /\b(am\s+i|i'?m|i\s+am)\s+linked\b/i,
];

/** Whether a question asks what the person's role may see or do, or why not (#911). */
export function asksAboutAccess(question) {
  const q = String(question ?? "");
  return ACCESS_PATTERNS.some((re) => re.test(q));
}

const ROUTES = new Map([...STATE_CHANGING_ROUTES, ...READ_ROUTES].map((r) => [r.route, r]));

/** The roles that may do an action, from the matrix's row of its first route. */
export function actionRoles(action) {
  const row = ROUTES.get(action.routes[0]);
  return (row?.roles ?? []).filter((r) => PERSON_ROLES.includes(r));
}

/** "only admins", "managers and admins", "everyone signed in": who may, in words. */
function whoMay(roles) {
  if (PERSON_ROLES.every((r) => roles.includes(r))) return "everyone signed in";
  if (roles.includes("manager") && roles.includes("admin")) return "managers and admins";
  if (roles.includes("admin")) return "only admins";
  return "no role";
}

const article = (role) => (role === "admin" ? "an admin" : `a ${role}`);

/** For a manager: which accounts their actions on a bot apply to, in words (their own link ids only). */
function linkWords(links) {
  if (!links.length) return "You are linked to no account, so as a manager you act on every account of this monitor.";
  return `As a manager linked to accounts, you act only on the bots of the accounts you are linked to: ${links.join(", ")}. On a bot of any other account the monitor refuses it; an admin can link an account to you on the Users page.`;
}

/** One action's passage for this role: allowed (and where), or who may and who to ask. */
function actionPassage(action, role, links) {
  const roles = actionRoles(action);
  const who = whoMay(roles);
  let text;
  if (roles.includes(role)) {
    text = `${action.words}: ${who} can do it, so your role, ${role}, can.`;
    if (action.linked && role === "manager") text += ` ${linkWords(links)}`;
    if (action.linked && role === "admin") text += " Admins act on every account.";
  } else {
    text = `${action.words}: ${who} can do it; your role is ${role}, so you cannot. An admin can give you another role on the Users page.`;
  }
  return { id: `you:can/${action.key}`, source: "you:can", title: `what your role may do: ${action.key}`, text };
}

/** The you:role passage: the role, what it may and may not do, links, and who changes it. */
function rolePassage(role, links) {
  const may = ACCESS_ACTIONS.filter((a) => actionRoles(a).includes(role)).map((a) => a.short);
  const mayNot = ACCESS_ACTIONS.filter((a) => !actionRoles(a).includes(role)).map((a) => `${a.short} (${whoMay(actionRoles(a))})`);
  const parts = [
    `Your role is ${role}: you are signed in as ${article(role)}.`,
    `With it you may: ${may.join("; ")}.`,
    mayNot.length ? `You may not: ${mayNot.join("; ")}.` : "There is nothing in the monitor your role may not do.",
  ];
  if (role === "manager") parts.push(linkWords(links));
  parts.push(
    role === "admin"
      ? "Admins change roles and account links on the Users page."
      : "Only an admin can give you another role or other account links, on the Users page; the assistant cannot.",
  );
  parts.push("The monitor's server checks your role on every action; the assistant itself never places or changes anything.");
  return { id: "you:role", source: "you:role", title: "your role and what it may do", text: parts.join(" ") };
}

/**
 * The access passages for the signed-in person (#911): "you:role" (their
 * role, what it may and may not do, who changes it) and one
 * "you:can/<action>" per ACCESS_ACTIONS entry (allowed, or which roles may
 * and that an admin can change the role). A manager's passages name the
 * account ids they are linked to — their own links only, given by the caller
 * — and say that their actions on a bot apply only to those accounts.
 * role: the session's role; links: that manager's account ids. [] for any
 * other role (no person, a test run).
 */
export function accessPassages({ role, links = [] } = {}) {
  if (!PERSON_ROLES.includes(role)) return [];
  const own = role === "manager" ? [...new Set((links ?? []).filter((l) => typeof l === "string" && l))].sort() : [];
  return [rolePassage(role, own), ...ACCESS_ACTIONS.map((a) => actionPassage(a, role, own))];
}
