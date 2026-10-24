/**
 * @jest-environment node
 */
import { NextRequest } from "next/server";
import { POST as login } from "@/app/api/auth/login/route";
import { POST as refresh } from "@/app/api/auth/refresh/route";
import { POST as changePassword } from "@/app/api/auth/password/route";
import { proxy } from "@/proxy";
import { getAuth } from "@/lib/auth/server";
import { newPasswordProblem } from "@/lib/auth/service";
import { hashPassword, MIN_PASSWORD_LENGTH } from "@/lib/auth/password";
import { MIN_LENGTH } from "@/components/ChangePassword";
import { useTestAuth } from "@/test/auth";

jest.setTimeout(60000);

const ORIGIN = "http://localhost:3100";
const OLD = "ana's first password";
const NEW = "ana's brand new password";

function req(path, { body, cookie = "", headers = {} } = {}) {
  return new Request(`${ORIGIN}${path}`, {
    method: "POST",
    headers: { origin: ORIGIN, host: "localhost:3100", "content-type": "application/json", cookie, ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

/** name → value of each Set-Cookie. */
function cookies(res) {
  return Object.fromEntries(res.headers.getSetCookie().map((c) => c.split(";")[0].split("=")));
}

/** Signs "ana" in through the real route → { access, refresh, cookie } of that browser. */
async function browser(password = OLD) {
  const res = await login(req("/api/auth/login", { body: { username: "ana", password } }));
  if (res.status !== 200) return { status: res.status };
  const c = cookies(res);
  return { status: 200, access: c.access_token, refresh: c.refresh_token, cookie: `access_token=${c.access_token}; refresh_token=${c.refresh_token}` };
}

const change = (b, body, headers) => changePassword(req("/api/auth/password", { body, cookie: b?.cookie ?? "", headers }));
const storedHash = async () => {
  const auth = await getAuth();
  return auth.store.passwordHash(auth.store.userByName("ana").id);
};

let logs;
beforeEach(async () => {
  useTestAuth();
  logs = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  const auth = await getAuth();
  auth.store.createUser({ username: "ana", passwordHash: await hashPassword(OLD), role: "user" });
});
afterEach(() => {
  jest.restoreAllMocks();
  delete process.env.MONITOR_API_TOKEN;
});

describe("Scenario: change own password", () => {
  test("browser A stays signed in, browser B is signed out, only the new password works", async () => {
    const a = await browser();
    const b = await browser();

    const res = await change(a, { current: OLD, next: NEW });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ user: { username: "ana", role: "user" } });
    const fresh = cookies(res);
    expect(fresh.access_token).toBeTruthy();
    expect(fresh.refresh_token).toBeTruthy();

    // Browser A: its new session is live (refresh and state-changing routes).
    const auth = await getAuth();
    expect((await refresh(req("/api/auth/refresh", { cookie: `refresh_token=${fresh.refresh_token}` }))).status).toBe(200);
    expect((await auth.actingUser(fresh.access_token, "user")).ok).toBe(true);
    // Its old session ended with every other one.
    expect((await refresh(req("/api/auth/refresh", { cookie: `refresh_token=${a.refresh}` }))).status).toBe(401);

    // Browser B: the next refresh fails (→ /login) and its token no longer acts.
    expect((await refresh(req("/api/auth/refresh", { cookie: `refresh_token=${b.refresh}` }))).status).toBe(401);
    expect((await auth.actingUser(b.access, "user")).status).toBe(401);
    expect(auth.store.sessionCount(auth.store.userByName("ana").id)).toBe(1);

    expect((await browser(NEW)).status).toBe(200);
    expect((await browser(OLD)).status).toBe(401);
  });

  test("one password_changed line and audit entry, never a password, hash or token", async () => {
    const a = await browser();
    const audit = [];
    (await getAuth()).auditSink = (e) => audit.push(e);
    const res = await change(a, { current: OLD, next: NEW }, { "x-forwarded-for": "10.0.0.7" });
    const fresh = cookies(res);
    const lines = logs.map((l) => JSON.parse(l)).filter((l) => l.event === "password_changed");
    expect(lines).toEqual([expect.objectContaining({ level: "INFO", username: "ana", ip: "10.0.0.7" })]);
    expect(audit).toEqual([expect.objectContaining({ action: "password_changed", username: "ana", role: "user", ip: "10.0.0.7" })]);
    const text = [logs.join("\n"), JSON.stringify(audit)].join("\n");
    for (const secret of [OLD, NEW, "$argon2id$", fresh.access_token, fresh.refresh_token, a.refresh]) expect(text).not.toContain(secret);
  });
});

describe("Scenario: wrong current password", () => {
  test("5 wrong answers are each 401 and lock ana exactly like 5 failed sign-ins", async () => {
    const a = await browser();
    const before = await storedHash();
    for (let i = 0; i < 5; i++) {
      const res = await change(a, { current: `wrong guess number ${i}`, next: NEW });
      expect(res.status).toBe(401);
      expect(await res.json()).toEqual({ error: "Current password is wrong" });
      // Not the monitor's "no session" 401: the browser must not refresh and retry.
      expect(res.headers.get("x-monitor-session")).toBeNull();
    }
    // Locked: the right password no longer signs in…
    const signIn = await login(req("/api/auth/login", { body: { username: "ana", password: OLD } }));
    expect(signIn.status).toBe(401);
    expect(await signIn.json()).toEqual({ error: "invalid username or password" });
    // …nor changes the password.
    const locked = await change(a, { current: OLD, next: NEW });
    expect(locked.status).toBe(429);
    expect(Number(locked.headers.get("retry-after"))).toBeGreaterThan(0);
    expect(await storedHash()).toBe(before);
  });

  test("failed sign-ins and wrong current passwords add up to one lock", async () => {
    const a = await browser();
    for (let i = 0; i < 3; i++) await login(req("/api/auth/login", { body: { username: "ana", password: "not the password" } }));
    expect((await change(a, { current: "still not it", next: NEW })).status).toBe(401);
    expect((await change(a, { current: "nor this one", next: NEW })).status).toBe(401);
    expect((await login(req("/api/auth/login", { body: { username: "ana", password: OLD } }))).status).toBe(401);
  });
});

describe("Scenario: weak new password", () => {
  test("11 characters or the current password → 400 with the reason, hash unchanged", async () => {
    const a = await browser();
    const before = await storedHash();
    const short = await change(a, { current: OLD, next: "x".repeat(11) });
    expect(short.status).toBe(400);
    expect((await short.json()).error).toBe("Too short: at least 12 characters");
    const same = await change(a, { current: OLD, next: OLD });
    expect(same.status).toBe(400);
    expect((await same.json()).error).toBe("The new password must differ from the current one");
    expect((await change(a, { current: OLD })).status).toBe(400);
    expect(await storedHash()).toBe(before);
    // Still signed in: a refused change ends no session.
    expect((await refresh(req("/api/auth/refresh", { cookie: `refresh_token=${a.refresh}` }))).status).toBe(200);
  });

  test("the rule is the sign-in rule, and the form shows the same length", () => {
    expect(MIN_LENGTH).toBe(MIN_PASSWORD_LENGTH);
    expect(newPasswordProblem(OLD, "y".repeat(MIN_PASSWORD_LENGTH))).toBeNull();
    expect(newPasswordProblem(OLD, "y".repeat(2000))).toMatch(/^Too long/);
    expect(newPasswordProblem(OLD, undefined)).toMatch(/^Too short/);
  });
});

describe("Scenario: no session", () => {
  test("no session, the service token or another origin: refused, nothing changes", async () => {
    const a = await browser();
    const before = await storedHash();
    const body = { current: OLD, next: NEW };

    const none = await change(null, body);
    expect(none.status).toBe(401);
    expect(none.headers.get("x-monitor-session")).toBe("required");

    const token = "s".repeat(40);
    process.env.MONITOR_API_TOKEN = token;
    const bearer = { authorization: `Bearer ${token}` };
    expect((await change(null, body, bearer)).status).toBe(401);
    const viaProxy = new NextRequest(`${ORIGIN}/api/auth/password`, { method: "POST", headers: bearer });
    expect((await proxy(viaProxy)).status).toBe(401);

    expect((await change(a, body, { origin: "http://evil.example" })).status).toBe(403);
    expect((await change(a, body, { "content-type": "text/plain" })).status).toBe(415);

    expect(await storedHash()).toBe(before);
    expect((await refresh(req("/api/auth/refresh", { cookie: `refresh_token=${a.refresh}` }))).status).toBe(200);
  });
});
