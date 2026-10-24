/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST } from "@/app/api/events/route";
import { AuthStore } from "@/lib/auth/store";
import { pseudonymousId } from "@/lib/analytics/collector";
import { signedIn, useTestAuth } from "@/test/auth";

// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
const SALT = "s".repeat(32);
const SESSION = "0123456789abcdef0123456789abcdef";

let dir, file, cookie, user, auth;
const post = (body, headers = {}) =>
  POST(
    new Request("http://monitor/api/events", {
      method: "POST",
      headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...headers },
      body: typeof body === "string" ? body : JSON.stringify(body),
    }),
  );
const stored = () => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "events-"));
  file = path.join(dir, "events.jsonl");
  process.env.EVENTS_PATH = file;
  process.env.ANALYTICS_SALT = SALT;
  process.env.MONITOR_DEPLOYMENT = "mvp";
  process.env.MONITOR_VERSION = "v0.3.0";
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
  ({ cookie, user, auth } = await signedIn("viewer", "user"));
  auth.store.setAnalyticsConsent(user.id, true);
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ["EVENTS_PATH", "ANALYTICS_SALT", "MONITOR_DEPLOYMENT", "MONITOR_VERSION"]) delete process.env[k];
});

describe("Scenario: allow-list", () => {
  test.each([
    ["a property not in the plan", { event: "panel_expanded", session_id: SESSION, props: { panel: "held", qty: 3 } }],
    ["a property of another event", { event: "panel_expanded", session_id: SESSION, props: { side: "put" } }],
    ["a numeric value in a free-text property", { event: "tip_opened", session_id: SESSION, props: { tip_id: "tip.premium_1250" } }],
    ["a number as a catalogue id", { event: "video_played", session_id: SESSION, props: { video_id: 1250 } }],
    ["a value outside an enum", { event: "panel_expanded", session_id: SESSION, props: { panel: "BTC-27NOV26-90000-P" } }],
    ["a number where an enum is", { event: "manual_close_opened", session_id: SESSION, props: { side: 90000 } }],
    ["a duration over its cap", { event: "panel_collapsed", session_id: SESSION, props: { panel: "held", open_ms: 3600001 } }],
    ["a fractional duration", { event: "panel_collapsed", session_id: SESSION, props: { panel: "held", open_ms: 1.5 } }],
    ["a string as a flag", { event: "manual_close_opened", session_id: SESSION, props: { allowed: "yes" } }],
    ["an event not in the plan", { event: "position_viewed", session_id: SESSION, props: {} }],
    ["an extra top-level field", { event: "screen_viewed", session_id: SESSION, props: {}, user_id: "me" }],
    ["a malformed session id", { event: "screen_viewed", session_id: "user-admin-42", props: {} }],
    ["props that are not an object", { event: "screen_viewed", session_id: SESSION, props: ["monitor"] }],
  ])("%s is refused (400) and not stored", async (_name, body) => {
    const res = await post(body);
    expect(res.status).toBe(400);
    expect(stored()).toEqual([]);
  });

  test("a body that is not JSON is refused and not stored", async () => {
    expect((await post("{not json")).status).toBe(400);
    expect(stored()).toEqual([]);
  });

  test("the refusal names the property, never its value", async () => {
    const res = await post({ event: "tip_opened", session_id: SESSION, props: { tip_id: "tip.premium_1250" } });
    const { error } = await res.json();
    expect(error).toContain("tip_id");
    expect(error).not.toContain("1250");
  });

  test("an allowed event is stored with its envelope and no identity", async () => {
    const res = await post({ event: "panel_collapsed", session_id: SESSION, props: { panel: "held", open_ms: 4200 } });
    expect(res.status).toBe(204);
    const [line] = stored();
    expect(line).toEqual({
      event: "panel_collapsed",
      ts: expect.stringMatching(/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/),
      user_id: pseudonymousId(SALT, user.id),
      session_id: SESSION,
      role: "user",
      environment: "mvp",
      app_version: "v0.3.0",
      panel: "held",
      open_ms: 4200,
    });
    expect(line.user_id).toMatch(/^[0-9a-f]{16}$/);
    expect(JSON.stringify(line)).not.toContain("viewer");
  });

  test("events append one line each", async () => {
    await post({ event: "screen_viewed", session_id: SESSION, props: { screen: "monitor" } });
    await post({ event: "signed_out", session_id: SESSION, props: { reason: "user" } });
    expect(stored().map((l) => l.event)).toEqual(["screen_viewed", "signed_out"]);
  });

  test("an environment or version the deployment did not set well is left out", async () => {
    process.env.MONITOR_DEPLOYMENT = "laptop";
    process.env.MONITOR_VERSION = "v1 <script>";
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} })).status).toBe(204);
    const [line] = stored();
    expect(line).not.toHaveProperty("environment");
    expect(line).not.toHaveProperty("app_version");
  });
});

