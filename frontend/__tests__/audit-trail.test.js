/**
 * @jest-environment node
 */
// #308 + #249: admins read audit.jsonl through GET /api/admin/audit — newest
// first, filtered by person, action and UTC day, paged, damaged lines skipped.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET as auditRoute } from "@/app/api/admin/audit/route";
import { readAudit } from "@/lib/auth/auditread";
import { resetAuth } from "@/lib/auth/server";
import { AUDIT_ACTIONS, auditDetail, auditQueryString, parseAuditQuery } from "@/lib/audit";
import { signedIn, useTestAuth } from "@/test/auth";

jest.setTimeout(30000);

const ORIGIN = "http://localhost:3000";
const get = (query, cookie) => auditRoute(new Request(`${ORIGIN}/api/admin/audit${query ? `?${query}` : ""}`, { headers: { cookie } }));

let dir;
let file;
beforeEach(() => {
  useTestAuth();
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "audit-trail-"));
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

/** Signs in an admin (its sign-in writes nothing: signedIn adds a session directly), then replaces the trail with `lines`. */
async function adminWith(lines) {
  const s = await signedIn("root", "admin");
  fs.writeFileSync(file, lines.map((l) => `${l}\n`).join(""), { mode: 0o600 });
  return s;
}

describe("Scenario: admins only", () => {
  test("a signed-in manager or user gets 403; no session 401", async () => {
    await adminWith([line("2026-10-20T10:00:00.000Z", "signed_in", "bob")]);
    for (const role of ["manager", "user"]) {
      const { cookie } = await signedIn(`p-${role}`, role);
      const res = await get("", cookie);
      expect(res.status).toBe(403);
      expect((await res.json()).entries).toBeUndefined();
    }
    expect((await get("", "")).status).toBe(401);
  });
});

describe("Scenario: filter by person and action", () => {
  test("user=bob action=manual_close lists only bob's manual closes, newest first, as stored", async () => {
    const { cookie } = await adminWith([
      line("2026-10-20T10:00:00.000Z", "manual_close", "bob", { bot: "btc", position_ids: ["p1"], status: 200 }),
      line("2026-10-20T11:00:00.000Z", "manual_close", "alice", { bot: "btc", position_ids: ["p2"], status: 200 }),
      line("2026-10-20T12:00:00.000Z", "signed_in", "bob", { ip: "127.0.0.1" }),
      line("2026-10-21T09:00:00.000Z", "manual_close", "bob", { bot: "eth", position_ids: ["p3", "p4"], status: 502 }),
    ]);
    const res = await get("user=Bob&action=manual_close", cookie);
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    const body = await res.json();
    expect(body.off).toBe(false);
    expect(body.skipped).toBe(0);
    expect(body.next_before).toBeNull();
    expect(body.entries).toEqual([
      { time: "2026-10-21T09:00:00.000Z", action: "manual_close", username: "bob", role: "manager", bot: "eth", position_ids: ["p3", "p4"], status: 502 },
      { time: "2026-10-20T10:00:00.000Z", action: "manual_close", username: "bob", role: "manager", bot: "btc", position_ids: ["p1"], status: 200 },
    ]);
  });

  test("from / to keep only the UTC days asked, inclusive", async () => {
    const { cookie } = await adminWith([
      line("2026-10-19T23:59:59.000Z", "signed_in", "bob"),
      line("2026-10-20T00:00:00.000Z", "signed_in", "bob"),
      line("2026-10-21T23:59:59.000Z", "signed_in", "bob"),
      line("2026-10-22T00:00:00.000Z", "signed_in", "bob"),
    ]);
    const body = await (await get("from=2026-10-20&to=2026-10-21", cookie)).json();
    expect(body.entries.map((e) => e.time)).toEqual(["2026-10-21T23:59:59.000Z", "2026-10-20T00:00:00.000Z"]);
  });
});

describe("Scenario: a damaged line does not break the page", () => {
  test("one line that is not JSON: the others are listed and skipped is 1", async () => {
    const { cookie } = await adminWith([line("2026-10-20T10:00:00.000Z", "signed_in", "bob"), "{not json", line("2026-10-20T11:00:00.000Z", "signed_in", "alice")]);
    const res = await get("", cookie);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.entries.map((e) => e.username)).toEqual(["alice", "bob"]);
    expect(body.skipped).toBe(1);
  });

  test("JSON that is not an object counts as damaged; blank lines are ignored", async () => {
    const { cookie } = await adminWith(["[1,2]", "42", "", line("2026-10-20T10:00:00.000Z", "signed_in", "bob")]);
    const body = await (await get("", cookie)).json();
    expect(body.entries).toHaveLength(1);
    expect(body.skipped).toBe(2);
  });
});

