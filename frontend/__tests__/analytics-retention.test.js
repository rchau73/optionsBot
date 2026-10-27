/**
 * @jest-environment node
 */
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  DAY_MS,
  deleteEventsOf,
  EVENT_RETENTION_DAYS,
  exportEventsOf,
  pruneAndAudit,
  pruneEvents,
  startRetentionJob,
  stopRetentionJob,
  userEventsEraser,
} from "@/lib/analytics/retention";
import { pseudonymousId } from "@/lib/analytics/config";
import { parseArgs, runUserRequest } from "@/lib/analytics/userrequest";
import { AuthService } from "@/lib/auth/service";
import { AuthStore } from "@/lib/auth/store";
import { authKey } from "@/lib/auth/tokens";
import { TEST_SECRET } from "@/test/auth";

// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
const SALT = "s".repeat(32);
const SESSION = "0123456789abcdef0123456789abcdef";
const NOW = Date.parse("2026-10-23T12:00:00Z");

let dir, file;
const iso = (ms) => new Date(ms).toISOString().replace(/\.\d+Z$/, "Z");
const event = (userId, ageDays, name = "screen_viewed") => ({ event: name, ts: iso(NOW - ageDays * DAY_MS), user_id: userId, session_id: SESSION, role: "user" });
const write = (events) => fs.writeFileSync(file, events.map((e) => (typeof e === "string" ? e : JSON.stringify(e))).join("\n") + "\n", { mode: 0o600 });
const lines = () => fs.readFileSync(file, "utf8").split("\n").filter(Boolean);
const mode = (f) => fs.statSync(f).mode & 0o777;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "retention-"));
  file = path.join(dir, "events.jsonl");
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
});
afterEach(() => {
  jest.restoreAllMocks();
  stopRetentionJob();
  fs.rmSync(dir, { recursive: true, force: true });
});

test("the retention period is one named constant: 13 months (event plan §5)", () => {
  expect(EVENT_RETENTION_DAYS).toBe(395);
});

describe("Scenario: Daily prune", () => {
  test("events dated 400 and 10 days before now: only the 10-day-old event remains and the file keeps mode 0600", () => {
    const u = pseudonymousId(SALT, 1);
    write([event(u, 400), event(u, 10, "help_opened")]);
    expect(pruneEvents(file, NOW)).toEqual({ kept: 1, removed: 1, unreadable: 0 });
    expect(lines().map((l) => JSON.parse(l))).toEqual([event(u, 10, "help_opened")]);
    expect(mode(file)).toBe(0o600);
    expect(fs.existsSync(`${file}.tmp`)).toBe(false);
  });

  test("an event exactly at the period's edge stays; one a second older goes", () => {
    const u = pseudonymousId(SALT, 1);
    write([event(u, EVENT_RETENTION_DAYS), { ...event(u, EVENT_RETENTION_DAYS), ts: iso(NOW - EVENT_RETENTION_DAYS * DAY_MS - 1000) }]);
    expect(pruneEvents(file, NOW)).toMatchObject({ kept: 1, removed: 1 });
  });

  test("a line without a readable time is kept and counted, never deleted on a guess", () => {
    const u = pseudonymousId(SALT, 1);
    write(["not json", JSON.stringify({ event: "x", user_id: u }), event(u, 500)]);
    expect(pruneEvents(file, NOW)).toEqual({ kept: 2, removed: 1, unreadable: 2 });
    expect(lines()).toEqual(["not json", JSON.stringify({ event: "x", user_id: u })]);
  });

  test("nothing to prune leaves the file untouched; no file is no error", () => {
    write([event(pseudonymousId(SALT, 1), 1)]);
    const before = fs.statSync(file);
    expect(pruneEvents(file, NOW)).toEqual({ kept: 1, removed: 0, unreadable: 0 });
    expect(fs.statSync(file).ino).toBe(before.ino);
    expect(pruneEvents(path.join(dir, "none.jsonl"), NOW)).toEqual({ kept: 0, removed: 0, unreadable: 0 });
    expect(fs.existsSync(path.join(dir, "none.jsonl"))).toBe(false);
  });

  test("runs at monitor start and then daily, each run with an audit line", () => {
    jest.useFakeTimers({ now: NOW });
    try {
      const u = pseudonymousId(SALT, 1);
      write([event(u, 400), event(u, EVENT_RETENTION_DAYS - 0.5)]);
      const audit = [];
      startRetentionJob({ file, audit: (e) => audit.push(e) });
      expect(audit).toEqual([
        { time: new Date(NOW).toISOString(), action: "events_pruned", username: "system", retention_days: 395, kept: 1, removed: 1, unreadable: 0 },
      ]);
      jest.advanceTimersByTime(DAY_MS);
      expect(audit).toHaveLength(2);
      expect(audit[1]).toMatchObject({ action: "events_pruned", removed: 1, kept: 0 });
      expect(lines()).toEqual([]);
    } finally {
      jest.useRealTimers();
    }
  });

  test("no events file (in-memory database): no job", () => {
    expect(startRetentionJob({ file: null, audit: () => {} })).toBeNull();
  });

  test("a failed prune is an ERROR log and an audit line, never a throw", () => {
    write([event(pseudonymousId(SALT, 1), 400)]);
    jest.spyOn(fs, "renameSync").mockImplementation(() => {
      throw new Error("disk full");
    });
    const audit = [];
    expect(pruneAndAudit(file, NOW, (e) => audit.push(e))).toEqual({ ok: false, error: "disk full" });
    expect(audit[0]).toMatchObject({ action: "events_prune_failed", username: "system", error: "disk full" });
    expect(console.error.mock.calls.map((c) => JSON.parse(c[0]))).toContainEqual(expect.objectContaining({ level: "ERROR", msg: "analytics events prune failed" }));
  });
});

