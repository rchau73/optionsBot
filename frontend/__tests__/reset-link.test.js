/**
 * @jest-environment node
 */
// #307 (+#214): an admin issues a single-use, 30-minute reset link; the person
// sets a new password with it at /reset, without a session.
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { NextRequest } from "next/server";
import { POST as createUser } from "@/app/api/admin/users/route";
import { POST as issueLink } from "@/app/api/admin/users/[id]/reset-link/route";
import { POST as resetPassword } from "@/app/api/admin/users/[id]/password/route";
import { POST as resetByLink } from "@/app/api/auth/reset/route";
import { POST as refresh } from "@/app/api/auth/refresh/route";
import { getAuth, resetAuth } from "@/lib/auth/server";
import { RESET_LINK_INVALID, RESET_LINK_TTL_MS } from "@/lib/auth/service";
import { REFRESH_COOKIE } from "@/lib/auth/tokens";
import { proxy } from "@/proxy";
import { signedIn, useTestAuth } from "@/test/auth";

jest.setTimeout(60000);

const ORIGIN = "http://localhost:3000";
const ANA_PASSWORD = "ana's first password";
const NEW_PASSWORD = "ana's chosen password";

function req(p, { method = "POST", body = {}, cookie = "", headers = {} } = {}) {
  return new Request(`${ORIGIN}${p}`, {
    method,
    headers: { origin: ORIGIN, host: "localhost:3000", "content-type": "application/json", cookie, ...headers },
    body: method === "GET" ? undefined : JSON.stringify(body),
  });
}
const ctx = (id) => ({ params: Promise.resolve({ id: String(id) }) });
const tokenOf = (url) => new URL(url).searchParams.get("token");

let logs;
beforeEach(() => {
  useTestAuth();
  logs = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
});
afterEach(() => jest.restoreAllMocks());

/** Admin "root" signed in, user "ana" created; audit entries collected. */
async function setup() {
  const root = await signedIn("root", "admin");
  const audit = [];
  root.auth.auditSink = (e) => audit.push(e);
  const res = await createUser(req("/api/admin/users", { cookie: root.cookie, body: { username: "ana", password: ANA_PASSWORD, role: "user" } }));
  expect(res.status).toBe(201);
  const ana = root.auth.store.userByName("ana");
  return { ...root, audit, ana };
}

async function link(cookie, id) {
  const res = await issueLink(req(`/api/admin/users/${id}/reset-link`, { cookie }), ctx(id));
  return { res, body: await res.json() };
}

const sendLink =(token, next = NEW_PASSWORD, headers = {}) => resetByLink(req("/api/auth/reset", { body: { token, next }, headers }));

describe("Scenario: reset with a link", () => {
  test("root issues a link for ana; within 30 min she sets a 12+ password, signs in with it, her sessions end, the audit names both", async () => {
    const { cookie, auth, audit, ana } = await setup();
    const before = await auth.login({ username: "ana", password: ANA_PASSWORD });
    expect(before.ok).toBe(true);

    const { res, body } = await link(cookie, ana.id);
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(body.url).toMatch(/^http:\/\/localhost:3000\/reset\?token=[A-Za-z0-9_-]{43}$/);
    const expires = Date.parse(body.expires_at);
    expect(expires - Date.now()).toBeGreaterThan(RESET_LINK_TTL_MS - 60000);
    expect(expires - Date.now()).toBeLessThanOrEqual(RESET_LINK_TTL_MS);

    const done = await sendLink(tokenOf(body.url));
    expect(done.status).toBe(200);
    expect(await done.json()).toEqual({});

    expect(auth.store.sessionCount(ana.id)).toBe(0);
    expect((await refresh(req("/api/auth/refresh", { cookie: `${REFRESH_COOKIE}=${before.refresh}` }))).status).toBe(401);
    expect((await auth.login({ username: "ana", password: ANA_PASSWORD })).ok).toBe(false);
    expect((await auth.login({ username: "ana", password: NEW_PASSWORD })).ok).toBe(true);

    expect(audit.find((e) => e.action === "reset_link_issued")).toMatchObject({ username: "root", role: "admin", target: "ana", expires_at: body.expires_at });
    expect(audit.find((e) => e.action === "password_reset_by_link")).toMatchObject({ username: "ana", role: "user", by: "root" });
  });
});

