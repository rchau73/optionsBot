/**
 * @jest-environment node
 */
// #171: roles enforced on the server, the user re-read on every
// state-changing request, and the person named down to the bot.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as close } from "@/app/api/bots/[bot]/close/route";
import { auditPath, fileAudit } from "@/lib/auth/audit";
import { POST as rebalance } from "@/app/api/bots/[bot]/rebalance/route";
import { AuthStore } from "@/lib/auth/store";
import { AuthService } from "@/lib/auth/service";
import { roleAtLeast } from "@/lib/auth/roles";
import { authKey } from "@/lib/auth/tokens";
import { hashPassword } from "@/lib/auth/password";
import { signedIn, TEST_SECRET, useTestAuth } from "@/test/auth";

jest.setTimeout(30000);

const ORIGIN = "http://localhost:3000";
const req = (bot, action, body, cookie) =>
  new Request(`${ORIGIN}/api/bots/${bot}/${action}`, {
    method: "POST",
    headers: { "content-type": "application/json", origin: ORIGIN, host: "localhost:3000", cookie },
    body: JSON.stringify(body),
  });
const ctx = (bot) => ({ params: Promise.resolve({ bot }) });
const closeAs = (cookie, ids = ["p-2"]) => close(req("btc", "close", { position_ids: ids }, cookie), ctx("btc"));
const rebalanceAs = (cookie, dryRun = true) => rebalance(req("btc", "rebalance", { dry_run: dryRun }, cookie), ctx("btc"));

const realFetch = global.fetch;
beforeEach(() => {
  process.env.BOT_APIS = "btc=http://bot-btc:8081";
  process.env.BOT_ADMIN_TOKEN = "s3cret";
  useTestAuth();
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  global.fetch = jest.fn(async () => new Response('{"results":[{"position_id":"p-2","instrument":"BTC-6NOV26-76000-P","qty":0.1,"filled":0.1}]}', { status: 200 }));
});
afterEach(() => {
  global.fetch = realFetch;
  delete process.env.BOT_ADMIN_TOKEN;
  jest.restoreAllMocks();
});

const sentActor = () => JSON.parse(global.fetch.mock.calls.at(-1)[1].body).actor;

describe("roles", () => {
  test("admin satisfies manager satisfies user; unknown roles satisfy nothing", () => {
    expect(roleAtLeast("admin", "manager")).toBe(true);
    expect(roleAtLeast("manager", "manager")).toBe(true);
    expect(roleAtLeast("manager", "admin")).toBe(false);
    expect(roleAtLeast("user", "manager")).toBe(false);
    expect(roleAtLeast("user", "user")).toBe(true);
    expect(roleAtLeast("root", "user")).toBe(false);
    expect(roleAtLeast(undefined, "user")).toBe(false);
    expect(roleAtLeast("admin", "superuser")).toBe(false);
  });
});

describe("Scenario: a user cannot act", () => {
  test("POST close or rebalance as a user answers 403 and the bot receives nothing", async () => {
    const { cookie } = await signedIn("carl", "user");
    const c = await closeAs(cookie);
    expect(c.status).toBe(403);
    expect((await c.json()).error).toMatch(/manager role required/);
    expect((await rebalanceAs(cookie, true)).status).toBe(403);
    expect((await rebalanceAs(cookie, false)).status).toBe(403);
    expect(global.fetch).not.toHaveBeenCalled();
  });
});

describe("Scenario: a manager closes a leg and is named", () => {
  test("the bot receives actor bob (manager) with the close and with a rebalance", async () => {
    const { cookie } = await signedIn("bob", "manager");
    expect((await closeAs(cookie)).status).toBe(200);
    expect(sentActor()).toEqual({ user: "bob", role: "manager" });
    expect(JSON.parse(global.fetch.mock.calls[0][1].body).position_ids).toEqual(["p-2"]);
    expect((await rebalanceAs(cookie, false)).status).toBe(200);
    expect(sentActor()).toEqual({ user: "bob", role: "manager" });
  });

  test("an admin acts as well, named as admin", async () => {
    const { cookie } = await signedIn("admin", "admin");
    expect((await closeAs(cookie)).status).toBe(200);
    expect(sentActor()).toEqual({ user: "admin", role: "admin" });
  });
});

