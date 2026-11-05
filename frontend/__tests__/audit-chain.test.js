/**
 * @jest-environment node
 */
// #619: every audit line carries the hash of the line before it (prev) and its
// own (line_hash); GET /api/admin/audit/verify finds the first edited, removed,
// reordered or damaged line; past AUDIT_MAX_BYTES the file is renamed with its
// UTC time and the chain goes on in a new file.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET as verifyRoute } from "@/app/api/admin/audit/verify/route";
import { archiveName, auditArchives, auditMaxBytes, DEFAULT_AUDIT_MAX_BYTES, fileAudit } from "@/lib/auth/audit";
import { CHAIN_FIELDS, sealIntact, sealLine, sha256, verifyChain } from "@/lib/auth/auditchain";
import { readAudit, verifyAudit } from "@/lib/auth/auditread";
import { resetAuth } from "@/lib/auth/server";
import { auditChainSummary, auditDetail, parseAuditQuery } from "@/lib/audit";
import { signedIn, useTestAuth } from "@/test/auth";

jest.setTimeout(30000);

const ORIGIN = "http://localhost:3000";
const verify = (headers = {}) => verifyRoute(new Request(`${ORIGIN}/api/admin/audit/verify`, { headers }));

let dir;
let file;
let logs;
beforeEach(() => {
  useTestAuth();
  delete process.env.ADMIN_PASSWORD; // no first admin: no argon2 hash per test (#590)
  logs = [];
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "audit-chain-"));
  process.env.AUTH_DB_PATH = path.join(dir, "auth.db");
  file = path.join(dir, "audit.jsonl");
  resetAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  delete process.env.AUTH_DB_PATH;
  delete process.env.AUDIT_MAX_BYTES;
  fs.rmSync(dir, { recursive: true, force: true });
});

const time = (i) => new Date(Date.UTC(2026, 10, 1) + i * 60000).toISOString();
const entry = (i) => ({ time: time(i), action: "signed_in", username: `u${i}`, role: "user", ip: "127.0.0.1" });
const lines = (f = file) => fs.readFileSync(f, "utf8").split("\n").filter(Boolean);
const writeLines = (ls) => fs.writeFileSync(file, ls.map((l) => `${l}\n`).join(""), { mode: 0o600 });

/** Writes `n` audited actions one after another through the real sink. */
function audited(n, opts) {
  const sink = fileAudit(file, opts);
  for (let i = 0; i < n; i++) sink(entry(i));
}

const QUERY = parseAuditQuery(new URLSearchParams()).query;

describe("Scenario: a normal trail is intact", () => {
  test("50 audit actions written one after another: the admin's verify answers ok with checked 50, and one log line names the admin", async () => {
    audited(50);
    const { cookie } = await signedIn("root", "admin");
    const res = await verify({ cookie });
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(await res.json()).toEqual({ off: false, ok: true, checked: 50, legacy: 0, legacy_before: null, first_break: null });
    const logged = logs.map((l) => JSON.parse(l)).filter((l) => l.msg === "audit chain verified");
    expect(logged).toEqual([expect.objectContaining({ level: "INFO", username: "root", ok: true, checked: 50 })]);
  });

  test("each line keeps its fields as given, then prev = the SHA-256 of the line before's bytes ('' for the first) and its own line_hash", () => {
    audited(3);
    const ls = lines();
    const parsed = ls.map((l) => JSON.parse(l));
    expect(parsed.map(({ prev, line_hash, ...rest }) => rest)).toEqual([entry(0), entry(1), entry(2)]);
    expect(parsed.map((p) => p.prev)).toEqual(["", sha256(ls[0]), sha256(ls[1])]);
    for (const l of ls) expect(sealIntact(l)).toBe(true);
    expect(Object.keys(parsed[0]).slice(-2)).toEqual(CHAIN_FIELDS);
    expect(fs.statSync(file).mode & 0o077).toBe(0);
  });

  test("the chain's fields stay out of the page's detail column", () => {
    const e = JSON.parse(sealLine({ time: "t", action: "manual_rebalance", username: "bob", dry_run: true }, "abc"));
    expect(auditDetail(e)).toBe("dry_run=true");
  });

  test("a missing trail is intact with nothing checked", async () => {
    expect(await verifyAudit(file)).toEqual({ ok: true, checked: 0, legacy: 0, legacy_before: null, first_break: null });
  });
});

