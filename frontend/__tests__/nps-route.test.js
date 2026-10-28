/**
 * @jest-environment node
 */
import { GET, POST } from "@/app/api/survey/nps/route";
import { AuthStore } from "@/lib/auth/store";
import { NPS_EVERY_DAYS, NPS_MIN_ACCOUNT_DAYS, NPS_MIN_SESSIONS, losingWeek, npsNotDueReason, npsStep2 } from "@/lib/analytics/nps";
import { NPS_TO_IMPROVE, NPS_WHAT_WORKS, validateSurveyAnswer } from "@/lib/analytics/surveys";
import { signedIn, useTestAuth } from "@/test/auth";

// #446: when the monthly NPS card is due — decided on the server from the
// user's row in auth.db, so another device never shows it again.

const DAY = 24 * 3600;
const NOW = Date.UTC(2026, 9, 28, 12);
const nowSec = Math.floor(NOW / 1000);

let cookie, user, auth;
const req = (method, body, over = {}) =>
  new Request("http://monitor/api/survey/nps", {
    method,
    headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...over },
    body: body === undefined ? undefined : typeof body === "string" ? body : JSON.stringify(body),
  });
const get = async () => (await GET(req("GET"))).json();
const post = (body, over) => POST(req("POST", body, over));
/** The user's row as the scenarios set it. */
const setRow = ({ ageDays = 20, sessions = 5, shownDaysAgo = null, consent = true } = {}) => {
  auth.store.setAnalyticsConsent(user.id, consent);
  auth.store.db.run("UPDATE users SET created_at = ?, sign_in_count = ?, nps_last_shown_at = ? WHERE id = ?", [
    nowSec - ageDays * DAY,
    sessions,
    shownDaysAgo == null ? null : nowSec - shownDaysAgo * DAY,
    user.id,
  ]);
};

beforeEach(async () => {
  jest.useFakeTimers({ now: NOW, doNotFake: ["nextTick", "setImmediate", "queueMicrotask"] });
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  useTestAuth();
  ({ cookie, user, auth } = await signedIn("viewer", "user"));
});
afterEach(() => {
  jest.useRealTimers();
  jest.restoreAllMocks();
});

describe("Scenario: badge while due", () => {
  test("a consenting person, 20 days old, 5 sessions, never shown NPS → due", async () => {
    setRow();
    expect(await get()).toEqual({ due: true, reason: null, opted_out: false });
  });

  test("opening the panel with the card records the day once; it is then not due", async () => {
    setRow();
    const res = await post({ action: "shown" });
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ due: false, reason: "shown_recently", opted_out: false });
    expect(auth.store.npsState(user.id).lastShownSec).toBe(nowSec);
    // A second "shown" (another tab) does not move the date: 409, nothing written.
    expect((await post({ action: "shown" })).status).toBe(409);
  });
});

describe("Scenario: not again within 30 days", () => {
  test.each([
    ["10 days ago", false, 10],
    ["29 days ago", false, 29],
    ["30 days ago", true, NPS_EVERY_DAYS],
  ])("shown %s → due is %s, on any device (the date is on the user's row)", async (_n, due, days) => {
    setRow({ shownDaysAgo: days });
    // Another device: a second session of the same user reads the same row.
    const other = await signedIn("viewer", "user");
    const res = await GET(req("GET", undefined, { cookie: other.cookie }));
    expect((await res.json()).due).toBe(due);
  });
});

describe("Scenario: opted out", () => {
  test("Don't ask me again → never due until they turn it back on", async () => {
    setRow();
    expect(await (await post({ action: "opt_out" })).json()).toEqual({ due: false, reason: "opted_out", opted_out: true });
    setRow({ shownDaysAgo: 400 });
    expect((await get()).reason).toBe("opted_out");
    expect(await (await post({ action: "opt_in" })).json()).toEqual({ due: true, reason: null, opted_out: false });
  });
});

