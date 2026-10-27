/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { AuthStore, normalizeUsername } from "@/lib/auth/store";
import { AuthService, hashToken, INVALID_LOGIN } from "@/lib/auth/service";
import { hashPassword, passwordProblem, verifyPassword } from "@/lib/auth/password";
import { IP_ATTEMPTS, LOCK_WINDOW_MS } from "@/lib/auth/limits";
import { authKey, verifyAccess } from "@/lib/auth/tokens";
import { TEST_SECRET } from "@/test/auth";

jest.setTimeout(30000);

const ADMIN = { ADMIN_USERNAME: "Admin", ADMIN_PASSWORD: "correct horse battery", ADMIN_EMAIL: "owner@example.com" };

/** A service on an in-memory database with a movable clock, the first admin created. */
async function setup(env = ADMIN) {
  const clock = { t: Date.UTC(2026, 9, 19, 12) };
  const store = await AuthStore.open(":memory:");
  const auth = new AuthService({ store, key: authKey(TEST_SECRET), now: () => clock.t });
  await auth.bootstrapAdmin(env);
  return { auth, store, clock };
}

let logs;
beforeEach(() => {
  logs = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
});
afterEach(() => jest.restoreAllMocks());

describe("Scenario: sign in", () => {
  test("the admin created on first start from ADMIN_USERNAME/ADMIN_PASSWORD signs in", async () => {
    const { auth, store, clock } = await setup();
    expect(store.userByName("admin")).toEqual({ id: 1, username: "admin", role: "admin", email: "owner@example.com" });

    const r = await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD, ip: "1.2.3.4" });
    expect(r.ok).toBe(true);
    expect(await verifyAccess(r.access, authKey(TEST_SECRET), clock.t)).toEqual({ id: 1, username: "admin", role: "admin", sid: hashToken(r.refresh) });
    expect(store.sessionUser(hashToken(r.refresh), clock.t)).toBe(1);
  });

  test("the access token lasts 15 minutes and is refused when forged", async () => {
    const { auth, clock } = await setup();
    const { access } = await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD });
    const key = authKey(TEST_SECRET);
    expect(await verifyAccess(access, key, clock.t + 15 * 60 * 1000 - 1000)).not.toBeNull();
    expect(await verifyAccess(access, key, clock.t + 15 * 60 * 1000 + 1000)).toBeNull();
    expect(await verifyAccess(access, authKey("b".repeat(40)), clock.t)).toBeNull();
    expect(await verifyAccess(`${access.split(".").slice(0, 2).join(".")}.`, key, clock.t)).toBeNull();
  });

  test("the first admin is created once, and never without a valid password", async () => {
    const { auth, store } = await setup();
    expect(await auth.bootstrapAdmin({ ...ADMIN, ADMIN_USERNAME: "other" })).toBeNull();
    expect(store.countUsers()).toBe(1);

    expect((await setup({})).store.countUsers()).toBe(0);
    expect((await setup({ ADMIN_USERNAME: "admin", ADMIN_PASSWORD: "short" })).store.countUsers()).toBe(0);
    expect((await setup({ ADMIN_USERNAME: "a b", ADMIN_PASSWORD: ADMIN.ADMIN_PASSWORD })).store.countUsers()).toBe(0);
  });

  test("passwords are argon2id and at least 12 characters", async () => {
    const hash = await hashPassword("twelve chars!");
    expect(hash).toMatch(/^\$argon2id\$v=19\$m=19456,t=2,p=1\$/);
    expect(await verifyPassword("twelve chars!", hash)).toBe(true);
    expect(await verifyPassword("twelve chars?", hash)).toBe(false);
    expect(await verifyPassword("x", "not a hash")).toBe(false);
    expect(passwordProblem("eleven char")).toMatch(/12/);
    expect(passwordProblem("twelve chars")).toBeNull();
  });
});

