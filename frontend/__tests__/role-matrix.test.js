/**
 * @jest-environment node
 */
// #490: the role matrix. Every state-changing route of the monitor is called,
// through proxy.js then its handler, as each role, with no session, with the
// read-only MONITOR_API_TOKEN and with an expired session. A caller the table
// (lib/auth/routeRoles.js) does not allow gets 401 or 403 and changes nothing;
// an allowed one passes, and where the table names an audit action, leaves that
// line with the person's username. A route file exporting POST/PUT/PATCH/DELETE
// that the table does not list fails the test by name.
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { NextRequest } from "next/server";
import { proxy } from "@/proxy";
import { getAuth, resetAuth } from "@/lib/auth/server";
import { hashPassword } from "@/lib/auth/password";
import { hashToken } from "@/lib/auth/service";
import { ACCESS_COOKIE, authKey, REFRESH_COOKIE, signAccess } from "@/lib/auth/tokens";
import { OPTIONAL_KINDS } from "@/lib/notifications";
import { CALLERS, callerAllowed, ROLE_GUARDED_READS, routeKey, STATE_CHANGING_ROUTES, STATE_METHODS, unlistedRoutes } from "@/lib/auth/routeRoles";
import { TEST_SECRET, useTestAuth } from "@/test/auth";

jest.setTimeout(120000);

const ORIGIN = "http://localhost:3100";
const API_DIR = path.join(__dirname, "..", "app", "api");
const BOT_ROUTES_DIR = path.join(__dirname, "..", "..", "internal", "api");
// Test values, not secrets (CI runs gitleaks): low entropy on purpose.
const PASSWORD = "role matrix password";
const NEW_PASSWORD = "role matrix new password";
const SERVICE_TOKEN = "r".repeat(40);
const SALT = "s".repeat(32);
const SESSION = "0123456789abcdef0123456789abcdef";

/** Every route.js under app/api, relative paths. */
function routeFiles(dir = API_DIR) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) return routeFiles(p);
    return e.name === "route.js" ? [p] : [];
  });
}

/** "METHOD /api/…" → the handler, for every state-changing method a route file exports. */
function stateHandlers() {
  const out = new Map();
  for (const file of routeFiles()) {
    const mod = require(file);
    for (const method of STATE_METHODS) {
      if (typeof mod[method] === "function") out.set(routeKey(path.relative(path.join(API_DIR, "..", ".."), file), method), mod[method]);
    }
  }
  return out;
}

let dir, auditFile, auth, passwordHash, seq;