describe("Scenario: a demoted manager is stopped at once", () => {
  test("bob's still-valid token is refused with 403 after a demotion, and his sessions are revoked", async () => {
    const { cookie, user, auth } = await signedIn("bob", "manager");
    const admin = auth.store.userByName("admin");
    expect((await closeAs(cookie)).status).toBe(200);
    global.fetch.mockClear();

    expect(auth.changeRole(admin, user.id, "user").ok).toBe(true);
    expect((await closeAs(cookie)).status).toBe(403);
    expect(auth.store.sessionCount(user.id)).toBe(0);
    expect(global.fetch).not.toHaveBeenCalled();
  });

  test("a deleted user's token is refused (401) before the bot", async () => {
    const { cookie, user, auth } = await signedIn("bob", "manager");
    expect(auth.deleteUser(auth.store.userByName("admin"), user.id).ok).toBe(true);
    expect((await closeAs(cookie)).status).toBe(401);
    expect(global.fetch).not.toHaveBeenCalled();
  });
});

describe("Scenario: sign out everywhere", () => {
  test("bob signed in on two devices: after sign out everywhere both refreshes fail and his next close is refused", async () => {
    const store = await AuthStore.open(":memory:");
    const auth = new AuthService({ store, key: authKey(TEST_SECRET) });
    await auth.bootstrapAdmin({ ADMIN_USERNAME: "admin", ADMIN_PASSWORD: "admin password for tests" });
    store.createUser({ username: "bob", passwordHash: await hashPassword("bob's password ok"), role: "manager" });
    const phone = await auth.login({ username: "bob", password: "bob's password ok" });
    const laptop = await auth.login({ username: "bob", password: "bob's password ok" });
    expect((await auth.actingUser(laptop.access, "manager")).ok).toBe(true);

    expect(auth.signOutEverywhere(store.userByName("admin"), phone.user.id).ok).toBe(true);
    expect((await auth.refresh(phone.refresh)).ok).toBe(false);
    expect((await auth.refresh(laptop.refresh)).ok).toBe(false);
    expect(await auth.actingUser(laptop.access, "manager")).toMatchObject({ ok: false, status: 401 });
    expect(await auth.actingUser(phone.access, "manager")).toMatchObject({ ok: false, status: 401 });
  });

  test("through the route: a token whose session ended, or one without a session, gets 401 and the bot nothing", async () => {
    const { cookie, user, auth } = await signedIn("bob", "manager");
    auth.signOutEverywhere(auth.store.userByName("admin"), user.id);
    const res = await closeAs(cookie);
    expect(res.status).toBe(401);
    expect(res.headers.get("x-monitor-session")).toBe("required");
    expect(global.fetch).not.toHaveBeenCalled();
  });
});

describe("Scenario: there is always an admin", () => {
  async function service() {
    const store = await AuthStore.open(":memory:");
    const auth = new AuthService({ store, key: authKey(TEST_SECRET) });
    await auth.bootstrapAdmin({ ADMIN_USERNAME: "admin", ADMIN_PASSWORD: "admin password for tests" });
    return { auth, store, admin: store.userByName("admin") };
  }

  test("the only admin can neither delete themselves nor change their role", async () => {
    const { auth, store, admin } = await service();
    expect(auth.deleteUser(admin, admin.id)).toMatchObject({ ok: false, status: 409 });
    for (const role of ["manager", "user"]) {
      const r = auth.changeRole(admin, admin.id, role);
      expect(r).toMatchObject({ ok: false, status: 409 });
      expect(r.error).toMatch(/always be an admin/);
    }
    expect(store.userByName("admin").role).toBe("admin");
    expect(store.countAdmins()).toBe(1);
  });

  test("with a second admin, one may be demoted; nobody deletes themselves; bad roles and unknown users are refused", async () => {
    const { auth, store, admin } = await service();
    const other = store.createUser({ username: "ops", passwordHash: "x", role: "admin" });
    expect(auth.deleteUser(admin, admin.id)).toMatchObject({ ok: false, status: 409 });
    expect(auth.changeRole(admin, other.id, "manager")).toMatchObject({ ok: true, user: { username: "ops", role: "manager" } });
    expect(auth.changeRole(admin, admin.id, "user")).toMatchObject({ ok: false, status: 409 });
    expect(auth.changeRole(admin, other.id, "root")).toMatchObject({ ok: false, status: 400 });
    expect(auth.changeRole(admin, 999, "user")).toMatchObject({ ok: false, status: 404 });
    expect(auth.deleteUser(admin, other.id)).toEqual({ ok: true });
  });
});

