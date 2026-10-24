/**
 * @jest-environment node
 */
// #306: admins manage people through /api/admin/users; every route re-reads
// the person (requireRole admin) and calls the AuthService rules.
import { GET as list, POST as create } from "@/app/api/admin/users/route";
import { DELETE as remove, PATCH as patch } from "@/app/api/admin/users/[id]/route";
import { POST as reset } from "@/app/api/admin/users/[id]/password/route";
import { POST as signout } from "@/app/api/admin/users/[id]/signout/route";
import { POST as changeOwnPassword } from "@/app/api/auth/password/route";
import { POST as refresh } from "@/app/api/auth/refresh/route";
import { AuthStore } from "@/lib/auth/store";
import { ACCESS_COOKIE, REFRESH_COOKIE } from "@/lib/auth/tokens";
import { signedIn, useTestAuth } from "@/test/auth";

jest.setTimeout(30000);

const ORIGIN = "http://localhost:3000";
const BOB_PASSWORD = "bob's first password";
const NEW_PASSWORD = "bob's reset password";

function req(path, { method = "POST", body = {}, cookie = "", headers = {} } = {}) {
  return new Request(`${ORIGIN}${path}`, {
    method,
    headers: { origin: ORIGIN, host: "localhost:3000", "content-type": "application/json", cookie, ...headers },
    body: method === "GET" ? undefined : JSON.stringify(body),
  });
}
const ctx = (id) => ({ params: Promise.resolve({ id: String(id) }) });

let logs;
beforeEach(() => {
  useTestAuth();
  logs = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
});
afterEach(() => jest.restoreAllMocks());

/** The admin's session and an audit sink collecting every entry. */
async function asAdmin() {
  const s = await signedIn("admin", "admin");
  const audit = [];
  s.auth.auditSink = (e) => audit.push(e);
  return { ...s, audit };
}

async function createBob(cookie, role = "user") {
  return create(req("/api/admin/users", { cookie, body: { username: "Bob", password: BOB_PASSWORD, role, email: "bob@example.com" } }));
}

describe("Scenario: a manager cannot administer users", () => {
  test("a signed-in manager's POST /api/admin/users answers 403 and no user is created", async () => {
    const { cookie, auth } = await signedIn("mia", "manager");
    const before = auth.store.countUsers();
    const res = await createBob(cookie);
    expect(res.status).toBe(403);
    expect((await res.json()).error).toMatch(/admin role required/);
    expect(auth.store.countUsers()).toBe(before);
    expect(auth.store.userByName("bob")).toBeNull();
  });

  test("a manager or a user gets 403 on every admin route; an ended session 401", async () => {
    const admin = await asAdmin();
    for (const role of ["manager", "user"]) {
      const { cookie } = await signedIn(`p-${role}`, role);
      const id = admin.user.id;
      expect((await list(req("/api/admin/users", { method: "GET", cookie }))).status).toBe(403);
      expect((await patch(req(`/api/admin/users/${id}`, { method: "PATCH", cookie, body: { role: "user" } }), ctx(id))).status).toBe(403);
      expect((await reset(req(`/api/admin/users/${id}/password`, { cookie, body: { password: NEW_PASSWORD } }), ctx(id))).status).toBe(403);
      expect((await signout(req(`/api/admin/users/${id}/signout`, { cookie }), ctx(id))).status).toBe(403);
      expect((await remove(req(`/api/admin/users/${id}`, { method: "DELETE", cookie }), ctx(id))).status).toBe(403);
    }
    expect(admin.auth.store.userById(admin.user.id).role).toBe("admin");
    expect(admin.audit).toEqual([]);

    admin.auth.signOutEverywhere(admin.user, admin.user.id);
    const res = await list(req("/api/admin/users", { method: "GET", cookie: admin.cookie }));
    expect(res.status).toBe(401);
    expect(res.headers.get("x-monitor-session")).toBe("required");
  });
});

describe("Scenario: the last admin is protected", () => {
  test("the one admin changing their own role to user gets 409 with the reason; the role is unchanged", async () => {
    const { cookie, user, auth, audit } = await asAdmin();
    expect(auth.store.countAdmins()).toBe(1);
    const res = await patch(req(`/api/admin/users/${user.id}`, { method: "PATCH", cookie, body: { role: "user" } }), ctx(user.id));
    expect(res.status).toBe(409);
    expect((await res.json()).error).toMatch(/always be an admin/);
    expect(auth.store.userById(user.id).role).toBe("admin");
    expect(audit).toEqual([]);
  });

  test("nobody deletes themselves (409)", async () => {
    const { cookie, user, auth } = await asAdmin();
    const res = await remove(req(`/api/admin/users/${user.id}`, { method: "DELETE", cookie }), ctx(user.id));
    expect(res.status).toBe(409);
    expect((await res.json()).error).toMatch(/cannot delete yourself/);
    expect(auth.store.userById(user.id)).not.toBeNull();
  });
});

