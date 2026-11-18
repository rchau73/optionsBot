/**
 * @jest-environment node
 */
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as refresh } from "@/app/api/auth/refresh/route";
import { getAuth } from "@/lib/auth/server";
import { AuthService, hashToken, shortSessionId } from "@/lib/auth/service";
import { AuthStore } from "@/lib/auth/store";
import { IDLE_LIMIT_HOURS, idleExpiry, idleSignOutMessage, limitWords } from "@/lib/auth/idle";
import { refreshOutcome } from "@/lib/api/http";
import { signedOutMessage } from "@/lib/session";
import { authKey, REFRESH_COOKIE } from "@/lib/auth/tokens";
import { TEST_SECRET, useTestAuth } from "@/test/auth";

// #821: a session nobody used for its role's limit ends by itself.

const HOUR = 3600 * 1000;
const MINUTE = 60 * 1000;

/** A service on an in-memory database with a movable clock and an audit sink. */
async function setup() {
  const clock = { t: Date.UTC(2026, 10, 17, 12) };
  const store = await AuthStore.open(":memory:");
  const audit = [];
  const auth = new AuthService({ store, key: authKey(TEST_SECRET), now: () => clock.t, audit: (e) => audit.push(e) });
  return { auth, store, clock, audit };
}

/** A session of a new `role` user opened now: { refresh, sid, publicId }. */
function session({ store, clock }, role, username = role) {
  const user = store.createUser({ username, passwordHash: "not a real hash", role }, clock.t);
  const refreshToken = crypto.randomBytes(32).toString("base64url");
  const sid = hashToken(refreshToken);
  const publicId = store.addSession(sid, user.id, clock.t + 14 * 24 * HOUR, clock.t, "Chrome on Windows");
  return { refresh: refreshToken, sid, publicId };
}