describe("Scenario: an edited line is found", () => {
  test("one line whose action was changed by hand: ok is false and first_break names that line and its time; every line is still listed", async () => {
    audited(30);
    const ls = lines();
    ls[19] = ls[19].replace('"action":"signed_in"', '"action":"signed_out"');
    writeLines(ls);
    const { cookie } = await signedIn("root", "admin");
    const body = await (await verify({ cookie })).json();
    expect(body).toMatchObject({ ok: false, checked: 19, first_break: { file: "audit.jsonl", archive: false, line: 20, time: time(19), reason: "edited" } });
    expect(logs.map((l) => JSON.parse(l)).find((l) => l.msg === "audit chain verified")).toMatchObject({ level: "WARN", ok: false });
    // A broken chain does not hide any line.
    expect((await readAudit(file, QUERY)).entries).toHaveLength(30);
  });

  test("the last line edited (no later prev covers it) is found by its own hash", async () => {
    audited(5);
    const ls = lines();
    ls[4] = ls[4].replace('"username":"u4"', '"username":"mallory"');
    writeLines(ls);
    expect((await verifyAudit(file)).first_break).toMatchObject({ line: 5, reason: "edited" });
  });
});

describe("Scenario: a removed line is found", () => {
  test("a middle line deleted: the break is at the line after it", async () => {
    audited(30);
    const ls = lines();
    ls.splice(14, 1); // line 15 gone: the old line 16 is now line 15
    writeLines(ls);
    expect(await verifyAudit(file)).toMatchObject({ ok: false, checked: 14, first_break: { line: 15, time: time(15), reason: "link" } });
  });

  test("two lines swapped: a reorder breaks the link", async () => {
    audited(10);
    const ls = lines();
    [ls[3], ls[4]] = [ls[4], ls[3]];
    writeLines(ls);
    expect((await verifyAudit(file)).first_break).toMatchObject({ line: 4, reason: "link" });
  });

  test("a damaged line (cut short) and a line without prev after the chain started are breaks too", async () => {
    audited(4);
    const ls = lines();
    writeLines([...ls.slice(0, 2), ls[2].slice(0, 30), ls[3]]);
    expect((await verifyAudit(file)).first_break).toMatchObject({ line: 3, time: null, reason: "damaged" });
    writeLines([...ls.slice(0, 2), JSON.stringify(entry(9)), ls[3]]);
    expect((await verifyAudit(file)).first_break).toMatchObject({ line: 3, time: time(9), reason: "unchained" });
  });

  test("a last line cut short by a crash: the next action starts on a line of its own and the cut line is the break", async () => {
    audited(2);
    fs.appendFileSync(file, '{"time":"2026-11-01T00:05:00.000Z","act');
    fileAudit(file)(entry(6));
    expect(lines()).toHaveLength(4);
    expect(JSON.parse(lines()[3]).username).toBe("u6");
    expect((await verifyAudit(file)).first_break).toMatchObject({ line: 3, reason: "damaged" });
  });
});

describe("Scenario: legacy lines", () => {
  test("lines written before this change have no prev: counted as legacy, only the chained lines checked", async () => {
    writeLines([0, 1, 2].map((i) => JSON.stringify(entry(i))));
    const sink = fileAudit(file);
    for (let i = 3; i < 8; i++) sink(entry(i));
    // The first chained line links to the last legacy one, so removing it shows too.
    expect(JSON.parse(lines()[3]).prev).toBe(sha256(lines()[2]));
    const result = await verifyAudit(file);
    expect(result).toEqual({ ok: true, checked: 5, legacy: 3, legacy_before: time(3), first_break: null });
    expect(auditChainSummary({ off: false, ...result })).toEqual({
      broken: false,
      text: "Chain intact — 5 lines checked.",
      legacy: `3 legacy lines before ${time(3)} are not covered.`,
    });
  });

  test("a trail with only legacy lines is intact with nothing checked", async () => {
    writeLines([JSON.stringify(entry(0)), "{not json"]);
    expect(await verifyAudit(file)).toEqual({ ok: true, checked: 0, legacy: 2, legacy_before: null, first_break: null });
  });
});

