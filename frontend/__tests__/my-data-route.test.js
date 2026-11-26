/**
 * @jest-environment node
 */
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET } from "@/app/api/auth/my-data/route";
import { pseudonymousId } from "@/lib/analytics/config";
import { getAuth } from "@/lib/auth/server";
import { hashToken } from "@/lib/auth/service";
import { ACCOUNT_COLUMNS, assembleMyData, MY_DATA_SECTIONS, WITHHELD_COLUMNS } from "@/lib/auth/mydata";
import { ACCESS_COOKIE, authKey, signAccess } from "@/lib/auth/tokens";
import { TEST_SECRET, useTestAuth } from "@/test/auth";

// #750: Account ▾ → Download my data, GET /api/auth/my-data.

const SALT = "s".repeat(32);
const SERVICE_TOKEN = "r".repeat(40);
const ADDRESS = "10.1.2.3";

let dir, auditFile, eventsFile, auth;
const auditLines = () => (fs.existsSync(auditFile) ? fs.readFileSync(auditFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);
const exported = () => auditLines().filter((l) => l.action === "my_data_exported");

/** A signed-in person: { user, sid, cookie }. */
async function person(username, role = "user") {
  const user = auth.store.userByName(username) ?? auth.store.createUser({ username, passwordHash: `argon2-hash-of-${username}`, role });
  const sid = hashToken(crypto.randomBytes(32).toString("base64url"));
  auth.store.addSession(sid, user.id, Date.now() + 3600 * 1000, Date.now(), "Chrome on Windows");
  return { user, sid, cookie: `${ACCESS_COOKIE}=${await signAccess(user, authKey(TEST_SECRET), Date.now(), sid)}` };
}

/** One stored analytics event of `user`. */
function event(user, name, props = {}) {
  const line = { event: name, ts: "2026-11-01T10:00:00Z", user_id: pseudonymousId(SALT, user.id), session_id: "0".repeat(32), role: user.role, props };
  fs.appendFileSync(eventsFile, `${JSON.stringify(line)}\n`);
}

/** alice and bob, each with events, a survey answer, a consent and a sign-in on the trail. */
async function aliceAndBob() {
  const alice = await person("alice");
  const bob = await person("bob");
  event(alice.user, "screen_viewed", { screen: "overview" });
  event(alice.user, "tip_opened", { tip_id: "tip.stop_loss" });
  event(bob.user, "screen_viewed", { screen: "income" });
  auth.store.addSurveyAnswer(alice.user.id, { surveyId: "nps", codes: { score: "9" }, text: "clear enough" });
  auth.store.addSurveyAnswer(bob.user.id, { surveyId: "nps", codes: { score: "3" }, text: "bob wrote this" });
  auth.setAnalyticsConsent(alice.user, true);
  auth.setAnalyticsConsent(bob.user, true);
  auth.audit("signed_in", { username: "alice", role: "user", ip: ADDRESS });
  auth.audit("signed_in", { username: "bob", role: "user", ip: ADDRESS });
  // An admin's action on alice: she is only its target (L14).
  auth.audit("role_changed", { username: "admin", role: "admin", target: "alice", from: "manager", to: "user" });
  return { alice, bob };
}

const download = (headers) => GET(new Request("http://monitor/api/auth/my-data", { headers }));

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "my-data-"));
  auditFile = path.join(dir, "audit.jsonl");
  eventsFile = path.join(dir, "events.jsonl");
  process.env.AUDIT_PATH = auditFile;
  process.env.EVENTS_PATH = eventsFile;
  process.env.ANALYTICS_SALT = SALT;
  process.env.MONITOR_API_TOKEN = SERVICE_TOKEN;
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  useTestAuth();
  auth = await getAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ["AUDIT_PATH", "EVENTS_PATH", "ANALYTICS_SALT", "MONITOR_API_TOKEN"]) delete process.env[k];
});

describe("Scenario: A person downloads their data", () => {
  test("alice gets one JSON file with every section, no secret, and one my_data_exported line with the counts", async () => {
    const { alice } = await aliceAndBob();
    const res = await download({ cookie: alice.cookie });
    expect(res.status).toBe(200);
    expect(res.headers.get("content-disposition")).toMatch(/^attachment; filename="my-data-alice-\d{4}-\d{2}-\d{2}\.json"$/);
    expect(res.headers.get("cache-control")).toBe("no-store");
    const text = await res.text();
    const file = JSON.parse(text);

    expect(file).toEqual(expect.objectContaining({ format: "optionsbot-my-data", version: 1, username: "alice" }));
    expect(Object.keys(file.sections)).toEqual(MY_DATA_SECTIONS);
    for (const name of MY_DATA_SECTIONS) expect([name, file.sections[name].status]).toEqual([name, "ok"]);
    const { account, consent, sessions, survey_answers: answers, analytics_events: events, audit_lines: audit } = file.sections;
    expect(Object.keys(account.data)).toEqual(ACCOUNT_COLUMNS);
    expect(account.data).toEqual(expect.objectContaining({ username: "alice", role: "user", analytics_consent: 1, created_at: expect.stringMatching(/Z$/) }));
    expect(consent.data).toEqual({ analytics: "yes" });
    expect(sessions.data).toEqual([expect.objectContaining({ browser: "Chrome on Windows", current: true })]);
    expect(answers.data).toEqual([expect.objectContaining({ survey_id: "nps", codes: { score: "9" }, text: "clear enough" })]);
    expect(events.data.map((e) => e.event)).toEqual(["screen_viewed", "tip_opened"]);
    expect(audit.data.map((l) => l.action)).toEqual(["analytics_consent_changed", "signed_in"]);

    // No password hash, token, token hash or address.
    expect(text).not.toContain("password_hash");
    expect(text).not.toContain("argon2-hash-of-alice");
    expect(text).not.toContain(alice.sid);
    expect(text).not.toContain(alice.cookie.split("=")[1]);
    expect(text).not.toContain(ADDRESS);
    expect(text).not.toMatch(/"(prev|line_hash|ip|token_hash)"/);

    expect(exported()).toEqual([
      expect.objectContaining({
        username: "alice",
        role: "user",
        counts: { account: 1, consent: 1, sessions: 1, survey_answers: 1, analytics_events: 2, audit_lines: 2 },
      }),
    ]);
    expect(JSON.stringify(exported())).not.toContain("clear enough");
  });
});

