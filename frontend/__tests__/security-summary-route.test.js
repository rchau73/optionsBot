/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET } from "@/app/api/auth/security-summary/route";
import { GET as getNotifications, POST as postNotifications } from "@/app/api/notifications/route";
import { getAuth } from "@/lib/auth/server";
import { hashPassword } from "@/lib/auth/password";
import { INVALID_LOGIN } from "@/lib/auth/service";
import { AuthStore } from "@/lib/auth/store";
import { ACCESS_COOKIE } from "@/lib/auth/tokens";
import { useTestAuth } from "@/test/auth";

// #803: a person sees their previous sign-in and the wrong passwords typed
// for their username since, in Active sessions and as one Needs you item.

jest.setTimeout(60000);

const HOUR = 3600 * 1000;
const START = Date.UTC(2026, 10, 15, 9);
// Test values, not secrets (CI runs gitleaks): low entropy on purpose.
const PASSWORD = "bob password for tests";
const IP = "203.0.113.7";

let auth, dir;

const signIn = async (password = PASSWORD, username = "bob") => auth.login({ username, password, ip: IP, userAgent: "curl/8.4.0" });
const cookieOf = (r) => `${ACCESS_COOKIE}=${r.access}`;
const summary = (headers) => GET(new Request("http://monitor/api/auth/security-summary", { headers }));
const summaryOf = async (r) => (await summary({ cookie: cookieOf(r) })).json();
const bell = async (r) => (await getNotifications(new Request("http://monitor/api/notifications", { headers: { cookie: cookieOf(r) } }))).json();
const securityItems = (view) => view.items.filter((i) => i.kind === "security");

beforeEach(async () => {
  jest.useFakeTimers({ now: START, doNotFake: ["nextTick", "setImmediate", "queueMicrotask"] });
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "security-summary-"));
  process.env.BOT_APIS = "";
  process.env.RELEASE_NOTES_DIR = path.join(dir, "releases");
  useTestAuth();
  auth = await getAuth();
  auth.store.createUser({ username: "bob", passwordHash: await hashPassword(PASSWORD), role: "user" });
});
afterEach(() => {
  jest.useRealTimers();
  jest.restoreAllMocks();
  delete process.env.BOT_APIS;
  delete process.env.RELEASE_NOTES_DIR;
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Scenario: no failed attempts", () => {
  test("yesterday's sign-in and 0 attempts; the bell has no security item", async () => {
    await signIn();
    jest.setSystemTime(START + 24 * HOUR);
    const today = await signIn();
    const res = await summary({ cookie: cookieOf(today) });
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(await res.json()).toEqual({ previous_sign_in: new Date(START).toISOString(), first_sign_in: false, failed_since: 0, notice_id: null });
    const view = await bell(today);
    expect(securityItems(view)).toEqual([]);
    expect(view.needs_you).toBe(0);
  });
});

describe("Scenario: failed attempts since the last sign-in", () => {
  test("3 wrong passwords → 3 attempts, one Needs you item until read, then 0 after the next sign-in", async () => {
    await signIn();
    jest.setSystemTime(START + HOUR);
    for (let i = 0; i < 3; i++) expect(await signIn("not the password")).toEqual(expect.objectContaining({ ok: false, status: 401, error: INVALID_LOGIN }));
    jest.setSystemTime(START + 2 * HOUR);
    const back = await signIn();

    const s = await summaryOf(back);
    expect(s).toEqual(expect.objectContaining({ previous_sign_in: new Date(START).toISOString(), first_sign_in: false, failed_since: 3 }));
    expect(s.notice_id).toMatch(/^security:\d+:3$/);

    const view = await bell(back);
    expect(securityItems(view)).toEqual([expect.objectContaining({ id: s.notice_id, group: "needs_you", read: false, title: "3 wrong-password attempts on your account" })]);
    expect(view.needs_you).toBe(1);

    // Opening the dialog marks the item read (what ActiveSessions sends).
    const read = await postNotifications(
      new Request("http://monitor/api/notifications", {
        method: "POST",
        headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie: cookieOf(back) },
        body: JSON.stringify({ action: "read", ids: [s.notice_id] }),
      }),
    );
    expect(read.status).toBe(200);
    expect((await bell(back)).needs_you).toBe(0);

    jest.setSystemTime(START + 3 * HOUR);
    const next = await signIn();
    expect(await summaryOf(next)).toEqual(expect.objectContaining({ previous_sign_in: new Date(START + 2 * HOUR).toISOString(), failed_since: 0, notice_id: null }));
  });

  test("a wrong password while signed in shows at once and brings a read item back", async () => {
    await signIn();
    jest.setSystemTime(START + HOUR);
    await signIn("not the password");
    const back = await signIn();
    const first = await summaryOf(back);
    expect(first.failed_since).toBe(1);
    auth.store.markNotificationsRead(auth.store.userByName("bob").id, [first.notice_id], 90, Date.now());
    await signIn("not the password");
    const later = await summaryOf(back);
    expect(later.failed_since).toBe(2);
    expect(later.notice_id).not.toBe(first.notice_id);
    expect((await bell(back)).needs_you).toBe(1);
  });

  test("a locked username says until when; the lockout and its 401 are unchanged", async () => {
    const here = await signIn();
    for (let i = 0; i < 5; i++) await signIn("not the password");
    // Locked: even the right password gets the one answer.
    expect(await signIn()).toEqual(expect.objectContaining({ ok: false, status: 401, error: INVALID_LOGIN }));
    const s = await summaryOf(here);
    expect(s.failed_since).toBe(5);
    expect(s.locked_until).toBe(new Date(START + 15 * 60 * 1000).toISOString());
  });

  test("an unknown username and a password change's wrong current password count for nobody", async () => {
    const here = await signIn();
    await signIn("not the password", "nobody");
    expect(auth.store.signInState(auth.store.userByName("bob").id).failedSince).toBe(0);
    expect(auth.store.one("SELECT SUM(failed_sign_ins) AS n FROM users").n).toBe(0);
    await auth.changePassword({ accessToken: here.access, current: "not the password", next: "a brand new long password" });
    expect((await summaryOf(here)).failed_since).toBe(0);
  });
});

