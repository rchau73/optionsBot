/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET as list } from "@/app/api/compare/route";
import { GET as one } from "@/app/api/compare/[id]/route";
import { signedIn, useTestAuth } from "@/test/auth";

// #302: the comparison reports cmd/compare wrote, read-only, managers and admins.

const A = "20261012T093000Z";
const B = "20261018T120000Z";
const DISCLAIMER = "Backtested/replayed, not a promise: modelled fills, prices and margin on past or scripted data.";

let dir, root, logs;
const report = (over = {}) => ({
  disclaimer: DISCLAIMER,
  underlying: "BTC",
  base: "/home/someone/clone/config_btc.yaml",
  proposed: "my.yaml",
  from: "2026-10-12T00:00:00Z",
  to: "2026-10-18T00:00:00Z",
  backtest: { ran: true },
  replay: { ran: true },
  stress: { ran: false },
  ...over,
});
const put = (id, content) => {
  fs.mkdirSync(path.join(dir, id), { recursive: true });
  fs.writeFileSync(path.join(dir, id, "report.json"), typeof content === "string" ? content : JSON.stringify(content));
};
const get = (fn, cookie, url, ctx) => fn(new Request(`http://monitor${url}`, { headers: cookie ? { cookie } : {} }), ctx);
const ctx = (id) => ({ params: Promise.resolve({ id }) });

beforeEach(async () => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "compare-"));
  dir = path.join(root, "compare");
  fs.mkdirSync(dir);
  process.env.COMPARE_DIR = dir;
  logs = [];
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  useTestAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
  delete process.env.COMPARE_DIR;
});

describe("Scenario: Manager lists reports", () => {
  test("both appear newest first with underlying, file names, days and what ran", async () => {
    put(A, report());
    put(B, report({ underlying: "ETH", replay: { ran: false }, stress: { ran: true } }));
    const { cookie } = await signedIn("mgr", "manager");
    const res = await get(list, cookie, "/api/compare");
    expect(res.status).toBe(200);
    const { reports } = await res.json();
    expect(reports.map((r) => r.id)).toEqual([B, A]);
    expect(reports[1]).toEqual({
      id: A,
      underlying: "BTC",
      base: "config_btc.yaml", // the name only, never the author's path
      proposed: "my.yaml",
      from: "2026-10-12T00:00:00Z",
      to: "2026-10-18T00:00:00Z",
      ran: { backtest: true, replay: true, stress: false },
    });
    expect(reports[0].ran).toEqual({ backtest: true, replay: false, stress: true });
    expect(logs.join("\n")).toContain("comparison reports listed");
  });

  test("an admin may list; an empty or missing dir is an empty list", async () => {
    const { cookie } = await signedIn("boss", "admin");
    expect(await (await get(list, cookie, "/api/compare")).json()).toEqual({ reports: [] });
    process.env.COMPARE_DIR = path.join(root, "nowhere");
    expect(await (await get(list, cookie, "/api/compare")).json()).toEqual({ reports: [] });
  });

  test("at most 50, other folders and unreadable reports skipped", async () => {
    for (let i = 0; i < 55; i++) put(`202610${String(10 + (i % 20)).padStart(2, "0")}T${String(i).padStart(6, "0")}Z`, report());
    fs.mkdirSync(path.join(dir, "notes"));
    put("20261001T000000Z", "{ broken");
    const { cookie } = await signedIn("mgr", "manager");
    const { reports } = await (await get(list, cookie, "/api/compare")).json();
    expect(reports).toHaveLength(50);
    expect(reports.every((r) => /^\d{8}T\d{6}Z$/.test(r.id))).toBe(true);
  });
});

describe("Scenario: User role refused", () => {
  test("403 on both routes, 401 without a session, nothing read", async () => {
    put(A, report());
    const { cookie } = await signedIn("viewer", "user");
    expect((await get(list, cookie, "/api/compare")).status).toBe(403);
    expect((await get(one, cookie, `/api/compare/${A}`, ctx(A))).status).toBe(403);
    expect((await get(list, null, "/api/compare")).status).toBe(401);
    expect((await get(one, null, `/api/compare/${A}`, ctx(A))).status).toBe(401);
  });
});

describe("Scenario: Path traversal", () => {
  test.each(["..", "../secret", "..%2Fsecret", "a/b", "20261012T093000Z/../x", "", "2026-10-12", "20261012T093000Z\n"])(
    "id %j is refused with 400 and no file is read",
    async (id) => {
      fs.writeFileSync(path.join(root, "report.json"), JSON.stringify(report()));
      const read = jest.spyOn(fs, "readFileSync");
      const { cookie } = await signedIn("mgr", "manager");
      const res = await get(one, cookie, "/api/compare/x", ctx(id));
      expect(res.status).toBe(400);
      expect(read).not.toHaveBeenCalled();
    },
  );
});

describe("Scenario: Disclaimer kept", () => {
  test("the report is served as stored, disclaimer included", async () => {
    const stored = JSON.stringify(report(), null, 1);
    put(A, stored);
    const { cookie } = await signedIn("mgr", "manager");
    const res = await get(one, cookie, `/api/compare/${A}`, ctx(A));
    expect(res.status).toBe(200);
    expect(await res.text()).toBe(stored);
    expect(JSON.parse(stored).disclaimer).toBe(DISCLAIMER);
    expect(logs.join("\n")).toContain("comparison report read");
  });

  test("a missing report is 404", async () => {
    const { cookie } = await signedIn("mgr", "manager");
    expect((await get(one, cookie, `/api/compare/${A}`, ctx(A))).status).toBe(404);
  });

  test("malformed JSON is 500 with a log line and none of the content", async () => {
    put(A, '{"secret": "do-not-echo", broken');
    const { cookie } = await signedIn("mgr", "manager");
    const res = await get(one, cookie, `/api/compare/${A}`, ctx(A));
    expect(res.status).toBe(500);
    expect(await res.text()).not.toContain("do-not-echo");
    const log = logs.join("\n");
    expect(log).toContain("comparison report unreadable");
    expect(log).not.toContain("do-not-echo");
  });
});
