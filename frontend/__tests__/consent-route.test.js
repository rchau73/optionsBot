/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET, POST } from "@/app/api/consent/route";
import { POST as postEvent } from "@/app/api/events/route";
import { signedIn, useTestAuth } from "@/test/auth";

// #363: the person's answer to usage analytics, set through the route.

// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
const SALT = "s".repeat(32);
const SESSION = "0123456789abcdef0123456789abcdef";

let dir, file, auditFile, cookie, user, auth;
const headers = (over = {}) => ({ origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...over });
const getConsent = (over = {}) => GET(new Request("http://monitor/api/consent", { headers: headers(over) }));
const setConsent = (body, over = {}) =>
  POST(new Request("http://monitor/api/consent", { method: "POST", headers: headers(over), body: typeof body === "string" ? body : JSON.stringify(body) }));
const sendEvent = () =>
  postEvent(new Request("http://monitor/api/events", { method: "POST", headers: headers(), body: JSON.stringify({ event: "screen_viewed", session_id: SESSION, props: {} }) }));
const stored = () => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n").filter(Boolean) : []);
// The consent lines only (the first admin's creation is audited too).
const audits = () =>
  (fs.existsSync(auditFile) ? fs.readFileSync(auditFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []).filter(
    (e) => e.action === "analytics_consent_changed",
  );

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "consent-"));
  file = path.join(dir, "events.jsonl");
  auditFile = path.join(dir, "audit.jsonl");
  process.env.EVENTS_PATH = file;
  process.env.AUDIT_PATH = auditFile;
  process.env.ANALYTICS_SALT = SALT;
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
  ({ cookie, user, auth } = await signedIn("viewer", "user"));
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ["EVENTS_PATH", "AUDIT_PATH", "ANALYTICS_SALT"]) delete process.env[k];
});

describe("Scenario: consent starts unanswered", () => {
  test("GET answers null and POST /api/events answers 403", async () => {
    const res = await getConsent();
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(await res.json()).toEqual({ consent: null });
    expect((await sendEvent()).status).toBe(403);
    expect(stored()).toEqual([]);
  });
});

describe("Scenario: saying yes opens collection", () => {
  test("POST {consent: true} from the same origin: GET says true, an audit line exists, a plan event is accepted", async () => {
    const res = await setConsent({ consent: true });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ consent: true, note: expect.stringContaining("on") });
    expect(await (await getConsent()).json()).toEqual({ consent: true });
    expect(audits()).toEqual([{ time: expect.any(String), action: "analytics_consent_changed", username: "viewer", consent: true }]);
    expect((await sendEvent()).status).toBe(204);
    expect(stored()).toHaveLength(1);
  });
});

describe("Scenario: saying no stops collection", () => {
  test("a person who consented says no: events answer 403 and the reply says earlier events stay until deletion", async () => {
    auth.store.setAnalyticsConsent(user.id, true);
    expect((await sendEvent()).status).toBe(204);

    const res = await setConsent({ consent: false });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.consent).toBe(false);
    expect(body.note).toMatch(/already stored stay until you ask for their deletion/);
    expect((await sendEvent()).status).toBe(403);
    expect(stored()).toHaveLength(1); // the earlier event stays
    expect(audits().at(-1)).toMatchObject({ action: "analytics_consent_changed", username: "viewer", consent: false });
  });
});

describe("Scenario: no session or another origin", () => {
  test.each([
    ["no session", { cookie: "" }, 401],
    ["another origin", { origin: "http://evil" }, 403],
    ["no origin", { origin: "" }, 403],
    ["not JSON", { "content-type": "text/plain" }, 415],
  ])("%s: refused and nothing changes", async (_name, over, status) => {
    expect((await setConsent({ consent: true }, over)).status).toBe(status);
    expect(auth.store.analyticsConsent(user.id)).toBeNull();
    expect(audits()).toEqual([]);
  });

  test("GET without a session is refused", async () => {
    expect((await getConsent({ cookie: "" })).status).toBe(401);
  });

  test.each([
    ["a string", { consent: "yes" }],
    ["null", { consent: null }],
    ["an extra field", { consent: true, user_id: 1 }],
    ["no field", {}],
    ["an array", [true]],
    ["not JSON", "consent=true"],
  ])("a body with %s is refused (400) and nothing changes", async (_name, body) => {
    expect((await setConsent(body)).status).toBe(400);
    expect(auth.store.analyticsConsent(user.id)).toBeNull();
    expect(audits()).toEqual([]);
  });
});