const auditLines = () =>
  fs.existsSync(auditFile) ? fs.readFileSync(auditFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : [];

/** A new person with a known password and a live session: { user, cookie, refreshCookie }. */
async function person(role) {
  seq += 1;
  const user = auth.store.createUser({ username: `${role}-${seq}`, passwordHash, role });
  const refresh = crypto.randomBytes(32).toString("base64url");
  const sid = hashToken(refresh);
  auth.store.addSession(sid, user.id, Date.now() + 3600 * 1000);
  const access = await signAccess(user, authKey(TEST_SECRET), Date.now(), sid);
  return { user, sid, cookie: `${ACCESS_COOKIE}=${access}`, refreshCookie: `${REFRESH_COOKIE}=${refresh}` };
}

/**
 * The request headers and the acting person for a caller: a role signs in
 * fresh; an expired session is a manager whose access token is two hours old.
 */
async function caller(name) {
  if (["admin", "manager", "user"].includes(name)) {
    const p = await person(name);
    return { person: p, headers: { cookie: p.cookie } };
  }
  if (name === "expired_session") {
    const p = await person("manager");
    const stale = await signAccess(p.user, authKey(TEST_SECRET), Date.now() - 2 * 3600 * 1000, p.sid);
    return { person: null, headers: { cookie: `${ACCESS_COOKIE}=${stale}` } };
  }
  if (name === "service_token") return { person: null, headers: { authorization: `Bearer ${SERVICE_TOKEN}` } };
  return { person: null, headers: {} };
}

/**
 * What each route needs to succeed: (who, auth) → { path, body, params?,
 * cookie? (added to the caller's), actor (the username the audit line names) }.
 * `who` is the caller's person, or null without a session.
 */
const FIXTURES = {
  "DELETE /api/admin/users/[id]": async ({ who }) => target(who),
  "DELETE /api/auth/account": async ({ who }) => {
    // Never the last admin: another one exists, so an admin caller may leave too.
    if (who?.user.role === "admin") await person("admin");
    return { path: "/api/auth/account", body: {}, actor: who?.user.username };
  },
  "PATCH /api/admin/users/[id]": async ({ who }) => ({ ...(await target(who)), body: { role: "manager" } }),
  "POST /api/admin/users": async ({ who }) => ({
    path: "/api/admin/users",
    body: { username: `created-${seq + 1}`, password: NEW_PASSWORD, role: "user" },
    actor: who?.user.username,
  }),
  "POST /api/admin/users/[id]/password": async ({ who }) => ({ ...(await target(who, "/password")), body: { password: NEW_PASSWORD } }),
  "POST /api/admin/users/[id]/reset-link": async ({ who }) => target(who, "/reset-link"),
  "POST /api/admin/users/[id]/signout": async ({ who }) => target(who, "/signout"),
  "POST /api/assistant": async () => ({ path: "/api/assistant", body: { question: "What is a strangle?" } }),
  "POST /api/assistant/flag": async () => ({ path: "/api/assistant/flag", body: { id: "a-1" } }),
  "POST /api/assistant/requests": async () => ({ path: "/api/assistant/requests", body: { answer_id: "a-1", kind: "bug", fields: {} } }),
  "POST /api/auth/login": async () => {
    const p = await person("user");
    return { path: "/api/auth/login", body: { username: p.user.username, password: PASSWORD }, actor: p.user.username };
  },
  "POST /api/auth/logout": async ({ who }) => ({ path: "/api/auth/logout", body: {}, actor: who?.user.username }),
  "POST /api/auth/password": async ({ who }) => ({ path: "/api/auth/password", body: { current: PASSWORD, next: NEW_PASSWORD }, actor: who?.user.username }),
  "POST /api/auth/refresh": async () => {
    const p = await person("user");
    return { path: "/api/auth/refresh", body: {}, cookie: p.refreshCookie };
  },
  "POST /api/auth/reset": async () => {
    const admin = await person("admin");
    const p = await person("user");
    const link = auth.issueResetLink(admin.user, p.user.id);
    return { path: "/api/auth/reset", body: { token: link.token, next: NEW_PASSWORD }, actor: p.user.username };
  },
  "POST /api/bots/[bot]/close": async ({ who }) => ({
    path: "/api/bots/btc/close",
    params: { bot: "btc" },
    body: { position_ids: ["p1"] },
    actor: who?.user.username,
  }),
  "POST /api/bots/[bot]/rebalance": async ({ who }) => ({
    path: "/api/bots/btc/rebalance",
    params: { bot: "btc" },
    body: { dry_run: true },
    actor: who?.user.username,
  }),
  "POST /api/consent": async ({ who }) => ({ path: "/api/consent", body: { consent: true }, actor: who?.user.username }),
  "POST /api/events": async ({ who }) => {
    if (who) auth.store.setAnalyticsConsent(who.user.id, true);
    return { path: "/api/events", body: { event: "panel_expanded", session_id: SESSION, props: { panel: "held" } } };
  },
  "POST /api/notifications": async () => ({ path: "/api/notifications", body: { action: "setting", kind: OPTIONAL_KINDS[0], enabled: false } }),
  "POST /api/survey": async ({ who }) => {
    if (who) auth.store.setAnalyticsConsent(who.user.id, true);
    return { path: "/api/survey", body: { survey_id: "nps", session_id: SESSION, codes: { score: "9" } }, actor: who?.user.username };
  },
  "POST /api/survey/answers": async ({ who }) => {
    if (who) {
      // Due: consent yes, an account 20 days old, 5 sign-ins, never shown.
      auth.store.setAnalyticsConsent(who.user.id, true);
      auth.store.db.run("UPDATE users SET created_at = ?, sign_in_count = 5, answers_last_shown_at = NULL WHERE id = ?", [Math.floor(Date.now() / 1000) - 20 * 86400, who.user.id]);
    }
    return { path: "/api/survey/answers", body: { action: "shown" }, actor: who?.user.username };
  },
  "POST /api/survey/nps": async ({ who }) => ({ path: "/api/survey/nps", body: { action: "opt_out" }, actor: who?.user.username }),
};

/** A user an admin route acts on; `suffix` follows /api/admin/users/<id>. */
async function target(who, suffix = "") {
  const p = await person("user");
  return { path: `/api/admin/users/${p.user.id}${suffix}`, params: { id: String(p.user.id) }, body: {}, actor: who?.user.username };
}

/** Sends one request the way Next does: proxy.js first, then the route's handler if the proxy lets it through. */
async function send(handler, method, { path: p, body, params, cookie }, headers) {
  seq += 1;
  const cookies = [headers.cookie, cookie].filter(Boolean).join("; ");
  const request = new NextRequest(`${ORIGIN}${p}`, {
    method,
    headers: {
      origin: ORIGIN,
      host: "localhost:3100",
      "content-type": "application/json",
      "x-forwarded-for": `10.0.${Math.floor(seq / 250)}.${seq % 250}`,
      ...(headers.authorization ? { authorization: headers.authorization } : {}),
      ...(cookies ? { cookie: cookies } : {}),
    },
    body: JSON.stringify(body),
  });
  const passed = await proxy(request);
  if (passed.headers.get("x-middleware-next") !== "1") return passed;
  return handler(request, { params: Promise.resolve(params ?? {}) });
}

const realFetch = global.fetch;
// The assistant stays off here (503 after the role check): no model is ever called.
const modelKey = process.env.ANTHROPIC_API_KEY;
beforeAll(async () => {
  passwordHash = await hashPassword(PASSWORD);
});
beforeEach(async () => {
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  useTestAuth();
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "role-matrix-"));
  process.env.AUTH_DB_PATH = path.join(dir, "auth.db");
  auditFile = path.join(dir, "audit.jsonl");
  process.env.MONITOR_API_TOKEN = SERVICE_TOKEN;
  process.env.ANALYTICS_SALT = SALT;
  process.env.BOT_APIS = "btc=http://bot-btc:8081";
  process.env.BOT_ADMIN_TOKEN = "admin token for tests";
  delete process.env.ANTHROPIC_API_KEY;
  resetAuth();
  auth = await getAuth();
  seq = 0;
  global.fetch = jest.fn(async () => Response.json({ results: [], dry_run: true, actions: [] }));
});
afterEach(() => {
  jest.restoreAllMocks();
  global.fetch = realFetch;
  resetAuth();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ["AUTH_DB_PATH", "MONITOR_API_TOKEN", "ANALYTICS_SALT", "BOT_APIS", "BOT_ADMIN_TOKEN"]) delete process.env[k];
  if (modelKey !== undefined) process.env.ANTHROPIC_API_KEY = modelKey;
});

