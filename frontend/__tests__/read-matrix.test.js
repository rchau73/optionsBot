/**
 * @jest-environment node
 */
// #782: the read matrix. Every GET route of the monitor is called, through
// proxy.js then its handler, as each role, with no session, with the read-only
// MONITOR_API_TOKEN and with an expired session. A caller the table
// (READ_ROUTES in lib/auth/routeRoles.js) allows gets 200; any other gets 401
// or 403 with nothing but the reason, and no audit line. A route file
// exporting GET that the table does not list fails the test by name.
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { NextRequest } from "next/server";
import { proxy } from "@/proxy";
import { getAuth, resetAuth } from "@/lib/auth/server";
import { hashPassword } from "@/lib/auth/password";
import { hashToken } from "@/lib/auth/service";
import { ACCESS_COOKIE, authKey, signAccess } from "@/lib/auth/tokens";
import { CALLERS, READ_ROUTES, readAllowed, routeKey, unlistedRoutes } from "@/lib/auth/routeRoles";
import { setAssistantModel } from "@/lib/assistant/server";
import { TEST_SECRET, useTestAuth } from "@/test/auth";

jest.setTimeout(120000);

const ORIGIN = "http://localhost:3100";
const API_DIR = path.join(__dirname, "..", "app", "api");
// Test values, not secrets (CI runs gitleaks): low entropy on purpose.
const PASSWORD = "read matrix password";
const SERVICE_TOKEN = "q".repeat(40);
const SALT = "t".repeat(32);
const RUN_ID = "20261012T093000Z";
// What the bots answer (fetch is mocked): a marker the refused answers must not carry.
const BOT_DATA = { results: [], positions: [{ id: "p1" }], mode: "off", secret_marker: "bot data" };

/** Every route.js under app/api, absolute paths. */
function routeFiles(dir = API_DIR) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) return routeFiles(p);
    return e.name === "route.js" ? [p] : [];
  });
}

/** "GET /api/…" → the handler, for every route file that exports GET. */
function readHandlers() {
  const out = new Map();
  for (const file of routeFiles()) {
    const mod = require(file);
    if (typeof mod.GET === "function") out.set(routeKey(path.relative(path.join(API_DIR, "..", ".."), file), "GET"), mod.GET);
  }
  return out;
}

/** The path and params each route needs to answer 200. */
const FIXTURES = {
  "GET /api/bots/[bot]/[...path]": { path: "/api/bots/btc/status", params: { bot: "btc", path: ["status"] } },
  "GET /api/bots/[bot]/hedge": { path: "/api/bots/btc/hedge", params: { bot: "btc" } },
  "GET /api/compare/[id]": { path: `/api/compare/${RUN_ID}`, params: { id: RUN_ID } },
  "GET /api/lab/runs/[id]": { path: `/api/lab/runs/${RUN_ID}`, params: { id: RUN_ID } },
  "GET /api/lab/runs/[id]/preview": { path: `/api/lab/runs/${RUN_ID}/preview`, params: { id: RUN_ID } },
};
const fixture = (route) => FIXTURES[route] ?? { path: route.split(" ")[1], params: {} };

let dir, auditFile, auth, passwordHash, seq;

