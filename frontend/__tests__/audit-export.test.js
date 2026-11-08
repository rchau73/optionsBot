/**
 * @jest-environment node
 */
// #702: an admin downloads the filtered audit trail as CSV through
// GET /api/admin/audit/export, and the export is itself an audit line.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET as exportRoute } from "@/app/api/admin/audit/export/route";
import { auditCsvStream, planAuditExport } from "@/lib/auth/auditexport";
import { verifyAudit } from "@/lib/auth/auditread";
import { getAuth, resetAuth } from "@/lib/auth/server";
import { AUDIT_ACTIONS, AUDIT_EXPORT_MAX, auditCsvCell, auditCsvRow, auditExportSummary, parseAuditQuery } from "@/lib/audit";
import { signedIn, useTestAuth } from "@/test/auth";

jest.setTimeout(30000);

const ORIGIN = "http://localhost:3000";
const HEADER = "time,person,role,action,target,detail";
const get = (query, cookie) => exportRoute(new Request(`${ORIGIN}/api/admin/audit/export${query ? `?${query}` : ""}`, { headers: { cookie } }));
const filtersOf = (query) => parseAuditQuery(new URLSearchParams(query)).query;

let dir;
let file;
beforeEach(() => {
  useTestAuth();
  delete process.env.ADMIN_PASSWORD; // no first admin: see audit-trail.test.js
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "audit-export-"));
  process.env.AUTH_DB_PATH = path.join(dir, "auth.db");
  file = path.join(dir, "audit.jsonl");
  resetAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  delete process.env.AUTH_DB_PATH;
  fs.rmSync(dir, { recursive: true, force: true });
});

const line = (time, action, username, extra = {}) => JSON.stringify({ time, action, username, role: "manager", ...extra });

/** Signs in an admin, then replaces the trail with `lines`. */
async function adminWith(lines) {
  const s = await signedIn("root", "admin");
  fs.writeFileSync(file, lines.map((l) => `${l}\n`).join(""), { mode: 0o600 });
  return s;
}

const fileLines = () => fs.readFileSync(file, "utf8").split("\n").filter(Boolean);
const exportLines = () => fileLines().map((l) => JSON.parse(l)).filter((l) => l.action === "audit_exported");

describe("Scenario: admin exports a filtered period", () => {
  test("a CSV of exactly those lines, newest first, with no prev or line_hash column", async () => {
    const chain = { prev: "a".repeat(64), line_hash: "b".repeat(64) };
    const { cookie } = await adminWith([
      line("2026-10-12T10:00:00.000Z", "manual_close", "bob", { bot: "btc", ...chain }),
      line("2026-10-20T09:00:00.000Z", "manual_close", "bob", { bot: "btc", position_ids: ["p1"], status: 200, ...chain }),
      line("2026-10-20T10:00:00.000Z", "manual_close", "alice", { bot: "btc", ...chain }),
      line("2026-10-22T09:00:00.000Z", "role_changed", "bob", { target: "carol", to: "user", ...chain }),
      line("2026-10-30T09:00:00.000Z", "signed_in", "bob", chain),
    ]);
    const res = await get("user=bob&from=2026-10-19&to=2026-10-25", cookie);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toBe("text/csv; charset=utf-8");
    expect(res.headers.get("content-disposition")).toMatch(/^attachment; filename="audit-trail-\d{4}-\d{2}-\d{2}\.csv"$/);
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(res.headers.get("x-audit-lines")).toBe("2");
    expect(res.headers.get("x-audit-cut")).toBe("false");
    expect((await res.text()).split("\r\n")).toEqual([
      HEADER,
      '2026-10-22T09:00:00.000Z,bob,manager,role_changed,carol,"{""to"":""user""}"',
      '2026-10-20T09:00:00.000Z,bob,manager,manual_close,btc,"{""position_ids"":[""p1""],""status"":200}"',
      "",
    ]);
  });

  test("the archives are read too, and a bad filter is a 400 before anything is written", async () => {
    const { cookie } = await adminWith([line("2026-10-20T09:00:00.000Z", "signed_in", "bob")]);
    fs.writeFileSync(path.join(dir, "audit.20261001T000000000Z.jsonl"), `${line("2026-10-01T09:00:00.000Z", "signed_in", "bob")}\n`);
    const text = await (await get("", cookie)).text();
    expect(text.split("\r\n").map((l) => l.slice(0, 10))).toEqual(["time,perso", "2026-10-20", "2026-10-01", ""]);

    const before = fileLines().length;
    expect((await get("action=nope", cookie)).status).toBe(400);
    expect((await get("from=2026-10-20&to=2026-10-19", cookie)).status).toBe(400);
    expect(fileLines()).toHaveLength(before);
  });

  test("no matching line is a file with only the header; a missing trail is too", async () => {
    const { cookie } = await adminWith([line("2026-10-20T09:00:00.000Z", "signed_in", "bob")]);
    expect(await (await get("user=nobody", cookie)).text()).toBe(`${HEADER}\r\n`);
    fs.rmSync(file);
    expect(await (await get("", cookie)).text()).toBe(`${HEADER}\r\n`);
  });

  test("a damaged line is left out and counted, never an error", async () => {
    const { cookie } = await adminWith(["not json", line("2026-10-20T09:00:00.000Z", "signed_in", "bob"), "[1]"]);
    const res = await get("", cookie);
    expect(res.status).toBe(200);
    expect(res.headers.get("x-audit-lines")).toBe("1");
    expect(res.headers.get("x-audit-skipped")).toBe("2");
    expect((await res.text()).split("\r\n")).toHaveLength(3);
  });

  test("the first pass failing is a 500 with no file", async () => {
    const { cookie } = await adminWith([line("2026-10-20T09:00:00.000Z", "signed_in", "bob")]);
    jest.spyOn(fs.promises, "open").mockRejectedValue(new Error("disk gone"));
    const res = await get("", cookie);
    expect(res.status).toBe(500);
    expect((await res.json()).error).toBe("the audit trail could not be read: see the monitor's logs");
  });
});

