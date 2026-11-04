/**
 * @jest-environment node
 */
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET } from "@/app/api/auth/sessions/route";
import { DELETE } from "@/app/api/auth/sessions/[id]/route";
import { POST as refresh } from "@/app/api/auth/refresh/route";
import { getAuth } from "@/lib/auth/server";
import { browserName } from "@/lib/auth/browser";
import { hashToken, shortSessionId } from "@/lib/auth/service";
import { AuthStore } from "@/lib/auth/store";
import { ACCESS_COOKIE, authKey, REFRESH_COOKIE, signAccess } from "@/lib/auth/tokens";
import { TEST_SECRET, useTestAuth } from "@/test/auth";

// #584: a person lists their own sessions and signs the other browsers out.

const CHROME_WIN = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36";
const SAFARI_IOS = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1";

let dir, auditFile, auth;
/** The audit's session_ended lines (the bootstrap admin's user_created comes first). */
const audit = () =>
  (fs.existsSync(auditFile) ? fs.readFileSync(auditFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []).filter(
    (l) => l.action === "session_ended",
  );
const headers = (cookie, over = {}) => ({ origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...over });

/** One browser of `username` signed in with `userAgent`: { user, refresh, cookie, refreshCookie }. */
async function browser(username, userAgent, role = "user") {
  const user = auth.store.userByName(username) ?? auth.store.createUser({ username, passwordHash: "not a real hash", role });
  const refreshToken = crypto.randomBytes(32).toString("base64url");
  const sid = hashToken(refreshToken);
  auth.store.addSession(sid, user.id, Date.now() + 3600 * 1000, Date.now(), browserName(userAgent));
  const access = await signAccess(user, authKey(TEST_SECRET), Date.now(), sid);
  return { user, cookie: `${ACCESS_COOKIE}=${access}`, refreshCookie: `${REFRESH_COOKIE}=${refreshToken}` };
}

const list = (b) => GET(new Request("http://monitor/api/auth/sessions", { headers: { cookie: b.cookie } }));
const end = (b, id, over = {}) =>
  DELETE(new Request(`http://monitor/api/auth/sessions/${id}`, { method: "DELETE", headers: headers(b.cookie, over), body: "{}" }), {
    params: Promise.resolve({ id }),
  });
const refreshOf = (b) => refresh(new Request("http://monitor/api/auth/refresh", { method: "POST", headers: { cookie: b.refreshCookie } }));

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "sessions-"));
  auditFile = path.join(dir, "audit.jsonl");
  process.env.AUDIT_PATH = auditFile;
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  useTestAuth();
  auth = await getAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
  delete process.env.AUDIT_PATH;
});

describe("Scenario: list own sessions", () => {
  test("bob on two browsers sees two rows, the current one marked, and no token or address", async () => {
    const laptop = await browser("bob", CHROME_WIN);
    await browser("bob", SAFARI_IOS);
    await browser("alice", CHROME_WIN);
    const res = await list(laptop);
    expect(res.status).toBe(200);
    const { sessions } = await res.json();
    expect(sessions).toHaveLength(2);
    expect(sessions.filter((s) => s.current).map((s) => s.browser)).toEqual(["Chrome on Windows"]);
    expect(sessions.map((s) => s.browser).sort()).toEqual(["Chrome on Windows", "Safari on iOS"]);
    for (const s of sessions) {
      expect(Object.keys(s).sort()).toEqual(["browser", "created_at", "current", "id", "last_used_at"]);
      expect(s.id).toMatch(/^[0-9a-f]{16}$/);
    }
    const text = JSON.stringify(sessions);
    for (const hash of auth.store.all("SELECT token_hash FROM refresh_tokens").map((r) => r.token_hash)) expect(text).not.toContain(hash);
    expect(text).not.toMatch(/\d+\.\d+\.\d+\.\d+/);
  });

  test("a session ended elsewhere cannot list (401)", async () => {
    const laptop = await browser("bob", CHROME_WIN);
    auth.store.revokeSessions(laptop.user.id);
    expect((await list(laptop)).status).toBe(401);
  });
});