describe("Scenario: same answer for a wrong user or a wrong password", () => {
  async function timed(auth, username, password) {
    const t0 = process.hrtime.bigint();
    const r = await auth.login({ username, password });
    return { r, ms: Number(process.hrtime.bigint() - t0) / 1e6 };
  }

  test("both answer 401 'invalid username or password' in about the same time", async () => {
    const { auth } = await setup();
    await timed(auth, "nobody", "warm up the dummy hash");
    await timed(auth, "admin", "warm up the real hash");
    const unknown = [];
    const wrong = [];
    for (let i = 0; i < 7; i++) {
      unknown.push(await timed(auth, `nobody${i}`, "wrong password"));
      wrong.push(await timed(auth, "admin", "wrong password"));
    }
    for (const { r } of [...unknown, ...wrong]) expect(r).toEqual({ ok: false, status: 401, error: INVALID_LOGIN });
    // The fastest of 7 interleaved tries per path: load only ever adds time, so
    // the minimum is each path's own cost even on a busy machine (a median of 3
    // measured 7x apart under load, 2026-10-27), while a path that skipped the
    // argon2id verification would still be many times faster.
    const fastest = (xs) => Math.min(...xs.map((x) => x.ms));
    const [a, b] = [fastest(unknown), fastest(wrong)];
    // Both paths run exactly one argon2id verification (~tens of ms each).
    expect(Math.max(a, b) / Math.min(a, b)).toBeLessThan(3);
  });

  test("malformed usernames and empty fields get no hint either", async () => {
    const { auth } = await setup();
    expect((await auth.login({ username: "x y", password: "whatever password" })).error).toBe(INVALID_LOGIN);
    expect((await auth.login({ username: "admin", password: "" })).status).toBe(400);
    expect((await auth.login({ username: 42, password: "x" })).status).toBe(400);
  });
});

describe("Scenario: lockout", () => {
  test("5 failures for a username within 15 minutes refuse the right password until 15 minutes after the last", async () => {
    const { auth, clock } = await setup();
    for (let i = 0; i < 5; i++) {
      clock.t += 60 * 1000;
      expect((await auth.login({ username: "admin", password: "wrong password", ip: `10.0.0.${i}` })).status).toBe(401);
    }
    const last = clock.t;
    const right = () => auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD, ip: "10.0.1.1" });

    const locked = await right();
    expect(locked).toEqual({ ok: false, status: 401, error: INVALID_LOGIN });
    clock.t = last + LOCK_WINDOW_MS - 1000;
    expect((await right()).ok).toBe(false);
    clock.t = last + LOCK_WINDOW_MS;
    expect((await right()).ok).toBe(true);
    expect(logs.some((l) => l.includes("username locked"))).toBe(true);
  });

  test("failures spread over more than 15 minutes do not lock, and a success clears them", async () => {
    const { auth, clock } = await setup();
    for (let i = 0; i < 4; i++) await auth.login({ username: "admin", password: "wrong password" });
    clock.t += LOCK_WINDOW_MS;
    await auth.login({ username: "admin", password: "wrong password" });
    expect((await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD })).ok).toBe(true);
    for (let i = 0; i < 4; i++) await auth.login({ username: "admin", password: "wrong password" });
    expect((await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD })).ok).toBe(true);
  });

  test("an IP gets 20 sign-ins a minute, then 429 with Retry-After", async () => {
    const { auth, clock } = await setup();
    for (let i = 0; i < IP_ATTEMPTS; i++) await auth.login({ username: `u${i}x`, password: "wrong password", ip: "6.6.6.6" });
    const r = await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD, ip: "6.6.6.6" });
    expect(r.status).toBe(429);
    expect(r.retryAfter).toBeGreaterThan(0);
    expect((await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD, ip: "7.7.7.7" })).ok).toBe(true);
    clock.t += 61 * 1000;
    expect((await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD, ip: "6.6.6.6" })).ok).toBe(true);
  });
});