// The in-product surveys (docs/product/analytics/surveys.md, #297): codes only.
describe("Scenario: survey answers", () => {
  test("Allowed survey answer: nps, score 9, value_unclear is stored (204)", async () => {
    const res = await post({ event: "survey_answered", session_id: SESSION, props: { survey_id: "nps", score: "9", reason_code: "value_unclear" } });
    expect(res.status).toBe(204);
    const [line] = stored();
    expect(line).toMatchObject({ event: "survey_answered", survey_id: "nps", score: "9", reason_code: "value_unclear" });
  });

  test("shown, dismissed and an exit reason are stored", async () => {
    await post({ event: "survey_shown", session_id: SESSION, props: { survey_id: "exit" } });
    await post({ event: "survey_answered", session_id: SESSION, props: { survey_id: "exit", reason_code: "switched" } });
    await post({ event: "survey_dismissed", session_id: SESSION, props: { survey_id: "answers" } });
    await post({ event: "survey_answered", session_id: SESSION, props: { survey_id: "answers", came_for: "income", hardest_to_find: "worst_case" } });
    expect(stored().map((l) => l.event)).toEqual(["survey_shown", "survey_answered", "survey_dismissed", "survey_answered"]);
  });

  test.each([
    ["a reason_code outside the enum", { survey_id: "exit", reason_code: "too expensive for 0.5 BTC" }],
    ["a free-text property", { survey_id: "exit", reason_code: "other", text: "I lost money" }],
    ["an extra property", { survey_id: "nps", score: "9", comment: "great" }],
    ["a numeric score", { survey_id: "nps", score: 9 }],
    ["a score outside 0–10", { survey_id: "nps", score: "11" }],
    ["a survey not in the plan", { survey_id: "pricing" }],
  ])("Free text refused: %s is refused whole (400) and not stored", async (_name, props) => {
    const res = await post({ event: "survey_answered", session_id: SESSION, props });
    expect(res.status).toBe(400);
    expect(stored()).toEqual([]);
  });
});

describe("consent, session and configuration", () => {
  test.each([
    ["said no", false],
    ["was never asked", null],
  ])("a person who %s is refused and nothing is stored", async (_name, consent) => {
    auth.store.setAnalyticsConsent(user.id, consent);
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} })).status).toBe(403);
    expect(stored()).toEqual([]);
  });

  test("without a signed-in session, cross-origin or not JSON: refused, nothing stored", async () => {
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} }, { cookie: "" })).status).toBe(401);
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} }, { origin: "http://evil" })).status).toBe(403);
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} }, { "content-type": "text/plain" })).status).toBe(415);
    expect(stored()).toEqual([]);
  });

  test("without ANALYTICS_SALT collection is off", async () => {
    delete process.env.ANALYTICS_SALT;
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} })).status).toBe(503);
    process.env.ANALYTICS_SALT = "short";
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} })).status).toBe(503);
    expect(stored()).toEqual([]);
  });

  test("a failed write answers 500 and logs an error", async () => {
    process.env.EVENTS_PATH = dir; // a directory: the append fails
    expect((await post({ event: "screen_viewed", session_id: SESSION, props: {} })).status).toBe(500);
    expect(console.error).toHaveBeenCalledWith(expect.stringContaining("analytics event write failed"));
  });

  test("a body over 2 KB is refused", async () => {
    const res = await post({ event: "screen_viewed", session_id: SESSION, props: {}, pad: "x".repeat(3000) });
    expect(res.status).toBe(413);
    expect(stored()).toEqual([]);
  });
});

describe("consent column", () => {
  test("a user database from before #193 gains the column, everyone not asked", async () => {
    const old = path.join(dir, "auth.db");
    const store = await AuthStore.open(old);
    store.createUser({ username: "early", passwordHash: "x", role: "user" });
    store.db.exec("ALTER TABLE users DROP COLUMN analytics_consent");
    store.save();

    const reopened = await AuthStore.open(old);
    const early = reopened.userByName("early");
    expect(reopened.analyticsConsent(early.id)).toBeNull();
    reopened.setAnalyticsConsent(early.id, true);
    expect((await AuthStore.open(old)).analyticsConsent(early.id)).toBe(true);
  });
});
