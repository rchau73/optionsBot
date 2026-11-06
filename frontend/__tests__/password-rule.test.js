/**
 * @jest-environment node
 */
// #680: beyond the 12 characters, the server refuses a common password and one
// that contains the username, on every way a person gets a new password.
import { POST as create } from "@/app/api/admin/users/route";
import { POST as adminReset } from "@/app/api/admin/users/[id]/password/route";
import { POST as issueLink } from "@/app/api/admin/users/[id]/reset-link/route";
import { POST as changePassword } from "@/app/api/auth/password/route";
import { POST as resetByLink } from "@/app/api/auth/reset/route";
import { COMMON_PASSWORDS } from "@/lib/auth/commonPasswords";
import { guessablePasswordProblem, hashPassword, MIN_PASSWORD_LENGTH } from "@/lib/auth/password";
import { signedIn, useTestAuth } from "@/test/auth";

jest.setTimeout(60000);

const ORIGIN = "http://localhost:3000";
const OLD = "the password she has now";
const GOOD = "violet tractor sings"; // 20 characters, on no list
const COMMON = "1q2w3e4r5t6y7u"; // 14 characters, on the list as it stands
const WITH_NAME = "maria-secure-2026";
// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
const BREAKS_RULE = "olga-olga-olga-olga";
const SECRETS = [OLD, GOOD, COMMON, WITH_NAME];

function req(path, { body = {}, cookie = "" } = {}) {
  return new Request(`${ORIGIN}${path}`, {
    method: "POST",
    headers: { origin: ORIGIN, host: "localhost:3000", "content-type": "application/json", cookie },
    body: JSON.stringify(body),
  });
}
const ctx = (id) => ({ params: Promise.resolve({ id: String(id) }) });

let logs;
let audit;
let root;
let maria;
beforeEach(async () => {
  useTestAuth();
  logs = [];
  audit = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  root = await signedIn("root", "admin");
  root.auth.auditSink = (e) => audit.push(e);
  maria = root.auth.store.createUser({ username: "maria", passwordHash: await hashPassword(OLD), role: "user" });
});
afterEach(() => jest.restoreAllMocks());

const expectRefused = async (res, reason) => {
  expect(res.status).toBe(400);
  expect((await res.json()).error).toBe(reason);
};

/** Every route that sets a password, each sending `password` for "maria". */
async function signedInMaria() {
  const { access } = await root.auth.login({ username: "maria", password: OLD });
  return `access_token=${access}`;
}
const newLink = () => root.auth.issueResetLink(root.user, maria.id).token;
const WAYS = [
  ["the admin's reset", async (password) => adminReset(req(`/api/admin/users/${maria.id}/password`, { cookie: root.cookie, body: { password } }), ctx(maria.id))],
  ["a reset link", async (password) => resetByLink(req("/api/auth/reset", { body: { token: newLink(), next: password } }))],
  ["change password", async (password) => changePassword(req("/api/auth/password", { cookie: await signedInMaria(), body: { current: OLD, next: password } }))],
];

describe("Scenario: common password", () => {
  test("creating a user with a 14-character password on the list: 400 too common, no user", async () => {
    expect(COMMON).toHaveLength(14);
    const res = await create(req("/api/admin/users", { cookie: root.cookie, body: { username: "nuno", password: COMMON, role: "user" } }));
    await expectRefused(res, "too common");
    expect(root.auth.store.userByName("nuno")).toBeNull();
  });

  test.each(WAYS)("%s: 400 too common, the password unchanged", async (_, send) => {
    const before = root.auth.store.passwordHash(maria.id);
    await expectRefused(await send(COMMON), "too common");
    expect(root.auth.store.passwordHash(maria.id)).toBe(before);
  });

  test.each([
    ["on the list as is", "qwertyuiop1234"],
    ["another case", "QwertyUiop1234"],
    ["look-alikes", "P@ssw0rdP@ssw0rd"],
    ["a 1 for an i", "1loveyou1loveyou"],
    ["a 1 for an l", "we1come12345"],
    ["digits and symbols after a short word", "dragon2026!!!"],
    ["look-alikes and digits after", "p4ssw0rd20262026"],
    ["digits before", "2026password"],
    ["only digits", "123456789012"],
  ])("%s", (_, password) => {
    expect(password.length).toBeGreaterThanOrEqual(MIN_PASSWORD_LENGTH);
    expect(guessablePasswordProblem(password, "nobody")).toBe("too common");
  });

  test("the list is about 1,000 lower-case entries, none too short to mean anything", () => {
    expect(COMMON_PASSWORDS.size).toBeGreaterThanOrEqual(900);
    expect(COMMON_PASSWORDS.size).toBeLessThanOrEqual(1200);
    for (const word of COMMON_PASSWORDS) {
      expect(word).toBe(word.toLowerCase());
      expect(word.length).toBeGreaterThanOrEqual(4);
    }
  });
});