describe("lockout kept in auth.db (#213)", () => {
  const ANA_PASSWORD = "ana right password";

  /** A service on a database file with "ana" in it; `restart()` opens a new store and service on the same file. */
  async function fileSetup() {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "monitor-lock-"));
    const file = path.join(dir, "auth.db");
    const clock = { t: Date.UTC(2026, 9, 19, 12) };
    const open = async () => {
      const store = await AuthStore.open(file);
      return { store, auth: new AuthService({ store, key: authKey(TEST_SECRET), now: () => clock.t }) };
    };
    const first = await open();
    first.store.createUser({ username: "ana", passwordHash: await hashPassword(ANA_PASSWORD), role: "user" });
    return { ...first, clock, restart: open };
  }

  const fail = (auth) => auth.login({ username: "ana", password: "wrong password", ip: "10.0.0.1" });
  const right = (auth) => auth.login({ username: "ana", password: ANA_PASSWORD, ip: "10.0.0.2" });

  async function lockAna(auth, clock) {
    for (let i = 0; i < 5; i++) {
      clock.t += 60 * 1000;
      await fail(auth);
    }
    return clock.t;
  }

  test("Scenario: lock survives a restart", async () => {
    const { auth, clock, restart } = await fileSetup();
    const lockedAt = await lockAna(auth, clock);

    const { auth: restarted, store } = await restart();
    expect(store.loginLockUntil("ana")).toBe(lockedAt + LOCK_WINDOW_MS);
    clock.t = lockedAt + 60 * 1000;
    logs.length = 0;
    expect(await right(restarted)).toEqual({ ok: false, status: 401, error: INVALID_LOGIN });
    expect(await fail(restarted)).toEqual({ ok: false, status: 401, error: INVALID_LOGIN });
    expect(logs.some((l) => l.includes("username locked"))).toBe(true);
  });

  test("Scenario: lock ends on time", async () => {
    const { auth, store, clock } = await fileSetup();
    const lockedAt = await lockAna(auth, clock);
    clock.t = lockedAt + LOCK_WINDOW_MS - 1000;
    expect((await right(auth)).ok).toBe(false);
    // An attempt during the lock is not a failure.
    expect(store.loginFailureTimes("ana")).toEqual([]);

    clock.t = lockedAt + LOCK_WINDOW_MS + 1000;
    expect((await right(auth)).ok).toBe(true);
    expect(store.loginFailureTimes("ana")).toEqual([]);
  });

  test("a success clears failures in the file, so a restart does not bring them back", async () => {
    const { auth, store, clock, restart } = await fileSetup();
    for (let i = 0; i < 4; i++) await fail(auth);
    expect(store.loginFailureTimes("ana")).toHaveLength(4);
    expect((await right(auth)).ok).toBe(true);
    const { auth: restarted, store: again } = await restart();
    expect(again.loginFailureTimes("ana")).toEqual([]);
    clock.t += 1000;
    await fail(restarted);
    expect((await right(restarted)).ok).toBe(true);
  });

  test("Scenario: old failures expire", async () => {
    const { auth, store, clock } = await fileSetup();
    const start = clock.t;
    for (let i = 0; i < 4; i++) await fail(auth);
    await auth.login({ username: "ghost", password: "wrong password" }); // an unknown name is counted too
    expect(store.loginFailureTimes("ghost")).toEqual([start]);

    clock.t = start + 16 * 60 * 1000;
    await fail(auth);
    expect(store.loginLockUntil("ana")).toBe(0);
    expect(store.loginFailureTimes("ana")).toEqual([clock.t]);
    expect(store.loginFailureTimes("ghost")).toEqual([]);
    expect((await right(auth)).ok).toBe(true);
  });

  test("ended locks are deleted when the next lock is set", async () => {
    const { auth, store, clock } = await fileSetup();
    const lockedAt = await lockAna(auth, clock);
    clock.t = lockedAt + LOCK_WINDOW_MS;
    for (let i = 0; i < 5; i++) await auth.login({ username: "bob", password: "wrong password" });
    expect(store.loginLockUntil("ana")).toBe(0);
    expect(store.loginLockUntil("bob")).toBe(clock.t + LOCK_WINDOW_MS);
  });

  test("Scenario: existing database", async () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "monitor-old-"));
    const file = path.join(dir, "auth.db");
    const old = await AuthStore.open(file);
    old.createUser({ username: "ana", passwordHash: await hashPassword(ANA_PASSWORD), role: "user" });
    old.addSession("h1", 1, Date.now() + 60000);
    // A file written before this change has neither lockout table.
    old.db.exec("DROP TABLE login_failures; DROP TABLE login_locks;");
    old.save();

    const store = await AuthStore.open(file);
    const tables = store.all("SELECT name FROM sqlite_master WHERE type = 'table'").map((t) => t.name);
    expect(tables).toEqual(expect.arrayContaining(["users", "refresh_tokens", "login_failures", "login_locks"]));
    expect(store.userByName("ana")).toMatchObject({ username: "ana", role: "user" });
    expect(store.sessionUser("h1")).toBe(1);
    const auth = new AuthService({ store, key: authKey(TEST_SECRET) });
    expect((await right(auth)).ok).toBe(true);
  });
});