describe("Scenario: two actions at once", () => {
  test("two audited actions in the same millisecond, from two sinks on the same file: both lines are written and the chain stays intact", async () => {
    const a = fileAudit(file);
    const b = fileAudit(file);
    const at = "2026-11-04T10:00:00.000Z";
    await Promise.all([async () => a({ ...entry(0), time: at }), async () => b({ ...entry(1), time: at })].map((f) => f()));
    const ls = lines();
    expect(ls).toHaveLength(2);
    expect(new Set(ls.map((l) => JSON.parse(l).prev)).size).toBe(2);
    expect(await verifyAudit(file)).toMatchObject({ ok: true, checked: 2 });
  });
});

describe("Scenario: rotation keeps the chain", () => {
  test("past AUDIT_MAX_BYTES the old file is renamed with its time, the new file's first line links to its last hash, and verify across both says ok", async () => {
    let clock = Date.parse("2026-11-04T03:14:33.123Z");
    const opts = { maxBytes: 1000, now: () => new Date(clock) };
    const sink = fileAudit(file, opts);
    let i = 0;
    while (!fs.existsSync(file) || fs.statSync(file).size < 1000) sink(entry(i++));
    const before = lines();
    sink(entry(i++)); // the next action rotates
    const archive = path.join(dir, "audit.20261104T031433123Z.jsonl");
    expect(auditArchives(file)).toEqual([archive]);
    expect(lines(archive)).toEqual(before);
    expect(fs.statSync(archive).mode & 0o077).toBe(0);
    expect(lines()).toHaveLength(1);
    expect(JSON.parse(lines()[0]).prev).toBe(sha256(before.at(-1)));
    expect(logs.map((l) => JSON.parse(l)).find((l) => l.msg === "audit trail rotated")).toMatchObject({ file, archive });

    // A second rotation, then everything verifies and pages as one trail.
    clock += 3600 * 1000;
    while (fs.statSync(file).size < 1000) sink(entry(i++));
    sink(entry(i++));
    expect(auditArchives(file)).toHaveLength(2);
    expect(await verifyAudit(file)).toEqual({ ok: true, checked: i, legacy: 0, legacy_before: null, first_break: null });

    const seen = [];
    let q = { ...QUERY, limit: 3 };
    for (let page = 0; page < 100; page++) {
      const r = await readAudit(file, q);
      seen.push(...r.entries.map((e) => e.username));
      if (r.next_before === null) break;
      q = { ...q, before: r.next_before };
    }
    expect(seen).toEqual(Array.from({ length: i }, (_, k) => `u${i - 1 - k}`));
  });

  test("a line removed from an archive is found there, named by the archive's file", async () => {
    const sink = fileAudit(file, { maxBytes: 600, now: () => new Date("2026-11-04T03:14:33.123Z") });
    for (let i = 0; i < 6; i++) sink(entry(i));
    const archive = auditArchives(file)[0];
    const ls = lines(archive);
    fs.writeFileSync(archive, [ls[0], ...ls.slice(2)].map((l) => `${l}\n`).join(""));
    expect((await verifyAudit(file)).first_break).toMatchObject({ file: path.basename(archive), archive: true, line: 2, reason: "link" });
  });

  test("a rotation never overwrites an archive with the same time; nothing is deleted", () => {
    const at = new Date("2026-11-04T03:14:33.123Z");
    const sink = fileAudit(file, { maxBytes: 1, now: () => at });
    for (let i = 0; i < 4; i++) sink(entry(i));
    const archives = auditArchives(file);
    expect(archives.map((a) => path.basename(a))).toEqual(["audit.20261104T031433123Z.jsonl", "audit.20261104T031433124Z.jsonl", "audit.20261104T031433125Z.jsonl"]);
    expect([...archives.flatMap((a) => lines(a)), ...lines()].map((l) => JSON.parse(l).username)).toEqual(["u0", "u1", "u2", "u3"]);
    expect(archiveName("/x/audit.jsonl", at)).toBe("/x/audit.20261104T031433123Z.jsonl");
  });

  test("AUDIT_MAX_BYTES: a positive number of bytes, else the 20 MB default with a warning", () => {
    expect(DEFAULT_AUDIT_MAX_BYTES).toBe(20 * 1024 * 1024);
    expect(auditMaxBytes(undefined)).toBe(DEFAULT_AUDIT_MAX_BYTES);
    expect(auditMaxBytes("")).toBe(DEFAULT_AUDIT_MAX_BYTES);
    expect(auditMaxBytes("5000")).toBe(5000);
    for (const bad of ["0", "-1", "20MB", "1.5"]) expect(auditMaxBytes(bad)).toBe(DEFAULT_AUDIT_MAX_BYTES);
    expect(logs.filter((l) => l.includes("AUDIT_MAX_BYTES"))).toHaveLength(4);
    process.env.AUDIT_MAX_BYTES = "100";
    fileAudit(file)(entry(0));
    fileAudit(file)(entry(1));
    fileAudit(file)(entry(2));
    expect(auditArchives(file)).toHaveLength(2);
  });
});