describe("Scenario: the export is audited", () => {
  test("an audit_exported line with the filters and the count, no exported line copied, the chain intact", async () => {
    const { cookie } = await adminWith([]);
    const auth = await getAuth();
    auth.recordAction({ username: "bob", role: "manager" }, "manual_close", { bot: "btc", position_ids: ["secret-looking-id"] });
    const log = jest.spyOn(console, "log");

    const res = await get("user=bob&action=manual_close&from=2026-01-01", cookie);
    expect(exportLines()).toEqual([]); // only once the file has gone out
    await res.text();

    const [audited, ...more] = exportLines();
    expect(more).toEqual([]);
    expect(audited).toEqual(
      expect.objectContaining({
        action: "audit_exported",
        username: "root",
        role: "admin",
        filters: { user: "bob", action: "manual_close", from: "2026-01-01", to: null },
        lines: 1,
        cut: false,
        skipped: 0,
        complete: true,
      }),
    );
    expect(JSON.stringify(audited)).not.toContain("secret-looking-id");
    expect(log.mock.calls.map(([l]) => JSON.parse(l))).toContainEqual(expect.objectContaining({ msg: "audit exported", username: "root", lines: 1 }));
    expect((await verifyAudit(file)).ok).toBe(true);
    expect(AUDIT_ACTIONS).toContain("audit_exported");
  });

  test("the export's own line is not in its file, and the next export shows it", async () => {
    const { cookie } = await adminWith([line("2026-10-20T09:00:00.000Z", "signed_in", "bob")]);
    const first = await (await get("", cookie)).text();
    expect(first).not.toContain("audit_exported");
    expect(await (await get("", cookie)).text()).toContain("audit_exported");
  });

  test("a download stopped halfway is audited with what was sent, complete false", async () => {
    const { cookie } = await adminWith([1, 2, 3].map((d) => line(`2026-10-0${d}T09:00:00.000Z`, "signed_in", "bob")));
    const res = await get("", cookie);
    const reader = res.body.getReader();
    await reader.read(); // the header
    await reader.read(); // the first line
    await reader.cancel();
    const [audited] = exportLines();
    expect(audited).toEqual(expect.objectContaining({ complete: false }));
    expect(audited.lines).toBeLessThan(3);
  });

  test("a read failing mid-file errors the download, is logged and is audited as incomplete", async () => {
    const { cookie } = await adminWith([line("2026-10-20T09:00:00.000Z", "signed_in", "bob")]);
    const res = await get("", cookie);
    const reader = res.body.getReader();
    jest.spyOn(fs.promises, "open").mockRejectedValue(new Error("disk gone"));
    await expect(reader.read()).resolves.toEqual(expect.objectContaining({ done: false })); // the header, sent before any read
    await expect(reader.read()).rejects.toThrow("disk gone");
    expect(exportLines()).toEqual([expect.objectContaining({ lines: 0, complete: false })]);
    const errors = console.error.mock.calls.map(([l]) => JSON.parse(l));
    expect(errors).toContainEqual(expect.objectContaining({ msg: "audit export failed", username: "root", error: "disk gone" }));
  });
});

