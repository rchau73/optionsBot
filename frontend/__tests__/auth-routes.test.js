/**
 * @jest-environment node
 */
import { NextRequest } from "next/server";
import { POST as login } from "@/app/api/auth/login/route";
import { POST as refresh } from "@/app/api/auth/refresh/route";
import { POST as logout } from "@/app/api/auth/logout/route";
import { proxy, serviceTokenOk } from "@/proxy";
import { cookieSecure } from "@/lib/auth/tokens";
import { sessionCookie, TEST_SECRET, useTestAuth } from "@/test/auth";

jest.setTimeout(30000);

const ORIGIN = "http://localhost:3100";
const PASSWORD = "admin password for tests";

function req(path, { method = "POST", body, cookie = "", headers = {} } = {}) {
  return new Request(`${ORIGIN}${path}`, {
    method,
    headers: { origin: ORIGIN, host: "localhost:3100", "content-type": "application/json", cookie, ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

/** name → value of each Set-Cookie, plus the raw strings. */
function cookies(res) {
  const raw = res.headers.getSetCookie();
  return { raw, values: Object.fromEntries(raw.map((c) => c.split(";")[0].split("="))) };
}

const page = (path, cookie = "", headers = {}) => new NextRequest(`${ORIGIN}${path}`, { headers: { cookie, ...headers } });

let logs;
beforeEach(() => {
  useTestAuth();
  logs = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
});
afterEach(() => {
  jest.restoreAllMocks();
  delete process.env.MONITOR_API_TOKEN;
});

describe("Scenario: no session, no data", () => {
  test("the page sends me to /login", async () => {
    const res = await proxy(page("/?bot=btc"));
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe(`${ORIGIN}/login?next=%2F%3Fbot%3Dbtc`);
  });

  test("GET /api/bots/btc/status answers 401", async () => {
    for (const path of ["/api/bots/btc/status", "/api/bots", "/api/auth/logout"]) {
      const res = await proxy(page(path));
      expect(res.status).toBe(401);
      expect(res.headers.get("x-monitor-session")).toBe("required");
    }
    expect((await proxy(page("/api/bots/btc/status", "access_token=garbage"))).status).toBe(401);
  });

  test("sign-in, refresh and the login page stay reachable", async () => {
    for (const path of ["/api/auth/login", "/api/auth/refresh", "/login"]) {
      expect((await proxy(page(path))).headers.get("x-middleware-next")).toBe("1");
    }
  });

  test("a session opens pages and APIs; /login then goes to the monitor", async () => {
    const cookie = await sessionCookie();
    expect((await proxy(page("/", cookie))).headers.get("x-middleware-next")).toBe("1");
    expect((await proxy(page("/api/bots/btc/status", cookie))).headers.get("x-middleware-next")).toBe("1");
    expect((await proxy(page("/login", cookie))).headers.get("location")).toBe(`${ORIGIN}/`);
  });

  test("any signed-in role reads the investor summary", async () => {
    for (const role of ["admin", "manager", "user"]) {
      const cookie = await sessionCookie({ id: 2, username: "reader", role });
      expect((await proxy(page("/api/bots/btc/summary", cookie))).headers.get("x-middleware-next")).toBe("1");
    }
    expect((await proxy(page("/api/bots/btc/summary"))).status).toBe(401);
  });

  test("performance is refused without a session or token, like every /api route", async () => {
    expect((await proxy(page("/api/bots/btc/performance?range=30d"))).status).toBe(401);
    expect((await proxy(page("/api/bots/btc/performance?range=30d", "access_token=garbage"))).status).toBe(401);
    const cookie = await sessionCookie({ id: 2, username: "reader", role: "user" });
    expect((await proxy(page("/api/bots/btc/performance?range=30d", cookie))).headers.get("x-middleware-next")).toBe("1");
    process.env.MONITOR_API_TOKEN = "s".repeat(40);
    const auth = { authorization: `Bearer ${"s".repeat(40)}` };
    expect((await proxy(page("/api/bots/btc/performance?range=7d", "", auth))).headers.get("x-middleware-next")).toBe("1");
  });

  test("without AUTH_SECRET nothing is served", async () => {
    delete process.env.AUTH_SECRET;
    expect((await proxy(page("/"))).status).toBe(500);
    expect((await proxy(page("/api/bots"))).status).toBe(500);
  });

  test("the service token reads bot data only", async () => {
    const token = "s".repeat(40);
    process.env.MONITOR_API_TOKEN = token;
    const auth = { authorization: `Bearer ${token}` };
    expect((await proxy(page("/api/bots/btc/status", "", auth))).headers.get("x-middleware-next")).toBe("1");
    expect((await proxy(page("/api/bots", "", auth))).headers.get("x-middleware-next")).toBe("1");
    expect((await proxy(page("/", "", auth))).status).toBe(307);
    expect((await proxy(page("/api/bots/btc/status", "", { authorization: `Bearer ${"t".repeat(40)}` }))).status).toBe(401);
    const close = new NextRequest(`${ORIGIN}/api/bots/btc/close`, { method: "POST", headers: auth });
    expect(serviceTokenOk(close)).toBe(false);
    expect((await proxy(close)).status).toBe(401);
    // Too short to be a secret: ignored.
    expect(serviceTokenOk(page("/api/bots", "", { authorization: "Bearer short" }), "short")).toBe(false);
  });
});

describe("Scenario: sign in", () => {
  test("the right password sets HttpOnly, SameSite=Lax cookies", async () => {
    const res = await login(req("/api/auth/login", { body: { username: "admin", password: PASSWORD } }));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ user: { username: "admin", role: "admin" } });
    const { raw, values } = cookies(res);
    expect(raw).toHaveLength(2);
    for (const c of raw) {
      expect(c).toContain("HttpOnly");
      expect(c).toContain("SameSite=Lax");
      expect(c).not.toContain("Secure"); // plain http to localhost
    }
    expect(raw.find((c) => c.startsWith("access_token="))).toContain("Path=/;");
    expect(raw.find((c) => c.startsWith("access_token="))).toContain("Max-Age=900");
    expect(raw.find((c) => c.startsWith("refresh_token="))).toContain("Path=/api/auth;");

    // The access cookie opens the monitor.
    expect((await proxy(page("/", `access_token=${values.access_token}`))).headers.get("x-middleware-next")).toBe("1");
  });

  test("cookies are Secure except on plain http to localhost", () => {
    const r = (url, headers = {}) => new Request(url, { headers });
    expect(cookieSecure(r("http://localhost:3000/", { host: "localhost:3000" }))).toBe(false);
    expect(cookieSecure(r("http://127.0.0.1:3100/", { host: "127.0.0.1:3100" }))).toBe(false);
    expect(cookieSecure(r("http://monitor.lan/", { host: "monitor.lan" }))).toBe(true);
    expect(cookieSecure(r("https://localhost/", { host: "localhost" }))).toBe(true);
    expect(cookieSecure(r("http://localhost:3000/", { host: "localhost:3000", "x-forwarded-proto": "https" }))).toBe(true);
  });

  test("refuses cross-origin and non-JSON sign-ins", async () => {
    const body = { username: "admin", password: PASSWORD };
    expect((await login(req("/api/auth/login", { body, headers: { origin: "http://evil.example" } }))).status).toBe(403);
    expect((await login(req("/api/auth/login", { body, headers: { "content-type": "text/plain" } }))).status).toBe(415);
  });
});

describe("Scenario: same answer for a wrong user or a wrong password", () => {
  test("both answer 401 with the same body and no cookie", async () => {
    const a = await login(req("/api/auth/login", { body: { username: "nobody", password: PASSWORD } }));
    const b = await login(req("/api/auth/login", { body: { username: "admin", password: "not the password" } }));
    for (const res of [a, b]) {
      expect(res.status).toBe(401);
      expect(await res.json()).toEqual({ error: "invalid username or password" });
      expect(res.headers.getSetCookie()).toEqual([]);
    }
  });
});

describe("Scenario: lockout", () => {
  test("after 5 failures the right password gets the same 401", async () => {
    for (let i = 0; i < 5; i++) await login(req("/api/auth/login", { body: { username: "admin", password: "not the password" } }));
    const res = await login(req("/api/auth/login", { body: { username: "admin", password: PASSWORD } }));
    expect(res.status).toBe(401);
    expect(await res.json()).toEqual({ error: "invalid username or password" });
  });
});

describe("Scenario: silent refresh and logout", () => {
  test("refresh gives a new access cookie; after logout the refresh token no longer works", async () => {
    const signed = cookies(await login(req("/api/auth/login", { body: { username: "admin", password: PASSWORD } }))).values;
    const refreshCookie = `refresh_token=${signed.refresh_token}`;

    const r1 = await refresh(req("/api/auth/refresh", { cookie: refreshCookie }));
    expect(r1.status).toBe(200);
    expect(cookies(r1).values.access_token).toBeTruthy();

    const out = await logout(req("/api/auth/logout", { cookie: `access_token=${signed.access_token}; ${refreshCookie}` }));
    expect(out.status).toBe(200);
    expect(cookies(out).raw.every((c) => c.includes("Max-Age=0"))).toBe(true);

    const r2 = await refresh(req("/api/auth/refresh", { cookie: refreshCookie }));
    expect(r2.status).toBe(401);
    expect(cookies(r2).raw.every((c) => c.includes("Max-Age=0"))).toBe(true);
  });
});

describe("Scenario: secrets stay out of the repo", () => {
  test("no password, hash or AUTH_SECRET in API answers or logs", async () => {
    const res = await login(req("/api/auth/login", { body: { username: "admin", password: PASSWORD } }));
    const body = await res.text();
    const { values } = cookies(res);
    for (const text of [body, logs.join("\n")]) {
      expect(text).not.toContain(PASSWORD);
      expect(text).not.toContain("$argon2id$");
      expect(text).not.toContain(TEST_SECRET);
      expect(text).not.toContain(values.refresh_token);
    }
  });
});