describe("Scenario: a new route without a table entry", () => {
  test("every state-changing route file is in the table, and the table lists no route that is gone", () => {
    const found = [...stateHandlers().keys()];
    expect(unlistedRoutes(found)).toEqual([]);
    expect(STATE_CHANGING_ROUTES.map((r) => r.route).filter((r) => !found.includes(r))).toEqual([]);
  });

  test("an unlisted route is named", () => {
    const found = ["POST /api/consent", "POST /api/bots/[bot]/pause"];
    expect(unlistedRoutes(found)).toEqual(["POST /api/bots/[bot]/pause"]);
    expect(routeKey("frontend/app/api/bots/[bot]/pause/route.js", "POST")).toBe("POST /api/bots/[bot]/pause");
  });

  test("every state-changing route of the bot's API is forwarded by a listed monitor route", () => {
    const source = fs.readdirSync(BOT_ROUTES_DIR).filter((f) => f.endsWith(".go")).map((f) => fs.readFileSync(path.join(BOT_ROUTES_DIR, f), "utf8")).join("\n");
    const botRoutes = [...source.matchAll(/HandleFunc\("((?:POST|PUT|PATCH|DELETE) [^"]+)"/g)].map((m) => m[1]).sort();
    expect(botRoutes.length).toBeGreaterThan(0);
    const forwarded = STATE_CHANGING_ROUTES.filter((r) => r.bot).map((r) => r.bot).sort();
    expect(forwarded).toEqual(botRoutes);
  });

  test("the table is in route order and every entry is complete", () => {
    const routes = STATE_CHANGING_ROUTES.map((r) => r.route);
    expect(routes).toEqual([...routes].sort());
    for (const r of STATE_CHANGING_ROUTES) {
      expect(r.roles.length).toBeGreaterThan(0);
      if (r.audit === null) expect(r.why).toEqual(expect.any(String));
      expect(FIXTURES[r.route]).toEqual(expect.any(Function));
    }
  });
});

describe("Scenario: a role that is not allowed, the read-only token, an allowed action", () => {
  test.each(STATE_CHANGING_ROUTES.map((r) => [r.route, r]))("%s", async (_, entry) => {
    const handler = stateHandlers().get(entry.route);
    for (const name of CALLERS) {
      const { person: who, headers } = await caller(name);
      const fixture = await FIXTURES[entry.route]({ who });
      const before = auditLines().length;
      global.fetch.mockClear();
      const res = await send(handler, entry.route.split(" ")[0], fixture, headers);
      const where = `${entry.route} as ${name}: ${res.status}`;

      if (!callerAllowed(entry, name)) {
        // A signed-in person of the wrong role hears 403; no session, the token or an expired one 401.
        expect([where, res.status]).toEqual([where, who ? 403 : 401]);
        expect([where, auditLines().slice(before)]).toEqual([where, []]);
        expect(global.fetch).not.toHaveBeenCalled();
        continue;
      }
      expect([where, [401, 403].includes(res.status)]).toEqual([where, false]);
      if (entry.audit) {
        expect([where, res.status < 300]).toEqual([where, true]);
        const line = auditLines().slice(before).find((l) => l.action === entry.audit);
        expect([where, line]).toEqual([where, expect.objectContaining({ action: entry.audit, username: fixture.actor, time: expect.any(String) })]);
      }
    }
  });
});

describe("Scenario: an allowed action", () => {
  test("a manager's manual close leaves an audit line with their username, the action and the time", async () => {
    const { person: who, headers } = await caller("manager");
    const handler = stateHandlers().get("POST /api/bots/[bot]/close");
    const res = await send(handler, "POST", await FIXTURES["POST /api/bots/[bot]/close"]({ who }), headers);
    expect(res.status).toBe(200);
    expect(auditLines().at(-1)).toEqual(
      expect.objectContaining({ action: "manual_close", username: who.user.username, role: "manager", bot: "btc", position_ids: ["p1"], time: expect.any(String) }),
    );
    expect(global.fetch).toHaveBeenCalledTimes(1);
  });

  test("a user's manual close is refused with 403 and the bot hears nothing", async () => {
    const { headers } = await caller("user");
    const handler = stateHandlers().get("POST /api/bots/[bot]/close");
    const res = await send(handler, "POST", await FIXTURES["POST /api/bots/[bot]/close"]({ who: null }), headers);
    expect(res.status).toBe(403);
    expect(global.fetch).not.toHaveBeenCalled();
    expect(auditLines().filter((l) => l.action === "manual_close")).toEqual([]);
  });
});

/** The module of a route ("GET /api/lab/runs/[id]" → app/api/lab/runs/[id]/route.js). */
function routeModule(route) {
  return require(path.join(API_DIR, route.split(" ")[1].replace(/^\/api\//, ""), "route.js"));
}

/** Sends a GET the way Next does: proxy.js first, then the handler. */
async function sendGet(handler, p, params, headers) {
  const request = new NextRequest(`${ORIGIN}${p}`, {
    headers: {
      host: "localhost:3100",
      ...(headers.authorization ? { authorization: headers.authorization } : {}),
      ...(headers.cookie ? { cookie: headers.cookie } : {}),
    },
  });
  const passed = await proxy(request);
  if (passed.headers.get("x-middleware-next") !== "1") return passed;
  return handler(request, { params: Promise.resolve(params) });
}

const RUN_ID = "20261012T093000Z";
const READ_FIXTURES = {
  "GET /api/lab/runs": { path: "/api/lab/runs", params: {} },
  "GET /api/lab/runs/[id]": { path: `/api/lab/runs/${RUN_ID}`, params: { id: RUN_ID } },
};

describe("Scenario: a role-guarded read (#539)", () => {
  beforeEach(() => {
    process.env.COMPARE_DIR = path.join(dir, "compare");
    fs.mkdirSync(path.join(process.env.COMPARE_DIR, RUN_ID), { recursive: true });
    fs.writeFileSync(path.join(process.env.COMPARE_DIR, RUN_ID, "report.json"), JSON.stringify({ underlying: "BTC", diffs: [] }));
  });
  afterEach(() => delete process.env.COMPARE_DIR);

  test("every entry has a fixture and its route file exports GET and no state-changing method", () => {
    for (const r of ROLE_GUARDED_READS) {
      expect(READ_FIXTURES[r.route]).toBeDefined();
      const mod = routeModule(r.route);
      expect([r.route, typeof mod.GET]).toEqual([r.route, "function"]);
      expect([r.route, STATE_METHODS.filter((m) => mod[m])]).toEqual([r.route, []]);
    }
  });

  test.each(ROLE_GUARDED_READS.map((r) => [r.route, r]))("%s as every caller", async (_, entry) => {
    const { GET } = routeModule(entry.route);
    const { path: p, params } = READ_FIXTURES[entry.route];
    for (const name of CALLERS) {
      const { person: who, headers } = await caller(name);
      const res = await sendGet(GET, p, params, headers);
      const where = `${entry.route} as ${name}`;
      if (callerAllowed(entry, name)) expect([where, res.status]).toEqual([where, 200]);
      else expect([where, res.status]).toEqual([where, who ? 403 : 401]);
    }
  });
});