beforeEach(() => {
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => jest.restoreAllMocks());

describe("Scenario: an admin's idle session ends", () => {
  test("12 h 1 min unused: refused, the row deleted, session_expired audited", async () => {
    const s = await setup();
    const admin = session(s, "admin");
    s.clock.t += 12 * HOUR + MINUTE;
    expect(await s.auth.refresh(admin.refresh)).toEqual({ ok: false, idleLimitHours: 12 });
    expect(s.store.liveSession(admin.sid, s.clock.t)).toBeNull();
    expect(s.audit).toEqual([
      { time: new Date(s.clock.t).toISOString(), action: "session_expired", username: "admin", role: "admin", target_session: shortSessionId(admin.publicId), idle_hours: 12 },
    ]);
    // The row is gone: a second try is an ordinary refusal, audited once.
    expect(await s.auth.refresh(admin.refresh)).toEqual({ ok: false });
    expect(s.audit).toHaveLength(1);
  });

  test("exactly 12 h unused is still within the limit", async () => {
    const s = await setup();
    const admin = session(s, "admin");
    s.clock.t += 12 * HOUR;
    expect((await s.auth.refresh(admin.refresh)).ok).toBe(true);
  });
});

describe("Scenario: the same idle time does not end a user's session", () => {
  test.each(["user", "manager"])("a %s session last used 13 hours ago is renewed", async (role) => {
    const s = await setup();
    const one = session(s, role);
    s.clock.t += 13 * HOUR;
    const r = await s.auth.refresh(one.refresh);
    expect(r.ok).toBe(true);
    expect(r.access).toEqual(expect.any(String));
    expect(s.store.liveSession(one.sid, s.clock.t).lastUsedSec).toBe(Math.floor(s.clock.t / 1000));
    expect(s.audit).toEqual([]);
  });
});

describe("Scenario: use keeps the session alive", () => {
  test("an admin session used every 11 hours for 3 days never expires for inactivity", async () => {
    const s = await setup();
    const admin = session(s, "admin");
    for (let i = 0; i < 7; i++) {
      s.clock.t += 11 * HOUR;
      expect((await s.auth.refresh(admin.refresh)).ok).toBe(true);
    }
    expect(s.audit).toEqual([]);
  });
});

describe("Scenario: other expiries are unchanged", () => {
  test("a session older than 14 days ends as before, with no inactivity reason and no audit line", async () => {
    const s = await setup();
    const one = session(s, "user");
    for (let d = 0; d < 14; d++) {
      s.clock.t += 24 * HOUR;
      const r = await s.auth.refresh(one.refresh);
      if (d < 13) expect(r.ok).toBe(true);
      else expect(r).toEqual({ ok: false });
    }
    expect(s.audit).toEqual([]);
  });

  test("a signed-out session ends as before", async () => {
    const s = await setup();
    const admin = session(s, "admin");
    s.auth.logout(admin.refresh, "admin");
    s.clock.t += 13 * HOUR;
    expect(await s.auth.refresh(admin.refresh)).toEqual({ ok: false });
    expect(s.audit.map((e) => e.action)).toEqual(["signed_out"]);
  });

  test("the login page shows nothing for an ordinary expiry, a sign-out or a made-up value", () => {
    expect(signedOutMessage("?next=%2F")).toBeNull();
    expect(signedOutMessage("")).toBeNull();
    expect(signedOutMessage("?next=%2F&idle=5")).toBeNull();
    expect(signedOutMessage("?idle=%3Cscript%3E")).toBeNull();
  });
});

describe("Scenario: the audit line holds no secret", () => {
  test("session_expired has no token, hash or address", async () => {
    const s = await setup();
    const admin = session(s, "admin");
    s.clock.t += 13 * HOUR;
    await s.auth.refresh(admin.refresh);
    const [line] = s.audit;
    expect(Object.keys(line).sort()).toEqual(["action", "idle_hours", "role", "target_session", "time", "username"]);
    const text = JSON.stringify(line);
    expect(text).not.toContain(admin.refresh);
    expect(text).not.toContain(admin.sid);
    expect(text).not.toContain(admin.publicId);
    expect(text).not.toMatch(/\d+\.\d+\.\d+\.\d+/);
  });
});

describe("the refresh route and the login page", () => {
  let dir;
  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "idle-"));
    process.env.AUDIT_PATH = path.join(dir, "audit.jsonl");
    useTestAuth();
  });
  afterEach(() => {
    fs.rmSync(dir, { recursive: true, force: true });
    delete process.env.AUDIT_PATH;
  });

  test("an idle admin's refresh is the monitor's own 401 with the limit, and the page says 12 hours", async () => {
    const auth = await getAuth();
    const user = auth.store.createUser({ username: "boss", passwordHash: "not a real hash", role: "admin" });
    const token = crypto.randomBytes(32).toString("base64url");
    auth.store.addSession(hashToken(token), user.id, Date.now() + HOUR, Date.now() - 12 * HOUR - MINUTE, null);
    const res = await refresh(new Request("http://monitor/api/auth/refresh", { method: "POST", headers: { cookie: `${REFRESH_COOKIE}=${token}` } }));
    expect(res.status).toBe(401);
    expect(res.headers.get("x-monitor-session")).toBe("required");
    expect(res.headers.getSetCookie().join(";")).toContain("Max-Age=0");
    const outcome = await refreshOutcome(res);
    expect(outcome).toEqual({ ok: false, idle: 12 });
    expect(signedOutMessage(`?next=%2F&idle=${outcome.idle}`)).toBe("You were signed out because the session was not used for 12 hours.");
    const lines = fs.readFileSync(process.env.AUDIT_PATH, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
    expect(lines.filter((l) => l.action === "session_expired")).toEqual([expect.objectContaining({ username: "boss", role: "admin", idle_hours: 12 })]);
  });

  test("an unknown refresh token is the plain 401, without the header or a limit", async () => {
    await getAuth();
    const res = await refresh(new Request("http://monitor/api/auth/refresh", { method: "POST", headers: { cookie: `${REFRESH_COOKIE}=nope` } }));
    expect(res.status).toBe(401);
    expect(res.headers.get("x-monitor-session")).toBeNull();
    expect(await refreshOutcome(res)).toEqual({ ok: false });
  });
});

describe("lib/auth/idle.js", () => {
  test("the limits: 12 h for an admin, 14 days otherwise, the shortest for an unknown role", () => {
    expect(IDLE_LIMIT_HOURS).toEqual({ admin: 12, manager: 336, user: 336 });
    expect(idleExpiry("nobody", 0, (12 * 3600 + 1) * 1000)).toEqual({ idleHours: 12, limitHours: 12 });
    expect(idleExpiry("user", 0, 13 * HOUR)).toBeNull();
  });

  test("limits in plain words", () => {
    expect(limitWords(12)).toBe("12 hours");
    expect(limitWords(336)).toBe("14 days");
    expect(limitWords(1)).toBe("1 hour");
    expect(idleSignOutMessage(336)).toBe("You were signed out because the session was not used for 14 days.");
    expect(idleSignOutMessage(null)).toBeNull();
  });
});
