/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET, POST } from "@/app/api/survey/answers/route";
import { POST as postSurvey } from "@/app/api/survey/route";
import { ANSWERS_AFTER_NPS_DAYS, ANSWERS_CODES, ANSWERS_EVERY_DAYS, answersNotDueReason } from "@/lib/analytics/answers";
import { NPS_MIN_ACCOUNT_DAYS, NPS_MIN_SESSIONS } from "@/lib/analytics/nps";
import { PROPERTIES } from "@/lib/analytics/plan";
import { SURVEYS } from "@/lib/analytics/surveys";
import { signedIn, useTestAuth } from "@/test/auth";

// #498: when the four answers card is due — decided on the server from the
// user's row in auth.db, beside the NPS columns — and its answer.

const DAY = 24 * 3600;
const NOW = Date.UTC(2026, 9, 29, 12);
const nowSec = Math.floor(NOW / 1000);
// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
const SALT = "s".repeat(32);
const SESSION = "0123456789abcdef0123456789abcdef";

let dir, file, cookie, user, auth;
const req = (url, method, body, over = {}) =>
  new Request(`http://monitor${url}`, {
    method,
    headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...over },
    body: body === undefined ? undefined : typeof body === "string" ? body : JSON.stringify(body),
  });
const get = async (over) => (await GET(req("/api/survey/answers", "GET", undefined, over))).json();
const post = (body, over) => POST(req("/api/survey/answers", "POST", body, over));
const shownSec = () => auth.store.answersState(user.id).lastShownSec;
const events = () => (fs.existsSync(file) ? fs.readFileSync(file, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l)) : []);
/** The user's row as the scenarios set it. */
const setRow = ({ ageDays = 20, sessions = 5, shownDaysAgo = null, npsDaysAgo = null, consent = true } = {}) => {
  auth.store.setAnalyticsConsent(user.id, consent);
  const ago = (d) => (d == null ? null : nowSec - d * DAY);
  auth.store.db.run("UPDATE users SET created_at = ?, sign_in_count = ?, answers_last_shown_at = ?, nps_last_shown_at = ? WHERE id = ?", [
    nowSec - ageDays * DAY,
    sessions,
    ago(shownDaysAgo),
    ago(npsDaysAgo),
    user.id,
  ]);
};

beforeEach(async () => {
  jest.useFakeTimers({ now: NOW, doNotFake: ["nextTick", "setImmediate", "queueMicrotask"] });
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "answers-"));
  file = path.join(dir, "events.jsonl");
  process.env.EVENTS_PATH = file;
  process.env.ANALYTICS_SALT = SALT;
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  useTestAuth();
  ({ cookie, user, auth } = await signedIn("viewer", "user"));
});
afterEach(() => {
  jest.useRealTimers();
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
  for (const k of ["EVENTS_PATH", "ANALYTICS_SALT"]) delete process.env[k];
});

describe("Scenario: Card is due", () => {
  test("consent yes, 14+ days, 3+ sign-ins, no answers card in 30 days, no NPS in 7 → due", async () => {
    setRow({ ageDays: NPS_MIN_ACCOUNT_DAYS, sessions: NPS_MIN_SESSIONS, shownDaysAgo: ANSWERS_EVERY_DAYS, npsDaysAgo: ANSWERS_AFTER_NPS_DAYS });
    expect(await get()).toEqual({ due: true, reason: null });
  });

  test("shown records the day once, and audits it with no answer", async () => {
    setRow();
    const res = await post({ action: "shown" });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ due: false, reason: "shown_recently" });
    expect(shownSec()).toBe(nowSec);
    // The NPS columns are untouched.
    expect(auth.store.npsState(user.id)).toMatchObject({ lastShownSec: null, optedOut: false });
  });
});

describe("Scenario: Dismiss", () => {
  test.each([
    ["1 day", 1, false],
    ["29 days", 29, false],
    ["30 days", ANSWERS_EVERY_DAYS, true],
  ])("shown (answered or dismissed) %s ago → due is %s", async (_n, days, due) => {
    setRow({ shownDaysAgo: days });
    expect((await get()).due).toBe(due);
  });
});

describe("Scenario: No consent", () => {
  test.each([
    ["said no", false],
    ["never answered", null],
  ])("%s → never due, shown stores nothing, and an answer is refused", async (_n, consent) => {
    setRow({ consent });
    expect(await get()).toEqual({ due: false, reason: "no_consent" });
    expect((await post({ action: "shown" })).status).toBe(409);
    expect(shownSec()).toBeNull();
    const res = await postSurvey(req("/api/survey", "POST", { survey_id: "answers", session_id: SESSION, codes: { came_for: "holding" } }));
    expect(res.status).toBe(403);
    expect(auth.store.surveyAnswers(user.id)).toEqual([]);
    expect(events()).toEqual([]);
  });
});