describe("the other rules", () => {
  test.each([
    ["no analytics consent", { consent: null }, "no_consent"],
    ["said no to analytics", { consent: false }, "no_consent"],
    ["an account under 14 days old", { ageDays: NPS_MIN_ACCOUNT_DAYS - 1 }, "account_too_new"],
    ["fewer than 3 sessions", { sessions: NPS_MIN_SESSIONS - 1 }, "too_few_sessions"],
  ])("%s → not due", async (_n, row, reason) => {
    setRow(row);
    expect(await get()).toEqual({ due: false, reason, opted_out: false });
    expect((await post({ action: "shown" })).status).toBe(409);
    expect(auth.store.npsState(user.id).lastShownSec).toBeNull();
  });

  test.each([
    ["an unknown action", { action: "answered" }],
    ["an extra field", { action: "shown", score: 9 }],
    ["not JSON", "shown"],
  ])("%s → 400", async (_n, body) => {
    setRow();
    expect((await post(body)).status).toBe(400);
  });

  test("signed out, cross-origin or not JSON → refused", async () => {
    expect((await GET(req("GET", undefined, { cookie: "" }))).status).toBe(401);
    expect((await post({ action: "opt_out" }, { origin: "http://evil" })).status).toBe(403);
    expect((await post({ action: "opt_out" }, { "content-type": "text/plain" })).status).toBe(415);
    expect(auth.store.npsState(user.id).optedOut).toBe(false);
  });

  test("a sign-in counts one more session", () => {
    const before = auth.store.npsState(user.id).sessions;
    auth.store.recordSignIn(user.id, NOW);
    expect(auth.store.npsState(user.id).sessions).toBe(before + 1);
  });
});

describe("the pure rules", () => {
  const due = { consent: true, createdAtSec: nowSec - 20 * DAY, sessions: 5, lastShownSec: null, optedOut: false };

  test("npsNotDueReason: null when every rule holds", () => {
    expect(npsNotDueReason(due, NOW)).toBeNull();
    expect(npsNotDueReason({ ...due, createdAtSec: undefined }, NOW)).toBe("account_too_new");
  });

  test("losingWeek: a net loss, an unknown sign or no bot postpones; results never otherwise", () => {
    const week = (net, known = true) => ({ net_pnl: net, net_pnl_known: known });
    expect(losingWeek([week(0.01), week(0)])).toBe(false);
    expect(losingWeek([week(0.01), week(-0.001)])).toBe(true);
    expect(losingWeek([week(0.01), week(0, false)])).toBe(true);
    expect(losingWeek([week(0.01), null])).toBe(true);
    expect(losingWeek([])).toBe(true);
  });

  test("Scenario: promoter question — 9 and 10 ask what we do best, 0–8 what to improve", () => {
    for (const s of [9, 10]) expect(npsStep2(s)).toEqual({ question: "What do we do best?", codes: NPS_WHAT_WORKS });
    for (const s of [0, 6, 8]) expect(npsStep2(s)).toEqual({ question: "What should we improve first?", codes: NPS_TO_IMPROVE });
  });

  test("POST /api/survey accepts only the list asked for the score", () => {
    const answer = (score, reason_code) => validateSurveyAnswer({ survey_id: "nps", session_id: "0".repeat(32), codes: { score, reason_code } });
    expect(answer("10", "explains_trades").ok).toBe(true);
    expect(answer("9", "other").ok).toBe(true);
    expect(answer("5", "hard_to_find").ok).toBe(true);
    expect(answer("9", "hard_to_find").ok).toBe(false);
    expect(answer("5", "clear_risk").ok).toBe(false);
  });
});

describe("an older auth.db", () => {
  test("gains the NPS columns, seeding the sessions from its sign-ins", async () => {
    const SQL = await (await import("sql.js")).default({
      wasmBinary: (await import("node:fs")).readFileSync(`${process.cwd()}/node_modules/sql.js/dist/sql-wasm.wasm`),
    });
    const db = new SQL.Database();
    db.exec(`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL,
      role TEXT NOT NULL, email TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, analytics_consent INTEGER, last_sign_in_at INTEGER);
      CREATE TABLE refresh_tokens (token_hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL);
      INSERT INTO users (username, password_hash, role, created_at, updated_at, last_sign_in_at) VALUES ('ann', 'h', 'user', 1, 1, 5), ('bob', 'h', 'user', 1, 1, NULL), ('cy', 'h', 'user', 1, 1, 7);
      INSERT INTO refresh_tokens VALUES ('t1', 3, 9, 1), ('t2', 3, 9, 1);`);
    const fs = await import("node:fs");
    const os = await import("node:os");
    const path = await import("node:path");
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "nps-db-"));
    const file = path.join(dir, "auth.db");
    fs.writeFileSync(file, Buffer.from(db.export()));
    const store = await AuthStore.open(file);
    expect([1, 2, 3].map((id) => store.npsState(id).sessions)).toEqual([1, 0, 2]);
    expect(store.npsState(1)).toMatchObject({ lastShownSec: null, optedOut: false });
    fs.rmSync(dir, { recursive: true, force: true });
  });
});