describe("Scenario: end another session", () => {
  test("its next refresh fails and the audit has session_ended with bob's name", async () => {
    const laptop = await browser("bob", CHROME_WIN);
    const phone = await browser("bob", SAFARI_IOS);
    expect((await refreshOf(phone)).status).toBe(200);
    const other = (await (await list(laptop)).json()).sessions.find((s) => !s.current);

    const res = await end(laptop, other.id);
    expect(res.status).toBe(200);
    expect((await refreshOf(phone)).status).toBe(401);
    // Its access token can no longer change anything either.
    expect((await list(phone)).status).toBe(401);
    expect((await refreshOf(laptop)).status).toBe(200);
    expect(audit()).toEqual([
      expect.objectContaining({ action: "session_ended", username: "bob", role: "user", target_session: shortSessionId(other.id), time: expect.any(String) }),
    ]);
    expect(shortSessionId(other.id)).toHaveLength(8);
    expect((await (await list(laptop)).json()).sessions).toHaveLength(1);
  });
});

describe("Scenario: not someone else's session", () => {
  test("bob's DELETE of alice's session is 404 and nothing changes", async () => {
    const bob = await browser("bob", CHROME_WIN);
    const alice = await browser("alice", SAFARI_IOS);
    const aliceId = (await (await list(alice)).json()).sessions[0].id;
    const res = await end(bob, aliceId);
    expect(res.status).toBe(404);
    expect((await refreshOf(alice)).status).toBe(200);
    expect(audit()).toEqual([]);
  });

  test("an unknown id is 404", async () => {
    const bob = await browser("bob", CHROME_WIN);
    expect((await end(bob, "0000000000000000")).status).toBe(404);
  });
});

describe("Scenario: the current session", () => {
  test("ending the session in use is 409 saying to use Sign out", async () => {
    const bob = await browser("bob", CHROME_WIN);
    const mine = (await (await list(bob)).json()).sessions[0];
    const res = await end(bob, mine.id);
    expect(res.status).toBe(409);
    expect((await res.json()).error).toMatch(/Sign out/);
    expect((await refreshOf(bob)).status).toBe(200);
    expect(audit()).toEqual([]);
  });
});

test.each([
  ["cross-origin", { origin: "http://evil" }, 403],
  ["not JSON", { "content-type": "text/plain" }, 415],
])("%s DELETE is refused and the session lives on", async (_name, over, status) => {
  const laptop = await browser("bob", CHROME_WIN);
  const phone = await browser("bob", SAFARI_IOS);
  const other = (await (await list(laptop)).json()).sessions.find((s) => !s.current);
  expect((await end(laptop, other.id, over)).status).toBe(status);
  expect((await refreshOf(phone)).status).toBe(200);
});

test("a refresh marks the session used, at most once a minute", async () => {
  const store = await AuthStore.open(":memory:");
  const user = store.createUser({ username: "bob", passwordHash: "x", role: "user" });
  store.addSession("h1", user.id, 10_000_000, 1_000_000, "Firefox on Linux");
  store.touchSession("h1", 1_030_000);
  expect(store.liveSessions(user.id, 1_030_000)[0].lastUsedSec).toBe(1000);
  store.touchSession("h1", 1_090_000);
  expect(store.liveSessions(user.id, 1_090_000)[0]).toEqual(expect.objectContaining({ browser: "Firefox on Linux", createdAtSec: 1000, lastUsedSec: 1090 }));
});

test("a database from before #584 gets a public id and a last use for each session", async () => {
  const old = await AuthStore.open(":memory:");
  const user = old.createUser({ username: "bob", passwordHash: "x", role: "user" });
  // As the file was: no public_id, browser or last_used_at.
  for (const c of ["public_id", "browser", "last_used_at"]) old.db.exec(`ALTER TABLE refresh_tokens DROP COLUMN ${c}`);
  old.db.run("INSERT INTO refresh_tokens (token_hash, user_id, expires_at, created_at) VALUES ('h1', ?, 9999999999, 1000)", [user.id]);
  expect(old.migrate()).toBe(true);
  const [s] = old.liveSessions(user.id, 2_000_000);
  expect(s.publicId).toMatch(/^[0-9a-f]{16}$/);
  expect(s).toEqual(expect.objectContaining({ browser: null, createdAtSec: 1000, lastUsedSec: 1000 }));
  expect(old.migrate()).toBe(false);
});

test.each([
  [CHROME_WIN, "Chrome on Windows"],
  [SAFARI_IOS, "Safari on iOS"],
  ["Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", "Safari on macOS"],
  ["Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36 Edg/130.0", "Edge on Windows"],
  ["Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36", "Chrome on Android"],
  ["Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", "Firefox on Linux"],
  ["curl/8.4.0", "Unknown browser"],
  ["", "Unknown browser"],
  [null, "Unknown browser"],
])("browserName(%p) → %s", (ua, name) => {
  expect(browserName(ua)).toBe(name);
});