describe("Scenario: one use only", () => {
  test("a second use of the same link answers the generic error and the password stays the first reset's", async () => {
    const { cookie, auth, ana } = await setup();
    const token = tokenOf((await link(cookie, ana.id)).body.url);
    expect((await sendLink(token)).status).toBe(200);
    const hash = auth.store.passwordHash(ana.id);

    const again = await sendLink(token, "a different new password");
    expect(again.status).toBe(400);
    expect((await again.json()).error).toBe(RESET_LINK_INVALID);
    expect(auth.store.passwordHash(ana.id)).toBe(hash);
  });

  test("two requests racing with one link: exactly one wins", async () => {
    const { cookie, ana } = await setup();
    const token = tokenOf((await link(cookie, ana.id)).body.url);
    const statuses = (await Promise.all([sendLink(token, "first racing password"), sendLink(token, "second racing password")])).map((r) => r.status);
    expect(statuses.sort()).toEqual([200, 400]);
  });
});

describe("Scenario: single use and expiry", () => {
  test("a link issued 31 minutes ago answers the generic error and the password is unchanged", async () => {
    const { cookie, auth, ana } = await setup();
    const start = Date.now();
    auth.now = () => start;
    const token = tokenOf((await link(cookie, ana.id)).body.url);
    const hash = auth.store.passwordHash(ana.id);

    auth.now = () => start + 31 * 60 * 1000;
    const res = await sendLink(token);
    expect(res.status).toBe(400);
    expect((await res.json()).error).toBe(RESET_LINK_INVALID);
    expect(auth.store.passwordHash(ana.id)).toBe(hash);
  });

  test("a link still works at 29 minutes", async () => {
    const { cookie, auth, ana } = await setup();
    const start = Date.now();
    auth.now = () => start;
    const token = tokenOf((await link(cookie, ana.id)).body.url);
    auth.now = () => start + 29 * 60 * 1000;
    expect((await sendLink(token)).status).toBe(200);
  });

  test("a link replaced by a newer one answers the generic error; the newer one works", async () => {
    const { cookie, auth, ana } = await setup();
    const old = tokenOf((await link(cookie, ana.id)).body.url);
    const fresh = tokenOf((await link(cookie, ana.id)).body.url);
    expect(auth.store.resetTokens(ana.id)).toHaveLength(1);
    const hash = auth.store.passwordHash(ana.id);

    const res = await sendLink(old);
    expect(res.status).toBe(400);
    expect((await res.json()).error).toBe(RESET_LINK_INVALID);
    expect(auth.store.passwordHash(ana.id)).toBe(hash);
    expect((await sendLink(fresh)).status).toBe(200);
  });

  test("unknown, malformed and missing tokens get the same 400; a short password is refused with the rule and keeps the link usable", async () => {
    const { cookie, auth, ana } = await setup();
    for (const token of [crypto.randomBytes(32).toString("base64url"), "short", "", null, 42]) {
      const res = await sendLink(token);
      expect(res.status).toBe(400);
      expect((await res.json()).error).toBe(RESET_LINK_INVALID);
    }
    const token = tokenOf((await link(cookie, ana.id)).body.url);
    const short = await sendLink(token, "too short");
    expect(short.status).toBe(400);
    expect((await short.json()).error).toMatch(/at least 12 characters/);
    expect((await sendLink(token)).status).toBe(200);
    expect((await auth.login({ username: "ana", password: NEW_PASSWORD })).ok).toBe(true);
  });

  test("a deleted user's link no longer works", async () => {
    const { cookie, user, auth, ana } = await setup();
    const token = tokenOf((await link(cookie, ana.id)).body.url);
    auth.deleteUser(user, ana.id);
    expect(auth.store.resetTokens(ana.id)).toEqual([]);
    expect((await sendLink(token)).status).toBe(400);
  });
});