describe("Scenario: Deleting a user deletes their events", () => {
  async function service(audit = () => {}) {
    const store = await AuthStore.open(":memory:");
    const auth = new AuthService({ store, key: authKey(TEST_SECRET), audit, deleteEvents: userEventsEraser(file, SALT) });
    await auth.bootstrapAdmin({ ADMIN_USERNAME: "admin", ADMIN_PASSWORD: "admin password for tests" });
    const alice = store.createUser({ username: "alice", passwordHash: "x", role: "user" });
    const bob = store.createUser({ username: "bob", passwordHash: "x", role: "user" });
    store.setAnalyticsConsent(alice.id, true);
    store.setAnalyticsConsent(bob.id, true);
    return { auth, store, admin: store.userByName("admin"), alice, bob };
  }

  test("an admin deletes one of two users: none of that user's lines remain, the other's are unchanged", async () => {
    const audit = [];
    const { auth, store, admin, alice, bob } = await service((e) => audit.push(e));
    const a = pseudonymousId(SALT, alice.id);
    const b = pseudonymousId(SALT, bob.id);
    write([event(a, 3), event(b, 3), event(a, 2), event(b, 1, "help_opened")]);
    const bobLines = lines().filter((l) => JSON.parse(l).user_id === b);

    expect(auth.deleteUser(admin, alice.id)).toEqual({ ok: true, events_removed: 2, survey_answers_removed: 0 });
    expect(lines().some((l) => l.includes(a))).toBe(false);
    expect(lines()).toEqual(bobLines);
    expect(mode(file)).toBe(0o600);
    // The consent is a column of the row: it went with it.
    expect(store.userById(alice.id)).toBeNull();
    expect(store.analyticsConsent(alice.id)).toBeNull();
    expect(audit).toContainEqual(
      expect.objectContaining({ action: "events_deleted", username: "admin", target: "alice", target_row_id: alice.id, removed: 2, reason: "user_deleted" }),
    );
  });

  test("a failed event deletion is logged at ERROR and reported to the admin, with the row id to retry", async () => {
    const audit = [];
    const store = await AuthStore.open(":memory:");
    const auth = new AuthService({ store, key: authKey(TEST_SECRET), audit: (e) => audit.push(e), deleteEvents: userEventsEraser(file, null) });
    await auth.bootstrapAdmin({ ADMIN_USERNAME: "admin", ADMIN_PASSWORD: "admin password for tests" });
    const alice = store.createUser({ username: "alice", passwordHash: "x", role: "user" });
    write([event(pseudonymousId(SALT, alice.id), 1)]);

    const r = auth.deleteUser(store.userByName("admin"), alice.id);
    expect(r.ok).toBe(true);
    expect(r.events_error).toMatch(/ANALYTICS_SALT is not set.*--row-id 2/);
    expect(store.userById(alice.id)).toBeNull(); // access is removed regardless
    expect(lines()).toHaveLength(1);
    expect(audit).toContainEqual(expect.objectContaining({ action: "events_delete_failed", target: "alice", target_row_id: alice.id }));
    expect(console.error.mock.calls.map((c) => JSON.parse(c[0]))).toContainEqual(
      expect.objectContaining({ level: "ERROR", msg: "analytics events of a deleted user NOT deleted", user_row_id: alice.id }),
    );
  });

  test("no events file: deletion succeeds with nothing removed", async () => {
    const { auth, admin, alice } = await service();
    expect(auth.deleteUser(admin, alice.id)).toEqual({ ok: true, events_removed: 0, survey_answers_removed: 0 });
  });
});

