/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST } from "@/app/api/survey/route";
import { PROPERTIES } from "@/lib/analytics/plan";
import { MAX_SURVEY_TEXT, SURVEYS, validateSurveyAnswer } from "@/lib/analytics/surveys";
import { pseudonymousId } from "@/lib/analytics/collector";
import { signedIn, useTestAuth } from "@/test/auth";

// #363: an in-product survey answer — codes and free text beside the user in
// auth.db, the codes alone in the event log.

// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
const SALT = "s".repeat(32);
const SESSION = "0123456789abcdef0123456789abcdef";
const TEXT = "I would like a weekly e-mail";

let dir, file, cookie, user, auth;
const post = (body, over = {}) =>
  POST(
    new Request("http://monitor/api/survey", {
      method: "POST",
      headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...over },
      body: typeof body === "string" ? body : JSON.stringify(body),
    }),
  );
const events = () => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);
const rows = () => auth.store.all("SELECT user_id, survey_id, codes, free_text FROM survey_answers");

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "survey-"));
  file = path.join(dir, "events.jsonl");
  process.env.EVENTS_PATH = file;
  process.env.ANALYTICS_SALT = SALT;
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
  ({ cookie, user, auth } = await signedIn("viewer", "user"));
  auth.store.setAnalyticsConsent(user.id, true);
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ["EVENTS_PATH", "ANALYTICS_SALT"]) delete process.env[k];
});

describe("Scenario: a survey answer is stored whole or refused whole", () => {
  test("a valid NPS answer: codes and free text in survey_answers, only the codes in the event log", async () => {
    const res = await post({ survey_id: "nps", session_id: SESSION, codes: { score: "9", reason_code: "saves_time" }, text: TEXT });
    expect(res.status).toBe(204);
    expect(rows()).toEqual([{ user_id: user.id, survey_id: "nps", codes: JSON.stringify({ score: "9", reason_code: "saves_time" }), free_text: TEXT }]);
    const stored = events();
    expect(stored).toEqual([
      {
        event: "survey_answered",
        ts: expect.any(String),
        user_id: pseudonymousId(SALT, user.id),
        session_id: SESSION,
        role: "user",
        survey_id: "nps",
        score: "9",
        reason_code: "saves_time",
      },
    ]);
    expect(fs.readFileSync(file, "utf8")).not.toContain("weekly");
    // Nor in a log line.
    const logged = [...console.log.mock.calls, ...console.error.mock.calls].map((c) => c.join(" ")).join("\n");
    expect(logged).not.toContain("weekly");
  });

  test.each([
    ["the four answers, one question", { survey_id: "answers", session_id: SESSION, codes: { hardest_to_find: "worst_case" } }],
    ["the four answers, both", { survey_id: "answers", session_id: SESSION, codes: { came_for: "income", hardest_to_find: "none" } }],
    ["an exit reason", { survey_id: "exit", session_id: SESSION, codes: { reason_code: "switched" } }],
    ["an exit 'other' with its text", { survey_id: "exit", session_id: SESSION, codes: { reason_code: "other" }, text: TEXT }],
    ["an NPS score alone", { survey_id: "nps", session_id: SESSION, codes: { score: "0" } }],
    ["a text of exactly the cap", { survey_id: "nps", session_id: SESSION, codes: { score: "5" }, text: "é".repeat(MAX_SURVEY_TEXT) }],
  ])("%s is stored", async (_name, body) => {
    expect((await post(body)).status).toBe(204);
    expect(rows()).toHaveLength(1);
    expect(events()).toEqual([expect.objectContaining({ event: "survey_answered", survey_id: body.survey_id, ...body.codes })]);
    expect(Object.keys(events()[0])).not.toContain("text");
  });

  test.each([
    ["an unknown survey id", { survey_id: "churn", session_id: SESSION, codes: { reason_code: "price" } }],
    ["an unknown code value", { survey_id: "nps", session_id: SESSION, codes: { score: "11" } }],
    ["a numeric score", { survey_id: "nps", session_id: SESSION, codes: { score: 9 } }],
    ["a code another survey asks", { survey_id: "nps", session_id: SESSION, codes: { score: "9", came_for: "income" } }],
    ["a promoter code on an exit", { survey_id: "exit", session_id: SESSION, codes: { reason_code: "saves_time" } }],
    ["an exit-only code on NPS", { survey_id: "nps", session_id: SESSION, codes: { score: "2", reason_code: "switched" } }],
    ["an NPS answer without a score", { survey_id: "nps", session_id: SESSION, codes: { reason_code: "price" } }],
    ["no answer at all", { survey_id: "answers", session_id: SESSION, codes: {} }],
    ["an over-long free text", { survey_id: "nps", session_id: SESSION, codes: { score: "9" }, text: "x".repeat(MAX_SURVEY_TEXT + 1) }],
    ["text on the four answers", { survey_id: "answers", session_id: SESSION, codes: { came_for: "income" }, text: TEXT }],
    ["exit text without 'other'", { survey_id: "exit", session_id: SESSION, codes: { reason_code: "price" }, text: TEXT }],
    ["text that is not a string", { survey_id: "nps", session_id: SESSION, codes: { score: "9" }, text: 42 }],
    ["an extra top-level field", { survey_id: "nps", session_id: SESSION, codes: { score: "9" }, amount: 1250 }],
    ["a malformed session id", { survey_id: "nps", session_id: "me", codes: { score: "9" } }],
    ["codes that are not an object", { survey_id: "nps", session_id: SESSION, codes: ["9"] }],
    ["not JSON", "score=9"],
  ])("%s is refused with 400 and nothing is stored", async (_name, body) => {
    const res = await post(body);
    expect(res.status).toBe(400);
    expect(rows()).toEqual([]);
    expect(events()).toEqual([]);
  });

  test("the refusal names the field, never the text", async () => {
    const secret = "my password is hunter2";
    const res = await post({ survey_id: "answers", session_id: SESSION, codes: { came_for: "income" }, text: secret });
    expect((await res.json()).error).toBe("text: this survey takes no text");
    const logged = [...console.log.mock.calls, ...console.error.mock.calls].map((c) => c.join(" ")).join("\n");
    expect(logged).not.toContain("hunter2");
  });

  test("an event that cannot be written leaves no answer behind (500)", async () => {
    process.env.EVENTS_PATH = dir; // a directory: the append fails
    const res = await post({ survey_id: "nps", session_id: SESSION, codes: { score: "9" }, text: TEXT });
    expect(res.status).toBe(500);
    expect(rows()).toEqual([]);
  });

  test("a body over 4 KB is refused", async () => {
    expect((await post({ survey_id: "nps", session_id: SESSION, codes: { score: "9" }, text: "x".repeat(5000) })).status).toBe(413);
    expect(rows()).toEqual([]);
  });
});