describe("Scenario: only admins issue links", () => {
  test("a manager or a user gets 403 and no token is stored", async () => {
    const { ana } = await setup();
    for (const role of ["manager", "user"]) {
      const { cookie, auth } = await signedIn(`p-${role}`, role);
      const { res } = await link(cookie, ana.id);
      expect(res.status).toBe(403);
      expect(auth.store.resetTokens(ana.id)).toEqual([]);
    }
  });

  test("no session → 401; cross-origin → 403; an unknown id → 404; your own account → 409", async () => {
    const { cookie, user, auth, ana } = await setup();
    expect((await issueLink(req(`/api/admin/users/${ana.id}/reset-link`), ctx(ana.id))).status).toBe(401);
    const cross = await issueLink(req(`/api/admin/users/${ana.id}/reset-link`, { cookie, headers: { origin: "http://evil.example" } }), ctx(ana.id));
    expect(cross.status).toBe(403);
    expect((await link(cookie, 999)).res.status).toBe(404);
    const own = await link(cookie, user.id);
    expect(own.res.status).toBe(409);
    expect(auth.store.resetTokens(user.id)).toEqual([]);
    expect(auth.store.resetTokens(ana.id)).toEqual([]);
  });

  test("the admin's direct password reset stays available", async () => {
    const { cookie, auth, ana } = await setup();
    const res = await resetPassword(req(`/api/admin/users/${ana.id}/password`, { cookie, body: { password: "set by the admin ok" } }), ctx(ana.id));
    expect(res.status).toBe(200);
    expect((await auth.login({ username: "ana", password: "set by the admin ok" })).ok).toBe(true);
  });
});

describe("the reset route is public, rate-limited and same-origin", () => {
  test("proxy.js lets /reset and POST /api/auth/reset through without a session, still guards the rest", async () => {
    const at = (p, method = "GET") => new NextRequest(`${ORIGIN}${p}`, { method });
    expect((await proxy(at("/reset?token=abc"))).headers.get("x-middleware-next")).toBe("1");
    expect((await proxy(at("/api/auth/reset", "POST"))).headers.get("x-middleware-next")).toBe("1");
    expect((await proxy(at("/api/admin/audit"))).status).toBe(401);
    expect((await proxy(at("/admin/users"))).status).toBe(307);
  });

  test("the per-IP sign-in limit applies: past 20 attempts a minute, 429 with Retry-After", async () => {
    await getAuth();
    const ip = { "x-forwarded-for": "203.0.113.9" };
    for (let i = 0; i < 20; i++) expect((await sendLink("x", "a password long enough", ip)).status).toBe(400);
    const limited = await sendLink("x", "a password long enough", ip);
    expect(limited.status).toBe(429);
    expect(Number(limited.headers.get("retry-after"))).toBeGreaterThan(0);
  });

  test("cross-origin or non-JSON requests are refused", async () => {
    await getAuth();
    expect((await sendLink("x", NEW_PASSWORD, { origin: "http://evil.example" })).status).toBe(403);
    expect((await sendLink("x", NEW_PASSWORD, { "content-type": "text/plain" })).status).toBe(415);
  });
});

describe("Scenario: no secret at rest", () => {
  let dir;
  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "reset-link-"));
    process.env.AUTH_DB_PATH = path.join(dir, "auth.db");
    resetAuth();
  });
  afterEach(() => {
    delete process.env.AUTH_DB_PATH;
    fs.rmSync(dir, { recursive: true, force: true });
  });

  test("auth.db holds only the token's SHA-256; audit.jsonl and the logs never hold the token or the url", async () => {
    const { cookie, auth, ana } = await signedIn("root", "admin").then(async (root) => {
      await createUser(req("/api/admin/users", { cookie: root.cookie, body: { username: "ana", password: ANA_PASSWORD, role: "user" } }));
      return { ...root, ana: root.auth.store.userByName("ana") };
    });
    const { body } = await link(cookie, ana.id);
    const token = tokenOf(body.url);
    const sha = crypto.createHash("sha256").update(token).digest("hex");

    const db = fs.readFileSync(path.join(dir, "auth.db"));
    expect(db.includes(token)).toBe(false);
    expect(db.includes(sha)).toBe(true);
    expect(auth.store.resetTokens(ana.id).map((t) => t.token_hash)).toEqual([sha]);

    expect((await sendLink(token)).status).toBe(200);
    const audit = fs.readFileSync(path.join(dir, "audit.jsonl"), "utf8");
    expect(audit).toContain('"action":"reset_link_issued"');
    expect(audit).toContain('"action":"password_reset_by_link"');
    const text = audit + logs.join("\n");
    for (const secret of [token, body.url, sha, NEW_PASSWORD]) expect(text).not.toContain(secret);
    expect(fs.readFileSync(path.join(dir, "auth.db")).includes(token)).toBe(false);
  });
});