describe("Scenario: Only their own data", () => {
  test("nothing in alice's file names or belongs to bob, and an admin's action on her stays out", async () => {
    const { alice } = await aliceAndBob();
    const text = await (await download({ cookie: alice.cookie })).text();
    expect(text).not.toMatch(/bob/i);
    expect(text).not.toContain('"screen": "income"');
    const file = JSON.parse(text);
    expect(file.sections.audit_lines.data.every((l) => l.username === "alice")).toBe(true);
    expect(file.sections.audit_lines.data.map((l) => l.action)).not.toContain("role_changed");
  });
});

describe("Scenario: Every role may, nobody else", () => {
  test.each(["admin", "manager", "user"])("%s gets their own data", async (role) => {
    const p = await person(`${role}-person`, role);
    const res = await download({ cookie: p.cookie });
    expect(res.status).toBe(200);
    expect((await res.json()).username).toBe(`${role}-person`);
  });

  test("no session, an ended session and the read-only service token get 401 and nothing is written", async () => {
    const ended = await person("carol");
    auth.store.revokeSessions(ended.user.id);
    const before = auditLines().length;
    for (const headers of [{}, { cookie: ended.cookie }, { authorization: `Bearer ${SERVICE_TOKEN}` }]) {
      expect((await download(headers)).status).toBe(401);
    }
    expect(auditLines().length).toBe(before);
  });
});

describe("Scenario: A section cannot be read", () => {
  test("the analytics store unreadable: the other sections are there, analytics says unavailable with its reason", async () => {
    const { alice } = await aliceAndBob();
    fs.rmSync(eventsFile);
    fs.mkdirSync(eventsFile); // a directory where the file should be: reading it fails
    const file = await (await download({ cookie: alice.cookie })).json();
    expect(file.sections.analytics_events).toEqual({ status: "unavailable", reason: expect.stringContaining("could not be read") });
    for (const name of MY_DATA_SECTIONS.filter((n) => n !== "analytics_events")) expect([name, file.sections[name].status]).toEqual([name, "ok"]);
    expect(exported().at(-1).counts.analytics_events).toBe("unavailable");
  });

  test("analytics off (no salt): unavailable, saying why", async () => {
    const { alice } = await aliceAndBob();
    delete process.env.ANALYTICS_SALT;
    const file = await (await download({ cookie: alice.cookie })).json();
    expect(file.sections.analytics_events).toEqual({ status: "unavailable", reason: expect.stringContaining("ANALYTICS_SALT is not set") });
  });
});

describe("Scenario: Too many downloads", () => {
  test("a 4th download within the hour is 429 in plain words and writes no audit line", async () => {
    const alice = await person("alice");
    for (let i = 0; i < 3; i++) expect((await download({ cookie: alice.cookie })).status).toBe(200);
    const res = await download({ cookie: alice.cookie });
    expect(res.status).toBe(429);
    expect(Number(res.headers.get("retry-after"))).toBeGreaterThan(0);
    expect((await res.json()).error).toMatch(/^You can download your data 3 times an hour\. Try again in \d+ minutes?\.$/);
    expect(exported()).toHaveLength(3);
    // Another person is not held by alice's count.
    expect((await download({ cookie: (await person("dave")).cookie })).status).toBe(200);
  });
});

describe("the assembler", () => {
  test("every users column is either in the file or withheld as a secret", () => {
    const columns = auth.store.all("PRAGMA table_info(users)").map((c) => c.name);
    expect([...ACCOUNT_COLUMNS, ...WITHHELD_COLUMNS].sort()).toEqual([...columns].sort());
  });

  test("a monitor without a trail or events file names both sections unavailable", async () => {
    const alice = await person("alice");
    const { file, counts } = await assembleMyData({
      store: auth.store,
      user: alice.user,
      sessions: () => [],
      eventsFile: null,
      salt: SALT,
      auditFile: null,
      nowMs: Date.parse("2026-11-11T00:00:00Z"),
    });
    expect(file.exported_at).toBe("2026-11-11T00:00:00.000Z");
    expect(file.sections.audit_lines).toEqual({ status: "unavailable", reason: expect.stringContaining("no audit trail") });
    expect(file.sections.analytics_events).toEqual({ status: "unavailable", reason: expect.stringContaining("no analytics events") });
    expect(counts).toEqual({ account: 1, consent: 1, sessions: 0, survey_answers: 0, analytics_events: "unavailable", audit_lines: "unavailable" });
  });
});