describe("Scenario: deleting a user removes the answers (#363)", () => {
  async function withAnswers(audit) {
    const store = await AuthStore.open(":memory:");
    const auth = new AuthService({ store, key: authKey(TEST_SECRET), audit: (e) => audit.push(e), deleteEvents: userEventsEraser(file, SALT) });
    await auth.bootstrapAdmin({ ADMIN_USERNAME: "admin", ADMIN_PASSWORD: "admin password for tests" });
    const alice = store.createUser({ username: "alice", passwordHash: "x", role: "user" });
    const bob = store.createUser({ username: "bob", passwordHash: "x", role: "user" });
    store.addSurveyAnswer(alice.id, { surveyId: "nps", codes: { score: "3", reason_code: "other" }, text: "alice's words" }, NOW);
    store.addSurveyAnswer(alice.id, { surveyId: "answers", codes: { came_for: "holding" } }, NOW);
    store.addSurveyAnswer(bob.id, { surveyId: "nps", codes: { score: "10" } }, NOW);
    return { auth, store, admin: store.userByName("admin"), alice, bob };
  }

  test("a user with survey answers is deleted: their rows are gone, the others' stay, and it is audited", async () => {
    const audit = [];
    const { auth, store, admin, alice, bob } = await withAnswers(audit);
    expect(auth.deleteUser(admin, alice.id)).toEqual({ ok: true, events_removed: 0, survey_answers_removed: 2 });
    expect(store.surveyAnswers(alice.id)).toEqual([]);
    expect(store.all("SELECT COUNT(*) AS n FROM survey_answers WHERE free_text LIKE '%alice%'")[0].n).toBe(0);
    expect(store.surveyAnswers(bob.id)).toHaveLength(1);
    expect(audit).toContainEqual(
      expect.objectContaining({ action: "survey_answers_deleted", username: "admin", target: "alice", target_row_id: alice.id, removed: 2, reason: "user_deleted" }),
    );
  });

  test("a failed delete is logged at ERROR with an audit line, and the next start finishes it", async () => {
    const audit = [];
    const { auth, store, admin, alice } = await withAnswers(audit);
    jest.spyOn(store, "deleteSurveyAnswers").mockImplementation(() => {
      throw new Error("disk full");
    });
    const r = auth.deleteUser(admin, alice.id);
    expect(r.ok).toBe(true);
    expect(r.survey_answers_error).toMatch(/disk full.*next start/);
    expect(store.userById(alice.id)).toBeNull(); // access is removed regardless
    expect(audit).toContainEqual(expect.objectContaining({ action: "survey_answers_delete_failed", target: "alice", target_row_id: alice.id, error: "disk full" }));
    expect(console.error.mock.calls.map((c) => JSON.parse(c[0]))).toContainEqual(
      expect.objectContaining({ level: "ERROR", msg: "survey answers of a deleted user NOT deleted", user_row_id: alice.id }),
    );

    // The rows left behind belong to no user: the next open deletes them.
    const dbFile = path.join(dir, "auth.db");
    store.file = dbFile;
    store.save();
    const reopened = await AuthStore.open(dbFile);
    expect(reopened.all("SELECT user_id FROM survey_answers").map((x) => x.user_id)).toEqual([3]);
    expect((await AuthStore.openReadOnly(dbFile)).surveyAnswers(alice.id)).toEqual([]);
  });

  test("deleteEventsOf refuses an id that is not a pseudonymous id", () => {
    write([event(pseudonymousId(SALT, 1), 1)]);
    expect(() => deleteEventsOf(file, "")).toThrow(/16 hex/);
    expect(() => exportEventsOf(file, undefined)).toThrow(/16 hex/);
    expect(lines()).toHaveLength(1);
  });
});