describe("Scenario: Second tab or device", () => {
  test("the first tab's shown wins; the second changes nothing and the card is not due there", async () => {
    setRow();
    expect((await post({ action: "shown" })).status).toBe(200);
    jest.setSystemTime(NOW + 60_000);
    const other = await signedIn("viewer", "user");
    const res = await POST(req("/api/survey/answers", "POST", { action: "shown" }, { cookie: other.cookie }));
    expect(res.status).toBe(409);
    expect(shownSec()).toBe(nowSec);
    expect((await get({ cookie: other.cookie })).due).toBe(false);
  });
});

describe("Scenario: Answer one question", () => {
  test("only hardest_to_find: stored with no came_for, one event with the codes only", async () => {
    setRow();
    const res = await postSurvey(req("/api/survey", "POST", { survey_id: "answers", session_id: SESSION, codes: { hardest_to_find: "worst_case" } }));
    expect(res.status).toBe(204);
    expect(auth.store.surveyAnswers(user.id)).toEqual([
      { survey_id: "answers", codes: { hardest_to_find: "worst_case" }, text: null, created_at: expect.any(String) },
    ]);
    const stored = events();
    expect(stored).toHaveLength(1);
    expect(stored[0]).toMatchObject({ event: "survey_answered", survey_id: "answers", hardest_to_find: "worst_case" });
    expect(stored[0]).not.toHaveProperty("came_for");
    expect(Object.keys(stored[0]).sort()).toEqual(["event", "hardest_to_find", "role", "session_id", "survey_id", "ts", "user_id"]);
  });

  test("text with an answers survey is refused whole", async () => {
    setRow();
    const res = await postSurvey(req("/api/survey", "POST", { survey_id: "answers", session_id: SESSION, codes: { came_for: "income" }, text: "x" }));
    expect(res.status).toBe(400);
    expect(auth.store.surveyAnswers(user.id)).toEqual([]);
  });
});

describe("Scenario: Deleting a user", () => {
  test("AuthService.deleteUser removes their answers rows and their row in the same step", async () => {
    setRow();
    await post({ action: "shown" });
    auth.store.addSurveyAnswer(user.id, { surveyId: "answers", codes: { came_for: "holding" } }, NOW);
    const { user: admin } = await signedIn("admin", "admin");
    expect(auth.deleteUser(admin, user.id)).toMatchObject({ ok: true, survey_answers_removed: 1 });
    expect(auth.store.surveyAnswers(user.id)).toEqual([]);
    expect(auth.store.answersState(user.id)).toBeNull();
  });
});

describe("the other rules", () => {
  test.each([
    ["an account under 14 days old", { ageDays: NPS_MIN_ACCOUNT_DAYS - 1 }, "account_too_new"],
    ["fewer than 3 sign-ins", { sessions: NPS_MIN_SESSIONS - 1 }, "too_few_sessions"],
    ["NPS shown today (the same session)", { npsDaysAgo: 0 }, "nps_recently"],
    ["NPS shown 6 days ago", { npsDaysAgo: ANSWERS_AFTER_NPS_DAYS - 1 }, "nps_recently"],
  ])("%s → not due, and shown writes nothing", async (_n, row, reason) => {
    setRow(row);
    expect(await get()).toEqual({ due: false, reason });
    expect((await post({ action: "shown" })).status).toBe(409);
    expect(shownSec()).toBeNull();
  });

  test.each([
    ["another action", { action: "opt_out" }],
    ["an extra field", { action: "shown", came_for: "holding" }],
    ["not JSON", "shown"],
  ])("%s → 400", async (_n, body) => {
    setRow();
    expect((await post(body)).status).toBe(400);
    expect(shownSec()).toBeNull();
  });

  test("signed out, cross-origin or not JSON → refused", async () => {
    setRow();
    expect((await GET(req("/api/survey/answers", "GET", undefined, { cookie: "" }))).status).toBe(401);
    expect((await post({ action: "shown" }, { origin: "http://evil" })).status).toBe(403);
    expect((await post({ action: "shown" }, { "content-type": "text/plain" })).status).toBe(415);
    expect(shownSec()).toBeNull();
  });

  test("answersNotDueReason: an unknown account age is too new", () => {
    const due = { consent: true, createdAtSec: nowSec - 20 * DAY, sessions: 5, lastShownSec: null, npsLastShownSec: null };
    expect(answersNotDueReason(due, NOW)).toBeNull();
    expect(answersNotDueReason({ ...due, createdAtSec: undefined }, NOW)).toBe("account_too_new");
  });

  test("the card's five options are the event plan's and the survey's", () => {
    expect(ANSWERS_CODES).toEqual(["holding", "income", "worst_case", "last_action", "none"]);
    expect(SURVEYS.answers.codes.came_for).toEqual(ANSWERS_CODES);
    expect(PROPERTIES.hardest_to_find.values).toEqual(ANSWERS_CODES);
  });
});