describe("Scenario: silent refresh and logout", () => {
  test("a valid refresh token gives a new access token, until logout", async () => {
    const { auth, clock } = await setup();
    const { refresh } = await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD });
    clock.t += 20 * 60 * 1000; // access token expired
    const r1 = await auth.refresh(refresh);
    expect(r1.ok).toBe(true);
    expect(await verifyAccess(r1.access, authKey(TEST_SECRET), clock.t)).toMatchObject({ username: "admin" });
    expect((await auth.refresh(refresh)).ok).toBe(true); // reusable (several tabs)

    auth.logout(refresh, "admin");
    expect((await auth.refresh(refresh)).ok).toBe(false);
    expect((await auth.refresh("made-up")).ok).toBe(false);
    expect((await auth.refresh(null)).ok).toBe(false);
  });

  test("a refresh token expires after 14 days", async () => {
    const { auth, clock } = await setup();
    const { refresh } = await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD });
    clock.t += 14 * 24 * 3600 * 1000 - 1000;
    expect((await auth.refresh(refresh)).ok).toBe(true);
    clock.t += 2000;
    expect((await auth.refresh(refresh)).ok).toBe(false);
  });

  test("a password change, a role change or a deletion ends every session of the user", async () => {
    const { auth, store } = await setup();
    const login = async () => (await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD })).refresh;

    const a = await login();
    const b = await login();
    expect(store.sessionCount(1)).toBe(2);
    store.setPassword(1, await hashPassword(ADMIN.ADMIN_PASSWORD));
    expect((await auth.refresh(a)).ok).toBe(false);
    expect((await auth.refresh(b)).ok).toBe(false);

    const c = await login();
    store.setRole(1, "manager");
    expect((await auth.refresh(c)).ok).toBe(false);

    const d = await login();
    store.deleteUser(1);
    expect((await auth.refresh(d)).ok).toBe(false);
    expect(store.sessionCount(1)).toBe(0);
  });
});

describe("Scenario: secrets stay out of the repo", () => {
  test("no password, hash or token reaches a log line or a stored row", async () => {
    const { auth, store } = await setup();
    const ok = await auth.login({ username: "admin", password: ADMIN.ADMIN_PASSWORD, ip: "1.1.1.1" });
    await auth.login({ username: "admin", password: "the wrong password", ip: "1.1.1.1" });
    auth.logout(ok.refresh, "admin");
    const hash = store.passwordHash(1);
    const text = logs.join("\n");
    for (const secret of [ADMIN.ADMIN_PASSWORD, "the wrong password", hash, ok.access, ok.refresh, TEST_SECRET]) {
      expect(text).not.toContain(secret);
    }
    expect(text).toContain('"msg":"signed in"');
    // Only the refresh token's SHA-256 is stored.
    const rows = JSON.stringify(store.all("SELECT * FROM refresh_tokens"));
    expect(rows).not.toContain(ok.refresh);
    expect(store.userByName("admin")).not.toHaveProperty("password_hash");
  });

  test("AUTH_SECRET is refused when missing or shorter than 32 bytes", () => {
    expect(() => authKey(undefined)).toThrow(/AUTH_SECRET/);
    expect(() => authKey("x".repeat(31))).toThrow(/32/);
    expect(authKey("x".repeat(32))).toBeInstanceOf(Uint8Array);
  });
});

describe("user database file", () => {
  test("survives a restart and is written owner-only", async () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "monitor-auth-"));
    const file = path.join(dir, "monitor", "auth.db");
    const first = await AuthStore.open(file);
    first.createUser({ username: "Alice", passwordHash: await hashPassword("alice password"), role: "user" });
    first.addSession("h1", 1, Date.now() + 60000);

    const again = await AuthStore.open(file);
    expect(again.userByName("ALICE")).toMatchObject({ username: "alice", role: "user" });
    expect(again.sessionUser("h1")).toBe(1);
    expect(fs.statSync(file).mode & 0o077).toBe(0);
    expect(() => again.createUser({ username: "alice", passwordHash: "x", role: "user" })).toThrow();
    expect(() => again.createUser({ username: "bob", passwordHash: "x", role: "root" })).toThrow(/role/);
  });

  test("usernames are 3–64 of a-z 0-9 . _ - (case-insensitive)", () => {
    expect(normalizeUsername("  Ops.Team_1 ")).toBe("ops.team_1");
    expect(normalizeUsername("ab")).toBeNull();
    expect(normalizeUsername("a b c")).toBeNull();
    expect(normalizeUsername(undefined)).toBeNull();
  });
});