describe("Scenario: Access request", () => {
  let db, auditFile;
  const env = () => ({ AUTH_DB_PATH: db, ANALYTICS_SALT: SALT, EVENTS_PATH: file, AUDIT_PATH: auditFile });
  const audits = () => fs.readFileSync(auditFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));

  beforeEach(async () => {
    db = path.join(dir, "auth.db");
    auditFile = path.join(dir, "audit.jsonl");
    const store = await AuthStore.open(db);
    store.createUser({ username: "admin", passwordHash: "x", role: "admin" });
    store.createUser({ username: "alice", passwordHash: "x", role: "user" });
  });

  test("a user with 5 events: export writes exactly those 5 lines and an audit line records it", async () => {
    const a = pseudonymousId(SALT, 2);
    const other = pseudonymousId(SALT, 1);
    write([event(a, 5), event(other, 5), event(a, 4), event(a, 3), event(other, 1), event(a, 2), event(a, 1)]);
    const aliceLines = lines().filter((l) => JSON.parse(l).user_id === a);
    const out = path.join(dir, "alice.jsonl");

    const r = await runUserRequest({ argv: ["export", "alice", "--out", out], env: env(), cwd: dir, nowMs: NOW, by: "cli:ops" });
    expect(r).toEqual({ code: 0, out: [`exported 5 events and 0 survey answers of alice to ${out}`] });
    expect(fs.readFileSync(out, "utf8").split("\n").filter(Boolean)).toEqual(aliceLines);
    expect(mode(out)).toBe(0o600);
    expect(lines()).toHaveLength(7); // export removes nothing
    expect(audits()).toEqual([
      {
        time: new Date(NOW).toISOString(),
        action: "events_exported",
        username: "cli:ops",
        role: "admin",
        target: "alice",
        target_row_id: 2,
        reason: "request",
        lines: 5,
        survey_answers: 0,
        file: out,
      },
    ]);
  });

  test("export carries the person's survey answers, free text included, and prints counts only (#363)", async () => {
    const store = await AuthStore.open(db);
    store.addSurveyAnswer(2, { surveyId: "nps", codes: { score: "9", reason_code: "saves_time" }, text: "private words" }, NOW);
    store.addSurveyAnswer(1, { surveyId: "exit", codes: { reason_code: "price" } }, NOW);
    store.addSurveyAnswer(2, { surveyId: "answers", codes: { came_for: "income" } }, NOW + 1000);
    write([event(pseudonymousId(SALT, 2), 1)]);
    const out = path.join(dir, "alice.jsonl");

    const r = await runUserRequest({ argv: ["export", "alice", "--out", out], env: env(), cwd: dir, nowMs: NOW });
    expect(r).toEqual({ code: 0, out: [`exported 1 events and 2 survey answers of alice to ${out}`] });
    expect(r.out.join()).not.toContain("private words");
    const exported = fs.readFileSync(out, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
    expect(exported.slice(1)).toEqual([
      { record: "survey_answer", survey_id: "nps", codes: { score: "9", reason_code: "saves_time" }, text: "private words", created_at: new Date(NOW).toISOString() },
      { record: "survey_answer", survey_id: "answers", codes: { came_for: "income" }, text: null, created_at: new Date(NOW + 1000).toISOString() },
    ]);
    expect(audits()).toEqual([expect.objectContaining({ action: "events_exported", lines: 1, survey_answers: 2 })]);
  });

  test("a user database from before #363 (no survey_answers table) exports events only", async () => {
    const store = await AuthStore.open(db);
    store.db.exec("DROP TABLE survey_answers");
    store.save();
    write([event(pseudonymousId(SALT, 2), 1)]);
    const out = path.join(dir, "alice.jsonl");
    const r = await runUserRequest({ argv: ["export", "alice", "--out", out], env: env(), cwd: dir, nowMs: NOW });
    expect(r).toEqual({ code: 0, out: [`exported 1 events and 0 survey answers of alice to ${out}`] });
  });

  test("scripts/analytics-user.mjs runs under plain node from the repository root: counts and path only", () => {
    const a = pseudonymousId(SALT, 2);
    write([event(a, 3), event(pseudonymousId(SALT, 1), 2), event(a, 1)]);
    const out = path.join(dir, "cli.jsonl");
    const r = spawnSync(process.execPath, ["frontend/scripts/analytics-user.mjs", "export", "alice", "--out", out], {
      cwd: path.resolve(__dirname, "../.."),
      env: { PATH: process.env.PATH, ...env() },
      encoding: "utf8",
    });
    expect(r.stderr).toBe("");
    expect(r.status).toBe(0);
    expect(r.stdout).toBe(`exported 2 events and 0 survey answers of alice to ${out}\n`);
    expect(fs.readFileSync(out, "utf8").split("\n").filter(Boolean)).toHaveLength(2);
    expect(audits()).toEqual([expect.objectContaining({ action: "events_exported", target: "alice", lines: 2 })]);

    const bad = spawnSync(process.execPath, ["frontend/scripts/analytics-user.mjs", "delete", "alice"], {
      cwd: path.resolve(__dirname, "../.."),
      env: { PATH: process.env.PATH },
      encoding: "utf8",
    });
    expect(bad.status).toBe(1);
    expect(bad.stderr).toMatch(/AUTH_DB_PATH is not set/);
  });

  test("the default export file sits beside the events, and an existing file is never overwritten", async () => {
    write([event(pseudonymousId(SALT, 2), 1)]);
    const r = await runUserRequest({ argv: ["export", "alice"], env: env(), cwd: dir, nowMs: NOW });
    const out = path.join(dir, "analytics-export-alice-20261023T120000Z.jsonl");
    expect(r.out).toEqual([`exported 1 events and 0 survey answers of alice to ${out}`]);
    const again = await runUserRequest({ argv: ["export", "alice"], env: env(), cwd: dir, nowMs: NOW });
    expect(again.code).toBe(1);
    expect(again.out[0]).toMatch(/export failed: EEXIST/);
    expect(audits().at(-1)).toMatchObject({ action: "events_export_failed" });
  });

  test("delete by username, and by --row-id for a user already deleted; only counts are printed", async () => {
    const a = pseudonymousId(SALT, 2);
    const other = pseudonymousId(SALT, 1);
    write([event(a, 2), event(other, 1), event(a, 1)]);
    const r = await runUserRequest({ argv: ["delete", "alice"], env: env(), cwd: dir, nowMs: NOW, by: "cli:ops" });
    expect(r).toEqual({ code: 0, out: [`deleted 2 events of alice from ${file}`] });
    expect(lines().map((l) => JSON.parse(l).user_id)).toEqual([other]);

    const byRow = await runUserRequest({ argv: ["delete", "--row-id", "1"], env: env(), cwd: dir, nowMs: NOW });
    expect(byRow.out).toEqual([`deleted 1 events of row 1 from ${file}`]);
    expect(lines()).toEqual([]);
    expect(audits().map((e) => [e.action, e.target, e.removed])).toEqual([
      ["events_deleted", "alice", 2],
      ["events_deleted", "row 1", 1],
    ]);
  });

  test("the CLI never writes auth.db (the monitor holds its own copy)", async () => {
    const before = fs.readFileSync(db);
    write([event(pseudonymousId(SALT, 2), 1)]);
    await runUserRequest({ argv: ["delete", "alice"], env: env(), cwd: dir, nowMs: NOW });
    expect(fs.readFileSync(db).equals(before)).toBe(true);
  });

  test.each([
    ["no AUTH_DB_PATH", { AUTH_DB_PATH: "" }, /AUTH_DB_PATH is not set/],
    ["no ANALYTICS_SALT", { ANALYTICS_SALT: "" }, /ANALYTICS_SALT is not set/],
    ["a short salt", { ANALYTICS_SALT: "short" }, /ANALYTICS_SALT is not set/],
    ["a missing database", { AUTH_DB_PATH: "nope.db" }, /no user database/],
  ])("refused: %s", async (_, over, msg) => {
    const r = await runUserRequest({ argv: ["export", "alice"], env: { ...env(), ...over }, cwd: dir, nowMs: NOW });
    expect(r.code).toBe(1);
    expect(r.out[0]).toMatch(msg);
  });

  test("an unknown username is refused and nothing is written", async () => {
    write([event(pseudonymousId(SALT, 2), 1)]);
    const r = await runUserRequest({ argv: ["delete", "carol"], env: env(), cwd: dir, nowMs: NOW });
    expect(r.code).toBe(1);
    expect(r.out[0]).toMatch(/no user carol/);
    expect(lines()).toHaveLength(1);
    expect(fs.existsSync(auditFile)).toBe(false);
  });

  test.each([
    [[], /export or delete/],
    [["show", "alice"], /export or delete/],
    [["export"], /either a username or --row-id/],
    [["export", "alice", "--row-id", "2"], /either a username or --row-id/],
    [["delete", "--row-id", "x"], /--row-id/],
    [["delete", "alice", "--out", "f"], /export only/],
    [["export", "alice", "bob"], /unexpected argument/],
  ])("bad arguments %j", (argv, msg) => {
    expect(parseArgs(argv).error).toMatch(msg);
  });
});