describe("Scenario: only an admin verifies", () => {
  test("a manager, a user, the service token or no session: 403, 403, 401, 401 and nothing is read", async () => {
    process.env.MONITOR_API_TOKEN = "t".repeat(40);
    audited(3);
    const manager = await signedIn("mia", "manager");
    const user = await signedIn("ulf", "user");
    const open = jest.spyOn(fs.promises, "open");
    const statuses = [];
    for (const headers of [{ cookie: manager.cookie }, { cookie: user.cookie }, { authorization: `Bearer ${"t".repeat(40)}` }, {}]) {
      const res = await verify(headers);
      statuses.push(res.status);
      expect((await res.json()).checked).toBeUndefined();
    }
    delete process.env.MONITOR_API_TOKEN;
    expect(statuses).toEqual([403, 403, 401, 401]);
    expect(open).not.toHaveBeenCalled();
    expect(logs.filter((l) => l.includes("audit chain verified"))).toEqual([]);
  });

  test("an in-memory user database has no trail: off", async () => {
    process.env.AUTH_DB_PATH = ":memory:";
    resetAuth();
    const { cookie } = await signedIn("root", "admin");
    expect(await (await verify({ cookie })).json()).toMatchObject({ off: true, ok: null, checked: 0 });
  });
});

describe("the chain never holds a secret", () => {
  test("prev and line_hash are hashes of audit lines, nothing else", async () => {
    audited(3);
    const ls = lines();
    ls.forEach((l, k) => {
      const { prev, line_hash } = JSON.parse(l);
      expect(prev).toBe(k === 0 ? "" : sha256(ls[k - 1]));
      expect(line_hash).toBe(sha256(`${l.slice(0, l.lastIndexOf(',"line_hash"'))}}`));
    });
  });

  test("verifyChain takes any iterable of lines", async () => {
    const a = sealLine(entry(0), "");
    const b = sealLine(entry(1), sha256(a));
    const one = (text, line) => ({ file: "f", line, bytes: Buffer.from(text) });
    expect(await verifyChain([one(a, 1), one("", 2), one(b, 3)])).toMatchObject({ ok: true, checked: 2 });
    expect(await verifyChain([one(b, 1)])).toMatchObject({ ok: false, first_break: { line: 1, reason: "link" } });
  });
});

describe("the backup's chain check agrees (#650)", () => {
  // The nightly backup checks its copy with internal/backup.CheckAuditChain
  // (tests/backup_check_test.go reads the same fixture): both must call it intact.
  test("the shared fixture (an archive, then audit.jsonl) is intact here too", async () => {
    const file = path.join(__dirname, "..", "..", "tests", "testdata", "audit_chain", "audit.jsonl");
    expect(await verifyAudit(file)).toMatchObject({ ok: true, checked: 4, legacy: 0, first_break: null });
  });
});

describe("auditChainSummary", () => {
  test("broken, in an archive, with legacy; off says nothing", () => {
    const fmt = (t) => `<${t}>`;
    expect(
      auditChainSummary(
        { off: false, ok: false, checked: 1234, legacy: 1, legacy_before: "T0", first_break: { file: "audit.x.jsonl", archive: true, line: 1235, time: "T1", reason: "edited" } },
        fmt,
      ),
    ).toEqual({
      broken: true,
      text: "Chain broken at line 1,235 of audit.x.jsonl (<T1>): this line was edited. 1,234 lines before it checked intact.",
      legacy: "1 legacy line before <T0> is not covered.",
    });
    expect(auditChainSummary({ off: false, ok: false, checked: 0, legacy: 0, first_break: { file: "audit.jsonl", archive: false, line: 1, time: null, reason: "damaged" } }).text).toBe(
      "Chain broken at line 1: this line is not a readable audit line. 0 lines before it checked intact.",
    );
    expect(auditChainSummary({ off: true })).toBeNull();
    expect(auditChainSummary(null)).toBeNull();
  });
});