describe("consent, session and configuration", () => {
  test.each([
    ["said no", false],
    ["was never asked", null],
  ])("a person who %s is refused (403) for every survey and nothing is stored", async (_name, consent) => {
    auth.store.setAnalyticsConsent(user.id, consent);
    for (const body of [
      { survey_id: "nps", session_id: SESSION, codes: { score: "9" } },
      { survey_id: "answers", session_id: SESSION, codes: { came_for: "income" } },
      { survey_id: "exit", session_id: SESSION, codes: { reason_code: "price" } },
    ]) {
      expect((await post(body)).status).toBe(403);
    }
    expect(rows()).toEqual([]);
    expect(events()).toEqual([]);
  });

  test("without a session, cross-origin or not JSON: refused, nothing stored", async () => {
    const body = { survey_id: "nps", session_id: SESSION, codes: { score: "9" } };
    expect((await post(body, { cookie: "" })).status).toBe(401);
    expect((await post(body, { origin: "http://evil" })).status).toBe(403);
    expect((await post(body, { "content-type": "text/plain" })).status).toBe(415);
    expect(rows()).toEqual([]);
    expect(events()).toEqual([]);
  });

  test("without ANALYTICS_SALT nothing is stored (503)", async () => {
    delete process.env.ANALYTICS_SALT;
    expect((await post({ survey_id: "nps", session_id: SESSION, codes: { score: "9" } })).status).toBe(503);
    expect(rows()).toEqual([]);
  });
});

describe("the survey rules stay inside the event plan's allow-list", () => {
  test("every survey id and every code is a value the collector accepts", () => {
    expect(Object.keys(SURVEYS)).toEqual(PROPERTIES.survey_id.values);
    for (const [id, survey] of Object.entries(SURVEYS)) {
      for (const [name, values] of Object.entries(survey.codes)) {
        expect(PROPERTIES[name]?.type).toBe("enum");
        for (const v of values) expect([id, name, PROPERTIES[name].values.includes(v)]).toEqual([id, name, true]);
      }
    }
  });

  test("validateSurveyAnswer keeps an empty text as none", () => {
    expect(validateSurveyAnswer({ survey_id: "nps", session_id: SESSION, codes: { score: "7" }, text: "   " })).toEqual({
      ok: true,
      surveyId: "nps",
      sessionId: SESSION,
      codes: { score: "7" },
      text: null,
    });
  });
});