describe("Scenario: Crash during prune", () => {
  test("a process stopped after writing the temp file: events.jsonl is the old full file, and the next run completes", () => {
    const u = pseudonymousId(SALT, 1);
    write([event(u, 400), event(u, 10)]);
    const old = fs.readFileSync(file, "utf8");
    // What a crash between the temp file and the rename leaves behind.
    fs.writeFileSync(`${file}.tmp`, `${JSON.stringify(event(u, 10)).slice(0, 20)}`, { mode: 0o644 });
    expect(fs.readFileSync(file, "utf8")).toBe(old);

    // The monitor restarts: the leftover is overwritten, never read as the events.
    expect(pruneEvents(file, NOW)).toMatchObject({ kept: 1, removed: 1 });
    expect(lines().map((l) => JSON.parse(l))).toEqual([event(u, 10)]);
    expect(mode(file)).toBe(0o600);
    expect(fs.existsSync(`${file}.tmp`)).toBe(false);
  });

  test("the rename fails: events.jsonl is still the old full file and no temp file is left", () => {
    const u = pseudonymousId(SALT, 1);
    write([event(u, 400), event(u, 10)]);
    const old = fs.readFileSync(file, "utf8");
    jest.spyOn(fs, "renameSync").mockImplementation(() => {
      throw new Error("killed");
    });
    expect(() => pruneEvents(file, NOW)).toThrow("killed");
    expect(fs.readFileSync(file, "utf8")).toBe(old);
    expect(fs.existsSync(`${file}.tmp`)).toBe(false);
  });

  test("an append by another process during the rewrite is not lost", () => {
    const u = pseudonymousId(SALT, 1);
    write([event(u, 400)]);
    const late = JSON.stringify(event(u, 0, "help_opened"));
    const real = fs.fsyncSync;
    let once = true;
    jest.spyOn(fs, "fsyncSync").mockImplementation((fd) => {
      real(fd);
      if (once) fs.appendFileSync(file, `${late}\n`);
      once = false;
    });
    expect(pruneEvents(file, NOW)).toEqual({ kept: 1, removed: 1, unreadable: 0 });
    expect(lines()).toEqual([late]);
  });
});