describe("audit trail (data/monitor/audit.jsonl)", () => {
  test("sign-ins, failures, lockouts, user changes and manual actions name the person and the time; never a secret", async () => {
    const entries = [];
    const store = await AuthStore.open(":memory:");
    const auth = new AuthService({ store, key: authKey(TEST_SECRET), audit: (e) => entries.push(e) });
    await auth.bootstrapAdmin({ ADMIN_USERNAME: "admin", ADMIN_PASSWORD: "admin password for tests" });
    const bob = store.createUser({ username: "bob", passwordHash: await hashPassword("bob's password ok"), role: "manager" });
    const ok = await auth.login({ username: "bob", password: "bob's password ok", ip: "1.1.1.1" });
    for (let i = 0; i < 5; i++) await auth.login({ username: "bob", password: "a wrong password", ip: "1.1.1.1" });
    auth.recordAction(bob, "manual_close", { bot: "btc", position_ids: ["p-2"], status: 200 });
    auth.changeRole(store.userByName("admin"), bob.id, "user");

    const actions = entries.map((e) => e.action);
    expect(actions).toEqual(expect.arrayContaining(["user_created", "signed_in", "sign_in_failed", "locked", "manual_close", "role_changed"]));
    expect(entries.find((e) => e.action === "locked")).toMatchObject({ username: "bob", failures: 5 });
    expect(entries.find((e) => e.action === "manual_close")).toMatchObject({ username: "bob", role: "manager", bot: "btc" });
    expect(entries.find((e) => e.action === "role_changed")).toMatchObject({ username: "admin", target: "bob", from: "manager", to: "user" });
    for (const e of entries) expect(Date.parse(e.time)).not.toBeNaN();
    const text = JSON.stringify(entries);
    for (const secret of ["bob's password ok", "a wrong password", "admin password for tests", ok.access, ok.refresh, store.passwordHash(bob.id)]) {
      expect(text).not.toContain(secret);
    }
  });

  test("audit.jsonl sits beside the user database, owner-only, one JSON line per entry; off in memory", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "monitor-audit-"));
    const file = auditPath(path.join(dir, "monitor", "auth.db"), undefined);
    expect(file).toBe(path.join(dir, "monitor", "audit.jsonl"));
    expect(auditPath(":memory:", undefined)).toBeNull();
    expect(auditPath(":memory:", "/x/a.jsonl")).toBe("/x/a.jsonl");
    const sink = fileAudit(file);
    sink({ action: "signed_in", username: "bob" });
    sink({ action: "locked", username: "bob" });
    const lines = fs.readFileSync(file, "utf8").trim().split("\n").map((l) => JSON.parse(l));
    expect(lines.map((l) => l.action)).toEqual(["signed_in", "locked"]);
    expect(fs.statSync(file).mode & 0o077).toBe(0);
  });

  test("the close route audits the person, the bot and what it closed", async () => {
    const { cookie, auth } = await signedIn("bob", "manager");
    const entries = [];
    auth.auditSink = (e) => entries.push(e);
    await closeAs(cookie);
    expect(entries).toEqual([
      expect.objectContaining({
        action: "manual_close",
        username: "bob",
        role: "manager",
        bot: "btc",
        status: 200,
        results: [{ instrument: "BTC-6NOV26-76000-P", qty: 0.1, filled: 0.1 }],
      }),
    ]);
  });
});
