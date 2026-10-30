/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { DELETE } from "@/app/api/auth/account/route";
import { POST as postSurvey } from "@/app/api/survey/route";
import { POST as postEvent } from "@/app/api/events/route";
import { leave } from "@/lib/analytics/exit";
import { signedIn, useTestAuth } from "@/test/auth";

// #543: a person deletes their own account, with the optional exit reason of
// surveys.md §3 stored only on their analytics yes — and removed with them.

// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
const SALT = "s".repeat(32);
const SESSION = "0123456789abcdef0123456789abcdef";

let dir, file, auditFile, person, seenBeforeDelete;
const headers = (cookie) => ({ origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie });
const call = (handler, url, method, body, cookie = person.cookie) =>
  handler(new Request(`http://monitor${url}`, { method, headers: headers(cookie), body: JSON.stringify(body) }));
const events = () => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);
const answers = () => person.auth.store.all("SELECT survey_id, codes FROM survey_answers");
const audit = () => (fs.existsSync(auditFile) ? fs.readFileSync(auditFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);

/** The screen's leaving steps against the real routes, as `cookie`'s person. */
const leaveWith = (answer, cookie = person.cookie) => {
  const answerStatus = [];
  return leave(answer, {
    sessionId: () => SESSION,
    sendAnswer: async (body) => {
      const res = await call(postSurvey, "/api/survey", "POST", body, cookie);
      answerStatus.push(res.status);
      if (!res.ok) throw new Error(String(res.status));
    },
    deleteAccount: async () => {
      seenBeforeDelete = { events: events(), answers: answers() };
      const res = await call(DELETE, "/api/auth/account", "DELETE", {}, cookie);
      const body = await res.json();
      if (!res.ok) throw new Error(body.error);
      return { status: res.status, body, setCookie: res.headers.get("set-cookie") };
    },
    track: (event, props) => call(postEvent, "/api/events", "POST", { event, session_id: SESSION, props }, cookie),
    wait: () => new Promise(() => {}),
  }).then((r) => ({ ...r, answerStatus }));
};

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "delete-account-"));
  file = path.join(dir, "events.jsonl");
  auditFile = path.join(dir, "audit.jsonl");
  process.env.EVENTS_PATH = file;
  process.env.AUDIT_PATH = auditFile;
  process.env.ANALYTICS_SALT = SALT;
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
  person = await signedIn("leaver", "user");
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ["EVENTS_PATH", "AUDIT_PATH", "ANALYTICS_SALT"]) delete process.env[k];
});

test("Scenario: Leaving is never blocked — Skip or close deletes the account and stores no reason", async () => {
  person.auth.store.setAnalyticsConsent(person.user.id, true);
  const r = await leaveWith({});
  expect(r.status).toBe(200);
  expect(r.answerStatus).toEqual([]); // no answer sent
  expect(person.auth.store.userById(person.user.id)).toBeNull();
  expect(r.setCookie).toMatch(/Max-Age=0|Expires=/);
  expect(answers()).toEqual([]);
  expect(events().filter((e) => e.event === "survey_answered")).toEqual([]);
  expect(audit()).toEqual(expect.arrayContaining([expect.objectContaining({ action: "account_deleted", username: "leaver", role: "user" })]));
});

test("Scenario: A consenting user gives a reason — one survey_answered price, then removed with the account's other events", async () => {
  person.auth.store.setAnalyticsConsent(person.user.id, true);
  const r = await leaveWith({ reason: "price" });
  expect(r.answerStatus).toEqual([204]);
  // Stored before the deletion: the one event of the reason, and its row.
  expect(seenBeforeDelete.events).toEqual([expect.objectContaining({ event: "survey_answered", survey_id: "exit", reason_code: "price" })]);
  expect(seenBeforeDelete.answers).toEqual([{ survey_id: "exit", codes: JSON.stringify({ reason_code: "price" }) }]);
  expect(r.body).toEqual({ events_removed: 1, survey_answers_removed: 1 });
  expect(events()).toEqual([]);
  expect(answers()).toEqual([]);
});

test("Scenario: A user who refused analytics — POST /api/survey answers 403, nothing is stored and the deletion still completes", async () => {
  person.auth.store.setAnalyticsConsent(person.user.id, false);
  const r = await leaveWith({ reason: "other", text: "too many e-mails" });
  expect(r.answerStatus).toEqual([403]);
  expect(r.status).toBe(200);
  expect(person.auth.store.userById(person.user.id)).toBeNull();
  expect(events()).toEqual([]);
  expect(answers()).toEqual([]);
});

test("Scenario: Last admin — the server refuses and says why, and nothing is deleted", async () => {
  // useTestAuth's bootstrap admin is the only admin.
  const admin = await signedIn("admin", "admin");
  expect(admin.auth.store.countAdmins()).toBe(1);
  const res = await call(DELETE, "/api/auth/account", "DELETE", {}, admin.cookie);
  expect(res.status).toBe(409);
  expect((await res.json()).error).toMatch(/only admin.*make another person an admin/);
  expect(admin.auth.store.userById(admin.user.id)).not.toBeNull();
  expect(audit().some((l) => l.action === "account_deleted")).toBe(false);

  // With a second admin, the first may leave.
  admin.auth.store.createUser({ username: "admin2", passwordHash: "not a real hash", role: "admin" });
  expect((await call(DELETE, "/api/auth/account", "DELETE", {}, admin.cookie)).status).toBe(200);
  expect(admin.auth.store.userById(admin.user.id)).toBeNull();
});

test("a manager may delete their own account too", async () => {
  const manager = await signedIn("boss", "manager");
  expect((await call(DELETE, "/api/auth/account", "DELETE", {}, manager.cookie)).status).toBe(200);
  expect(manager.auth.store.userById(manager.user.id)).toBeNull();
});

test.each([
  ["cross-origin", { origin: "http://evil" }, 403],
  ["not JSON", { "content-type": "text/plain" }, 415],
])("%s is refused and nothing is deleted", async (_name, over, status) => {
  const res = await DELETE(new Request("http://monitor/api/auth/account", { method: "DELETE", headers: { ...headers(person.cookie), ...over }, body: "{}" }));
  expect(res.status).toBe(status);
  expect(person.auth.store.userById(person.user.id)).not.toBeNull();
});

test("an ended session cannot delete (401) and a deleted account cannot delete twice", async () => {
  await leaveWith({});
  const again = await call(DELETE, "/api/auth/account", "DELETE", {});
  expect(again.status).toBe(401);
});

test("a deletion whose answer cannot be stored still deletes", async () => {
  person.auth.store.setAnalyticsConsent(person.user.id, true);
  const r = await leave(
    { reason: "price" },
    {
      sessionId: () => SESSION,
      sendAnswer: async () => {
        throw new Error("network down");
      },
      deleteAccount: async () => (await call(DELETE, "/api/auth/account", "DELETE", {})).status,
      track: () => {},
      wait: () => new Promise(() => {}),
    },
  );
  expect(r).toBe(200);
  expect(person.auth.store.userById(person.user.id)).toBeNull();
});