describe("Scenario: a reset ends the sessions", () => {
  test("bob on two browsers: after an admin's reset both answer 401, and one password_reset audit line names the admin and bob", async () => {
    const { cookie, auth, audit } = await asAdmin();
    expect((await createBob(cookie)).status).toBe(201);
    const laptop = await auth.login({ username: "bob", password: BOB_PASSWORD });
    const phone = await auth.login({ username: "bob", password: BOB_PASSWORD });
    const browsers = [laptop, phone].map((s) => ({ access: `${ACCESS_COOKIE}=${s.access}`, refresh: `${REFRESH_COOKIE}=${s.refresh}` }));

    const res = await reset(req(`/api/admin/users/${laptop.user.id}/password`, { cookie, body: { password: NEW_PASSWORD } }), ctx(laptop.user.id));
    expect(res.status).toBe(200);

    for (const b of browsers) {
      // A request that re-reads the session, and the silent refresh, both answer 401.
      const own = await changeOwnPassword(req("/api/auth/password", { cookie: b.access, body: { current: BOB_PASSWORD, next: "a third password ok" } }));
      expect(own.status).toBe(401);
      expect(own.headers.get("x-monitor-session")).toBe("required");
      expect((await refresh(req("/api/auth/refresh", { cookie: b.refresh }))).status).toBe(401);
    }
    expect(auth.store.sessionCount(laptop.user.id)).toBe(0);

    const resets = audit.filter((e) => e.action === "password_reset");
    expect(resets).toHaveLength(1);
    expect(resets[0]).toMatchObject({ username: "admin", role: "admin", target: "bob" });
    const text = JSON.stringify(audit) + logs.join("\n");
    for (const secret of [NEW_PASSWORD, BOB_PASSWORD, "$argon2id$"]) expect(text).not.toContain(secret);

    // The new password signs in, the old one no longer does.
    expect((await auth.login({ username: "bob", password: BOB_PASSWORD })).ok).toBe(false);
    expect((await auth.login({ username: "bob", password: NEW_PASSWORD })).ok).toBe(true);
  });

  test("a short password is refused (400), nothing changes; your own password is refused (409); an unknown id 404", async () => {
    const { cookie, user, auth, audit } = await asAdmin();
    await createBob(cookie);
    const bob = auth.store.userByName("bob");
    const hash = auth.store.passwordHash(bob.id);
    const short = await reset(req(`/api/admin/users/${bob.id}/password`, { cookie, body: { password: "too short" } }), ctx(bob.id));
    expect(short.status).toBe(400);
    expect((await short.json()).error).toMatch(/at least 12 characters/);
    expect(auth.store.passwordHash(bob.id)).toBe(hash);

    const own = await reset(req(`/api/admin/users/${user.id}/password`, { cookie, body: { password: NEW_PASSWORD } }), ctx(user.id));
    expect(own.status).toBe(409);
    expect((await own.json()).error).toMatch(/Change password/);

    for (const id of ["999", "abc", "0", "1.5"]) {
      expect((await reset(req(`/api/admin/users/${id}/password`, { cookie, body: { password: NEW_PASSWORD } }), ctx(id))).status).toBe(404);
    }
    expect(audit.filter((e) => e.action === "password_reset")).toEqual([]);
  });
});