describe("empty, missing and off", () => {
  test("a missing file is an empty list, not an error", async () => {
    const { cookie } = await signedIn("root", "admin");
    fs.rmSync(file, { force: true }); // the first admin's user_created line
    expect(fs.existsSync(file)).toBe(false);
    expect(await (await get("", cookie)).json()).toEqual({ off: false, entries: [], skipped: 0, next_before: null });
  });

  test("an in-memory user database has no audit trail: off, not empty", async () => {
    process.env.AUTH_DB_PATH = ":memory:";
    resetAuth();
    const { cookie } = await signedIn("root", "admin");
    expect(await (await get("", cookie)).json()).toEqual({ off: true, entries: [], skipped: 0, next_before: null });
  });

  test("a query outside the rules is 400 with the reason", async () => {
    const { cookie } = await signedIn("root", "admin");
    for (const [q, why] of [
      ["action=drop_table", /action: one of/],
      ["from=2026-13-01", /from: a UTC day/],
      ["to=yesterday", /to: a UTC day/],
      ["from=2026-10-21&to=2026-10-20", /from is after to/],
      ["limit=0", /limit: 1 to 1000/],
      ["limit=1001", /limit: 1 to 1000/],
      ["before=-1", /before/],
    ]) {
      const res = await get(q, cookie);
      expect(res.status).toBe(400);
      expect((await res.json()).error).toMatch(why);
    }
  });
});

describe("paging", () => {
  test("100 a page by default, then the cursor gives the older lines with none repeated or lost — across 64 KB chunks", async () => {
    const pad = "x".repeat(300); // ~ 400 bytes a line: 450 lines span several read chunks
    const lines = Array.from({ length: 450 }, (_, i) => line(new Date(Date.UTC(2026, 9, 1) + i * 60000).toISOString(), "signed_in", `u${i}`, { pad }));
    const { cookie } = await adminWith(lines);

    const seen = [];
    let before = null;
    for (let pageNo = 0; pageNo < 10; pageNo++) {
      const body = await (await get(auditQueryString({ before }), cookie)).json();
      seen.push(...body.entries.map((e) => e.username));
      if (body.next_before === null) break;
      expect(body.entries).toHaveLength(100);
      before = body.next_before;
    }
    expect(seen).toEqual(Array.from({ length: 450 }, (_, i) => `u${449 - i}`));
  });

  test("limit sets the page size; a last page that is exactly full has no cursor", async () => {
    const lines = Array.from({ length: 4 }, (_, i) => line(`2026-10-2${i}T10:00:00.000Z`, "signed_in", `u${i}`));
    const { cookie } = await adminWith(lines);
    const first = await (await get("limit=2", cookie)).json();
    expect(first.entries.map((e) => e.username)).toEqual(["u3", "u2"]);
    const second = await (await get(`limit=2&before=${first.next_before}`, cookie)).json();
    expect(second.entries.map((e) => e.username)).toEqual(["u1", "u0"]);
    expect(second.next_before).toBeNull();
  });

  test("a file without a final newline and multi-byte characters read whole", async () => {
    fs.writeFileSync(file, `${line("2026-10-20T10:00:00.000Z", "signed_in", "joão")}\n${line("2026-10-20T11:00:00.000Z", "signed_in", "zoë")}`);
    const r = await readAudit(file, parseAuditQuery(new URLSearchParams()).query);
    expect(r.entries.map((e) => e.username)).toEqual(["zoë", "joão"]);
  });
});

describe("the read never writes", () => {
  test("the file is unchanged, still owner-only, after reading", async () => {
    const { cookie } = await adminWith([line("2026-10-20T10:00:00.000Z", "signed_in", "bob")]);
    const before = fs.readFileSync(file);
    await get("", cookie);
    expect(fs.readFileSync(file).equals(before)).toBe(true);
    expect(fs.statSync(file).mode & 0o777).toBe(0o600);
  });
});

describe("lib/audit.js", () => {
  test("the known actions include every action the code audits", () => {
    for (const a of ["signed_in", "role_changed", "password_reset", "reset_link_issued", "password_reset_by_link", "manual_close", "manual_rebalance"]) {
      expect(AUDIT_ACTIONS).toContain(a);
    }
  });

  test("detail lists the fields without their own column, as stored", () => {
    expect(auditDetail({ time: "t", action: "manual_rebalance", username: "bob", role: "manager", bot: "btc", dry_run: true, status: 200 })).toBe("dry_run=true status=200");
    expect(auditDetail({ time: "t", action: "manual_close", username: "bob", position_ids: ["a", "b"] })).toBe('position_ids=["a","b"]');
  });

  test("the query string leaves empty filters out", () => {
    expect(auditQueryString({ user: " bob ", action: "", from: "2026-10-20" })).toBe("user=bob&from=2026-10-20");
    expect(auditQueryString({ before: 0 })).toBe("before=0");
  });
});