const auditLines = () =>
  fs.existsSync(auditFile) ? fs.readFileSync(auditFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : [];

/** A new person with a live session: { user, sid, cookie }. */
async function person(role) {
  seq += 1;
  const user = auth.store.createUser({ username: `${role}-${seq}`, passwordHash, role });
  const sid = hashToken(crypto.randomBytes(32).toString("base64url"));
  auth.store.addSession(sid, user.id, Date.now() + 3600 * 1000);
  const access = await signAccess(user, authKey(TEST_SECRET), Date.now(), sid);
  return { user, sid, cookie: `${ACCESS_COOKIE}=${access}` };
}

/** The headers and person of a caller; an expired session is a manager whose access token is two hours old. */
async function caller(name) {
  if (["admin", "manager", "user"].includes(name)) {
    const p = await person(name);
    return { who: p, headers: { cookie: p.cookie } };
  }
  if (name === "expired_session") {
    const p = await person("manager");
    const stale = await signAccess(p.user, authKey(TEST_SECRET), Date.now() - 2 * 3600 * 1000, p.sid);
    return { who: null, headers: { cookie: `${ACCESS_COOKIE}=${stale}` } };
  }
  if (name === "service_token") return { who: null, headers: { authorization: `Bearer ${SERVICE_TOKEN}` } };
  return { who: null, headers: {} };
}

/** Sends a GET the way Next does: proxy.js first, then the handler if the proxy lets it through. */
async function sendGet(handler, { path: p, params }, headers) {
  seq += 1;
  const request = new NextRequest(`${ORIGIN}${p}`, {
    headers: {
      host: "localhost:3100",
      "x-forwarded-for": `10.1.${Math.floor(seq / 250)}.${seq % 250}`,
      ...(headers.authorization ? { authorization: headers.authorization } : {}),
      ...(headers.cookie ? { cookie: headers.cookie } : {}),
    },
  });
  const passed = await proxy(request);
  if (passed.headers.get("x-middleware-next") !== "1") return passed;
  return handler(request, { params: Promise.resolve(params) });
}

/** The answer's body as JSON, or its text when it is not JSON. */
async function bodyOf(res) {
  const text = await res.text();
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

const ENV_KEYS = ["AUTH_DB_PATH", "MONITOR_API_TOKEN", "ANALYTICS_SALT", "BOT_APIS", "COMPARE_DIR", "USAGE_REPORTS_DIR", "DORA_DIR", "ASSISTANT_REQUESTS_PATH", "ASSISTANT_LOG_PATH", "EVENTS_PATH"];
const realFetch = global.fetch;
const modelKey = process.env.ANTHROPIC_API_KEY;
beforeAll(async () => {
  passwordHash = await hashPassword(PASSWORD);
});
beforeEach(async () => {
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  useTestAuth();
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "read-matrix-"));
  process.env.AUTH_DB_PATH = path.join(dir, "auth.db");
  auditFile = path.join(dir, "audit.jsonl");
  process.env.MONITOR_API_TOKEN = SERVICE_TOKEN;
  process.env.ANALYTICS_SALT = SALT;
  process.env.BOT_APIS = "btc=http://bot-btc:8081";
  // Every file a read touches sits in the test's folder, never the clone's data/.
  process.env.COMPARE_DIR = path.join(dir, "compare");
  process.env.USAGE_REPORTS_DIR = path.join(dir, "usage");
  process.env.DORA_DIR = path.join(dir, "dora");
  process.env.ASSISTANT_REQUESTS_PATH = path.join(dir, "requests.jsonl");
  process.env.ASSISTANT_LOG_PATH = path.join(dir, "assistant.jsonl");
  process.env.EVENTS_PATH = path.join(dir, "events.jsonl");
  fs.mkdirSync(path.join(process.env.COMPARE_DIR, RUN_ID), { recursive: true });
  fs.writeFileSync(path.join(process.env.COMPARE_DIR, RUN_ID, "report.json"), JSON.stringify({ underlying: "BTC", diffs: [{ key: "backtest.slippage_pct", base: "0.001", proposed: "0.002" }] }));
  delete process.env.ANTHROPIC_API_KEY;
  // The assistant on, with a model that is never asked: a GET reads its state only.
  setAssistantModel(() => {
    throw new Error("the read matrix never asks the model");
  });
  resetAuth();
  auth = await getAuth();
  seq = 0;
  global.fetch = jest.fn(async () => Response.json(BOT_DATA));
});
afterEach(() => {
  jest.restoreAllMocks();
  global.fetch = realFetch;
  setAssistantModel(undefined);
  resetAuth();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ENV_KEYS) delete process.env[k];
  if (modelKey !== undefined) process.env.ANTHROPIC_API_KEY = modelKey;
});