describe("Scenario: not an admin", () => {
  test("a manager or a user gets 403, no session 401, and no file is opened", async () => {
    await adminWith([line("2026-10-20T09:00:00.000Z", "signed_in", "bob")]);
    const open = jest.spyOn(fs.promises, "open");
    for (const role of ["manager", "user"]) {
      const { cookie } = await signedIn(`p-${role}`, role);
      const res = await get("", cookie);
      expect(res.status).toBe(403);
      expect(await res.text()).not.toContain("signed_in");
    }
    expect((await get("", "")).status).toBe(401);
    expect(open).not.toHaveBeenCalled();
    expect(exportLines()).toEqual([]);
  });
});

describe("Scenario: spreadsheet formulas are neutralised", () => {
  test.each([
    ["=HYPERLINK(1)", "'=HYPERLINK(1)"],
    ["+1", "'+1"],
    ["-1", "'-1"],
    ["@SUM", "'@SUM"],
    ["\tx", "'\tx"],
    ["a=b", "a=b"],
    ['=x,"y"', `"'=x,""y"""`],
    ["two\nlines", '"two\nlines"'],
    [null, ""],
    [undefined, ""],
    [200, "200"],
  ])("cell %j → %j", (value, cell) => {
    expect(auditCsvCell(value)).toBe(cell);
  });

  test("a typed username and a detail value that start with = are prefixed", () => {
    const row = auditCsvRow({ time: "2026-10-20T09:00:00.000Z", action: "sign_in_failed", username: "=cmd()", reason: "=1+1" });
    expect(row).toBe(`2026-10-20T09:00:00.000Z,'=cmd(),,sign_in_failed,,"'{""reason"":""=1+1""}"\r\n`);
  });

  test("a target and a bot on one line both stay; an ordinary detail stays plain JSON", () => {
    expect(auditCsvRow({ action: "x", target: "carol", bot: "btc" })).toBe(',,,x,carol,"{""bot"":""btc""}"\r\n');
    expect(auditCsvRow({ action: "x", bot: "btc" })).toBe(",,,x,btc,{}\r\n");
  });
});

describe("Scenario: a very long trail is capped", () => {
  const many = (n) => Array.from({ length: n }, (_, i) => line("2026-10-20T09:00:00.000Z", "signed_in", `u${i}`));

  test("more than 50,000 matching lines: the file holds the newest 50,000 and says it was cut", async () => {
    const { cookie } = await adminWith(many(AUDIT_EXPORT_MAX + 1));
    const res = await get("", cookie);
    expect(res.headers.get("x-audit-lines")).toBe("50000");
    expect(res.headers.get("x-audit-cut")).toBe("true");
    const rows = (await res.text()).trimEnd().split("\r\n");
    expect(rows).toHaveLength(AUDIT_EXPORT_MAX + 1); // with the header
    expect(rows[1]).toContain(`,u${AUDIT_EXPORT_MAX},`);
    expect(rows.at(-1)).toContain(",u1,");
    expect(exportLines()).toEqual([expect.objectContaining({ lines: AUDIT_EXPORT_MAX, cut: true, complete: true })]);
  });

  test("exactly at the cap nothing is cut; one past it is", async () => {
    fs.writeFileSync(file, many(3).map((l) => `${l}\n`).join(""));
    const query = filtersOf("");
    expect(await planAuditExport(file, query, 3)).toEqual(expect.objectContaining({ lines: 3, cut: false }));
    expect(await planAuditExport(file, query, 2)).toEqual(expect.objectContaining({ lines: 2, cut: true }));
  });

  test("the two passes agree when lines are appended between them", async () => {
    fs.writeFileSync(file, many(2).map((l) => `${l}\n`).join(""));
    const query = filtersOf("");
    const plan = await planAuditExport(file, query);
    fs.appendFileSync(file, `${line("2026-10-20T09:00:00.000Z", "signed_in", "late")}\n`);
    const done = jest.fn();
    const text = await new Response(auditCsvStream(file, query, plan, done)).text();
    expect(text).not.toContain("late");
    expect(done).toHaveBeenCalledWith({ lines: 2, complete: true, error: null });
  });
});

describe("Scenario: in-memory trail", () => {
  test("no trail to export: the route says the trail is off", async () => {
    const { cookie } = await adminWith([]);
    process.env.AUTH_DB_PATH = ":memory:";
    const res = await get("", cookie);
    expect(res.status).toBe(409);
    expect((await res.json()).error).toContain("the trail is off");
  });
});

describe("what the page says", () => {
  test.each([
    [{ lines: 1, cut: false, skipped: 0 }, "Exported 1 line."],
    [{ lines: 1234, cut: false, skipped: 1 }, "Exported 1,234 lines. 1 damaged line was skipped."],
    [{ lines: 50000, cut: true, skipped: 2 }, "The export was cut: the file holds the newest 50,000 lines. Narrow the filters to export the older ones. 2 damaged lines were skipped."],
  ])("%j", (result, text) => {
    expect(auditExportSummary(result)).toEqual({ cut: result.cut, text });
  });
});