describe("Scenario: first ever sign-in", () => {
  test("first_sign_in, no previous time and no bell item even after wrong passwords before it", async () => {
    await signIn("not the password");
    const first = await signIn();
    const s = await summaryOf(first);
    expect(s).toEqual(expect.objectContaining({ previous_sign_in: null, first_sign_in: true, notice_id: null }));
    expect(securityItems(await bell(first))).toEqual([]);
  });
});

describe("Scenario: nothing leaks", () => {
  test("only the person's own fields: no IP, browser, token or another user's data", async () => {
    auth.store.createUser({ username: "alice", passwordHash: await hashPassword(PASSWORD), role: "admin" });
    await signIn(PASSWORD, "alice");
    await signIn("not the password", "alice");
    const bob = await signIn();
    const text = JSON.stringify(await summaryOf(bob));
    expect(Object.keys(JSON.parse(text)).sort()).toEqual(["failed_since", "first_sign_in", "notice_id", "previous_sign_in"]);
    expect(text).not.toContain(IP);
    expect(text).not.toMatch(/curl|alice|bob|token/i);
    expect(text).not.toContain(bob.access);
  });

  test("no session, an ended session and the service token answer 401", async () => {
    process.env.MONITOR_API_TOKEN = "q".repeat(40);
    try {
      expect((await summary({})).status).toBe(401);
      expect((await summary({ authorization: `Bearer ${"q".repeat(40)}` })).status).toBe(401);
      const bob = await signIn();
      auth.store.revokeSessions(auth.store.userByName("bob").id);
      expect((await summary({ cookie: cookieOf(bob) })).status).toBe(401);
    } finally {
      delete process.env.MONITOR_API_TOKEN;
    }
  });

  test("guardrail: no log line carries a failed-attempt count with the username", async () => {
    await signIn();
    for (let i = 0; i < 3; i++) await signIn("not the password");
    const back = await signIn();
    await summaryOf(back);
    await bell(back);
    const lines = [...console.log.mock.calls, ...console.error.mock.calls].map((c) => c.join(" "));
    expect(lines.some((l) => l.includes("bob"))).toBe(true);
    expect(lines.filter((l) => l.includes("bob") && /failed_since|failed_sign_ins|attempts/.test(l))).toEqual([]);
  });
});

test("an auth.db from before #803 gets the columns, counts from 0 and no previous sign-in", async () => {
  const old = await AuthStore.open(":memory:");
  const user = old.createUser({ username: "bob", passwordHash: "x", role: "user" });
  old.recordSignIn(user.id, START);
  for (const c of ["previous_sign_in_at", "failed_sign_ins", "failed_sign_ins_before"]) old.db.exec(`ALTER TABLE users DROP COLUMN ${c}`);
  expect(old.migrate()).toBe(true);
  expect(old.signInState(user.id)).toEqual({ lastSignInSec: START / 1000, previousSignInSec: null, signIns: 1, failedSince: 0 });
  expect(old.migrate()).toBe(false);
});