describe("Scenario: a new GET route without a table entry", () => {
  test("every GET route file is in the table, and the table lists no route that is gone", () => {
    const found = [...readHandlers().keys()];
    expect(unlistedRoutes(found, READ_ROUTES)).toEqual([]);
    expect(READ_ROUTES.map((r) => r.route).filter((r) => !found.includes(r))).toEqual([]);
  });

  test("an unlisted GET route is named", () => {
    const found = ["GET /api/consent", "GET /api/admin/secrets"];
    expect(unlistedRoutes(found, READ_ROUTES)).toEqual(["GET /api/admin/secrets"]);
    expect(routeKey("frontend/app/api/admin/secrets/route.js", "GET")).toBe("GET /api/admin/secrets");
  });

  test("the table is in route order and every entry is complete", () => {
    const routes = READ_ROUTES.map((r) => r.route);
    expect(routes).toEqual([...routes].sort());
    for (const r of READ_ROUTES) {
      expect([r.route, r.roles.length > 0, typeof r.why]).toEqual([r.route, true, "string"]);
    }
  });

  test("only /api/bots routes list the service token, as proxy.js lets it through there only", () => {
    const withToken = READ_ROUTES.filter((r) => r.token).map((r) => r.route);
    expect(withToken).toEqual(["GET /api/bots", "GET /api/bots/[bot]/[...path]", "GET /api/bots/[bot]/hedge"]);
  });
});

describe("Scenario: each GET route as every caller", () => {
  test.each(READ_ROUTES.map((r) => [r.route, r]))("%s", async (_, entry) => {
    const handler = readHandlers().get(entry.route);
    for (const name of CALLERS) {
      const { who, headers } = await caller(name);
      const before = auditLines().length;
      global.fetch.mockClear();
      const res = await sendGet(handler, fixture(entry.route), headers);
      const where = `${entry.route} as ${name}`;
      const added = auditLines().slice(before);

      if (readAllowed(entry, name)) {
        expect([where, res.status]).toEqual([where, 200]);
        if (entry.audit) expect([where, added.map((l) => [l.action, l.username])]).toEqual([where, [[entry.audit, who.user.username]]]);
        continue;
      }
      // A signed-in person of the wrong role hears 403; no session, the token or an expired one 401.
      expect([where, res.status]).toEqual([where, who ? 403 : 401]);
      // Nothing but the reason: no data, no audit line, the bot not asked.
      const body = await bodyOf(res);
      expect([where, Object.keys(body)]).toEqual([where, ["error"]]);
      expect([where, typeof body.error]).toEqual([where, "string"]);
      expect([where, added]).toEqual([where, []]);
      expect([where, global.fetch.mock.calls.length]).toEqual([where, 0]);
    }
  });
});

describe("Scenario: an admin-only read", () => {
  const ADMIN_ONLY = ["GET /api/admin/analytics", "GET /api/admin/analytics/delivery", "GET /api/admin/audit", "GET /api/admin/audit/export", "GET /api/admin/audit/verify", "GET /api/admin/users"];

  test("audit, audit verify and export, analytics and users are admins only in the table", () => {
    for (const route of ADMIN_ONLY) {
      const entry = READ_ROUTES.find((r) => r.route === route);
      expect([route, entry?.roles, entry?.token]).toEqual([route, ["admin"], undefined]);
    }
  });

  test.each(ADMIN_ONLY)("%s: a manager or a user gets 403 and none of the data", async (route) => {
    const handler = readHandlers().get(route);
    // The admin's answer holds the data: the other users' names for the users list.
    const admin = await caller("admin");
    const other = await person("user");
    const allowed = await sendGet(handler, fixture(route), admin.headers);
    expect(allowed.status).toBe(200);
    if (route === "GET /api/admin/users") expect(JSON.stringify(await bodyOf(allowed))).toContain(other.user.username);
    for (const role of ["manager", "user"]) {
      const { headers } = await caller(role);
      const res = await sendGet(handler, fixture(route), headers);
      const text = await res.text();
      expect([route, role, res.status]).toEqual([route, role, 403]);
      expect([route, role, Object.keys(JSON.parse(text))]).toEqual([route, role, ["error"]]);
      expect(text).not.toContain(other.user.username);
    }
  });
});