describe("Scenario: password contains the username", () => {
  test("creating a user whose password holds the username in another case: 400 contains your username", async () => {
    const res = await create(req("/api/admin/users", { cookie: root.cookie, body: { username: "Nuno", password: "NUNO-secure-2026", role: "user" } }));
    await expectRefused(res, "contains your username");
    expect(root.auth.store.userByName("nuno")).toBeNull();
  });

  test.each(WAYS)("%s: 400 contains your username, the password unchanged", async (_, send) => {
    const before = root.auth.store.passwordHash(maria.id);
    await expectRefused(await send(WITH_NAME), "contains your username");
    expect(root.auth.store.passwordHash(maria.id)).toBe(before);
  });

  test("any case; a part of the username alone is fine", () => {
    expect(guessablePasswordProblem("my MARIA secure phrase", "maria")).toBe("contains your username");
    expect(guessablePasswordProblem("my mari secure phrase", "maria")).toBeNull();
  });
});

describe("Scenario: reset link survives a refused password", () => {
  test.each([
    ["common", COMMON],
    ["with the username", WITH_NAME],
    ["too short", "short"],
  ])("a %s password leaves the link usable", async (_, refused) => {
    const token = newLink();
    expect((await resetByLink(req("/api/auth/reset", { body: { token, next: refused } }))).status).toBe(400);
    expect(root.auth.store.resetTokens(maria.id)[0].used_at).toBeNull();
    expect((await resetByLink(req("/api/auth/reset", { body: { token, next: GOOD } }))).status).toBe(200);
    expect((await root.auth.login({ username: "maria", password: GOOD })).ok).toBe(true);
  });

  test("a dead link gets its one answer, never the reason of a password that names someone", async () => {
    const res = await resetByLink(req("/api/auth/reset", { body: { token: "x".repeat(43), next: WITH_NAME } }));
    await expectRefused(res, "link expired or already used");
  });
});

describe("Scenario: a good password", () => {
  test("creating a user with a 16-character password works as before", async () => {
    const password = "plum river stone";
    expect(password).toHaveLength(16);
    const res = await create(req("/api/admin/users", { cookie: root.cookie, body: { username: "nuno", password, role: "user" } }));
    expect(res.status).toBe(201);
    expect((await root.auth.login({ username: "nuno", password })).ok).toBe(true);
  });

  test.each(WAYS)("%s accepts it", async (_, send) => {
    expect((await send(GOOD)).status).toBe(200);
    expect((await root.auth.login({ username: "maria", password: GOOD })).ok).toBe(true);
  });

  test("a word from the list inside a longer phrase, or digits alone, stay allowed", () => {
    expect(guessablePasswordProblem("my dragon flies at noon", "maria")).toBeNull();
    expect(guessablePasswordProblem("849302175564", "maria")).toBeNull();
  });
});

describe("Scenario: existing passwords", () => {
  test("a stored password that breaks the rule still signs in: no forced change", async () => {
    const old = root.auth.store.createUser({ username: "olga", passwordHash: await hashPassword(BREAKS_RULE), role: "user" });
    expect(guessablePasswordProblem(BREAKS_RULE, old.username)).toBe("contains your username");
    expect((await root.auth.login({ username: old.username, password: BREAKS_RULE })).ok).toBe(true);
  });

  test("the first admin keeps the length rule alone (#680 names create, change and reset)", () => {
    // useTestAuth's ADMIN_PASSWORD, "admin password for tests", contains the username.
    expect(root.auth.store.userByName("admin")).not.toBeNull();
  });
});

describe("Scenario: no leak", () => {
  test("neither the log nor the audit trail holds a refused or an accepted password", async () => {
    await create(req("/api/admin/users", { cookie: root.cookie, body: { username: "nuno", password: COMMON, role: "user" } }));
    for (const [, send] of WAYS) {
      await send(COMMON);
      await send(WITH_NAME);
      await send(GOOD);
    }
    const text = [logs.join("\n"), JSON.stringify(audit)].join("\n");
    expect(audit.length).toBeGreaterThan(0);
    for (const secret of SECRETS) expect(text).not.toContain(secret);
  });

  test("a refused password adds no audit line of its own", async () => {
    await WAYS[0][1](COMMON);
    expect(audit).toEqual([]);
  });
});