describe("list and create", () => {
  test("GET lists every user with role, email, created and last sign-in — never a hash", async () => {
    const { cookie, auth } = await asAdmin();
    await createBob(cookie, "manager");
    let users = (await (await list(req("/api/admin/users", { method: "GET", cookie }))).json()).users;
    const bob = users.find((u) => u.username === "bob");
    expect(bob).toEqual({ id: bob.id, username: "bob", role: "manager", email: "bob@example.com", created_at: expect.any(String), last_sign_in_at: null });
    expect(Date.parse(bob.created_at)).not.toBeNaN();
    expect(JSON.stringify(users)).not.toMatch(/argon2|password/);

    await auth.login({ username: "bob", password: BOB_PASSWORD });
    users = (await (await list(req("/api/admin/users", { method: "GET", cookie }))).json()).users;
    expect(Date.parse(users.find((u) => u.username === "bob").last_sign_in_at)).not.toBeNaN();
  });

  test("POST creates with the username and password rules; audited as user_created without the password", async () => {
    const { cookie, auth, audit } = await asAdmin();
    const res = await createBob(cookie, "manager");
    expect(res.status).toBe(201);
    expect((await res.json()).user).toMatchObject({ username: "bob", role: "manager", email: "bob@example.com" });
    expect(audit).toEqual([expect.objectContaining({ action: "user_created", username: "admin", role: "admin", target: "bob", to: "manager" })]);
    expect(JSON.stringify(audit)).not.toContain(BOB_PASSWORD);
    expect((await auth.login({ username: "bob", password: BOB_PASSWORD })).ok).toBe(true);

    const bad = async (body) => create(req("/api/admin/users", { cookie, body }));
    const good = { username: "carol", password: "carol's password", role: "user" };
    expect((await bad({ ...good, username: "x" })).status).toBe(400);
    expect((await bad({ ...good, username: "Has Space" })).status).toBe(400);
    expect((await bad({ ...good, password: "short" })).status).toBe(400);
    expect((await bad({ ...good, role: "root" })).status).toBe(400);
    expect((await bad({ ...good, email: "not an email" })).status).toBe(400);
    expect((await bad({ ...good, email: 42 })).status).toBe(400);
    const dup = await bad({ ...good, username: "BOB" });
    expect(dup.status).toBe(409);
    expect((await dup.json()).error).toMatch(/taken/);
    expect(auth.store.userByName("carol")).toBeNull();
    expect((await bad(good)).status).toBe(201);
  });

  test("state-changing requests must be same-origin JSON", async () => {
    const { cookie, auth } = await asAdmin();
    const body = { username: "bob", password: BOB_PASSWORD, role: "user" };
    expect((await create(req("/api/admin/users", { cookie, body, headers: { origin: "http://evil.example" } }))).status).toBe(403);
    expect((await create(req("/api/admin/users", { cookie, body, headers: { "content-type": "text/plain" } }))).status).toBe(415);
    expect(auth.store.userByName("bob")).toBeNull();
  });
});

describe("role change, sign out everywhere and delete", () => {
  test("an admin changes bob's role, signs him out and deletes him; each is audited", async () => {
    const { cookie, auth, audit } = await asAdmin();
    await createBob(cookie);
    const bob = auth.store.userByName("bob");
    await auth.login({ username: "bob", password: BOB_PASSWORD });

    const role = await patch(req(`/api/admin/users/${bob.id}`, { method: "PATCH", cookie, body: { role: "manager" } }), ctx(bob.id));
    expect(role.status).toBe(200);
    expect((await role.json()).user).toMatchObject({ username: "bob", role: "manager" });
    expect(auth.store.sessionCount(bob.id)).toBe(0);
    expect((await patch(req(`/api/admin/users/${bob.id}`, { method: "PATCH", cookie, body: { role: "root" } }), ctx(bob.id))).status).toBe(400);

    await auth.login({ username: "bob", password: BOB_PASSWORD });
    expect((await signout(req(`/api/admin/users/${bob.id}/signout`, { cookie }), ctx(bob.id))).status).toBe(200);
    expect(auth.store.sessionCount(bob.id)).toBe(0);

    const del = await remove(req(`/api/admin/users/${bob.id}`, { method: "DELETE", cookie }), ctx(bob.id));
    expect(del.status).toBe(200);
    expect(await del.json()).toEqual({ events_removed: 0 });
    expect(auth.store.userById(bob.id)).toBeNull();
    expect((await remove(req(`/api/admin/users/${bob.id}`, { method: "DELETE", cookie }), ctx(bob.id))).status).toBe(404);

    const changes = audit.map((e) => e.action).filter((a) => a !== "signed_in");
    expect(changes).toEqual(["user_created", "role_changed", "signed_out_everywhere", "user_deleted", "events_deleted"]);
    expect(audit.filter((e) => e.action !== "signed_in").every((e) => e.username === "admin")).toBe(true);
  });
});

describe("last sign-in column", () => {
  test("a user database from before #306 gains last_sign_in_at, everyone never signed in", async () => {
    const store = await AuthStore.open(":memory:");
    store.createUser({ username: "early", passwordHash: "x", role: "user" });
    store.db.exec("ALTER TABLE users DROP COLUMN last_sign_in_at");
    expect(store.migrate()).toBe(true);
    expect(store.listUsers()[0]).toMatchObject({ username: "early", last_sign_in_at: null });
    expect(store.migrate()).toBe(false);
  });
});
