/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET as list } from "@/app/api/lab/runs/route";
import * as listModule from "@/app/api/lab/runs/route";
import * as oneModule from "@/app/api/lab/runs/[id]/route";
import { ANALYSIS_WITHHELD, L10_ANALYSIS_GATE_OPEN, LAB_LABEL, readRun } from "@/lib/lab";
import { signedIn, useTestAuth } from "@/test/auth";

// #539: the strategy-change lab's read-only API, managers and admins.

const one = oneModule.GET;
const A = "20261012T093000Z";
const B = "20261018T120000Z";

let dir, root, logs;
const backtestRun = (over = {}) => ({
  net_usd: 1200,
  return_pct: 1.2,
  fees_usd: 300,
  worst_week_usd: -800,
  worst_week_pct: -0.8,
  max_drawdown_usd: 1500,
  max_drawdown_pct: 1.5,
  ...over,
});
const replayRun = (over = {}) => ({ net: 0.01, return_pct: 0.5, fees: 0.004, worst_week_pct: -0.3, max_drawdown: 0.02, ...over });
const report = (over = {}) => ({
  disclaimer: "Backtested/replayed, not a promise: modelled fills, prices and margin on past or scripted data.",
  underlying: "BTC",
  base: "config_btc.yaml",
  proposed: "my.yaml",
  diffs: [{ key: "slots.0.delta", base: "0.15", proposed: "0.12" }],
  backtest: { ran: true, equity_usd: 100000, base: backtestRun(), proposed: backtestRun({ return_pct: 1.8, fees_usd: 400 }) },
  replay: { ran: true, equity: 2, base: replayRun(), proposed: replayRun({ error: "boom" }) },
  stress: { ran: false },
  ...over,
});
const put = (id, content, file = "report.json") => {
  fs.mkdirSync(path.join(dir, id), { recursive: true });
  fs.writeFileSync(path.join(dir, id, file), typeof content === "string" ? content : JSON.stringify(content));
};
const req = (cookie, url, method = "GET") => new Request(`http://monitor${url}`, { method, headers: cookie ? { cookie } : {} });
const ctx = (id) => ({ params: Promise.resolve({ id }) });

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "lab-"));
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

describe("Scenario: list runs", () => {
  test("both appear, newest first, with the setting changed and the headline numbers", async () => {
    put(A, report());
    put(B, report({ underlying: "ETH", diffs: [{ key: "min_premium", base: "0.001", proposed: "0.002" }], replay: { ran: false } }));
    const { cookie } = await signedIn("mgr", "manager");
    const res = await list(req(cookie, "/api/lab/runs"));
    expect(res.status).toBe(200);
    const { runs } = await res.json();
    expect(runs.map((r) => r.id)).toEqual([B, A]);
    expect(runs[0].changed).toEqual([{ key: "min_premium", base: "0.001", proposed: "0.002" }]);
    expect(runs[0].replay).toBeNull();
    expect(runs[1]).toEqual({
      id: A,
      time: "2026-10-12T09:30:00Z",
      underlying: "BTC",
      label: LAB_LABEL,
      changed: [{ key: "slots.0.delta", base: "0.15", proposed: "0.12" }],
      backtest: {
        base: { return_pct: 1.2, worst_week_pct: -0.8, max_drawdown_pct: 1.5, fees_pct: 0.3 },
        proposed: { return_pct: 1.8, worst_week_pct: -0.8, max_drawdown_pct: 1.5, fees_pct: 0.4 },
      },
      // Percentages only: drawdown and fees as % of the starting equity; a failed run is null.
      replay: { base: { return_pct: 0.5, worst_week_pct: -0.3, max_drawdown_pct: 1, fees_pct: 0.2 }, proposed: null },
      stress_ran: false,
      windows: null,
      cliffs: null,
    });
    expect(logs.join("\n")).toContain("lab runs listed");
  });

  test("every run carries the label and the risk fields beside the return", async () => {
    put(A, report());
    const { cookie } = await signedIn("boss", "admin");
    const { runs } = await (await list(req(cookie, "/api/lab/runs"))).json();
    expect(runs[0].label).toBe("backtested, not a forecast");
    for (const side of [runs[0].backtest.base, runs[0].backtest.proposed, runs[0].replay.base]) {
      expect(Object.keys(side).sort()).toEqual(["fees_pct", "max_drawdown_pct", "return_pct", "worst_week_pct"]);
    }
  });

  test("windows' wins and the neighbours' cliffs are counted; no amount reaches the list", async () => {
    const windows = { summary: { backtest: { run: 4, proposed_wins: 3, base_wins: 1 }, replay: { run: 2, proposed_wins: 0, base_wins: 2 } } };
    put(A, report({ windows, neighbours: { cliffs: [{ reason: "worst week" }] } }));
    const { cookie } = await signedIn("mgr", "manager");
    const { runs } = await (await list(req(cookie, "/api/lab/runs"))).json();
    expect(runs[0].windows).toEqual({ backtest: { run: 4, proposed_wins: 3, base_wins: 1 }, replay: { run: 2, proposed_wins: 0, base_wins: 2 } });
    expect(runs[0].cliffs).toBe(1);
    const text = JSON.stringify(runs);
    for (const amount of ["net_usd", "fees_usd", "worst_week_usd", "max_drawdown_usd", '"net"', '"fees"']) expect(text).not.toContain(amount);
  });

  test("an empty or missing dir is an empty list; an unreadable report is skipped", async () => {
    const { cookie } = await signedIn("mgr", "manager");
    expect(await (await list(req(cookie, "/api/lab/runs"))).json()).toEqual({ runs: [] });
    put(A, "{ broken");
    expect(await (await list(req(cookie, "/api/lab/runs"))).json()).toEqual({ runs: [] });
    expect(logs.join("\n")).toContain("lab runs unreadable");
    process.env.COMPARE_DIR = path.join(root, "nowhere");
    expect(await (await list(req(cookie, "/api/lab/runs"))).json()).toEqual({ runs: [] });
  });
});

describe("Scenario: a user may not", () => {
  test("403 on either lab route for the role user, 401 without a session", async () => {
    put(A, report());
    const { cookie } = await signedIn("viewer", "user");
    expect((await list(req(cookie, "/api/lab/runs"))).status).toBe(403);
    expect((await one(req(cookie, `/api/lab/runs/${A}`), ctx(A))).status).toBe(403);
    expect((await list(req(null, "/api/lab/runs"))).status).toBe(401);
    expect((await one(req(null, `/api/lab/runs/${A}`), ctx(A))).status).toBe(401);
  });
});

describe("Scenario: analysis withheld", () => {
  test("the analysis is not in the response and a notice says why", async () => {
    put(A, report());
    put(A, { text: "SECRET-ANALYSIS the proposal looks better" }, "analysis.json");
    put(A, "SECRET-ANALYSIS", "analysis.md");
    const read = jest.spyOn(fs, "readFileSync");
    const { cookie } = await signedIn("mgr", "manager");
    const res = await one(req(cookie, `/api/lab/runs/${A}`), ctx(A));
    expect(res.status).toBe(200);
    const text = await res.text();
    expect(text).not.toContain("SECRET-ANALYSIS");
    const body = JSON.parse(text);
    expect(body.analysis).toBe(ANALYSIS_WITHHELD);
    expect(body.analysis).toBe("withheld pending legal review");
    expect(read.mock.calls.map((c) => String(c[0])).filter((f) => f.includes("analysis"))).toEqual([]);
    expect(logs.join("\n")).toContain("lab run read");
  });

  test("the run carries the report as stored, the label and the headline", async () => {
    const stored = report();
    put(A, stored);
    const { cookie } = await signedIn("boss", "admin");
    const body = await (await one(req(cookie, `/api/lab/runs/${A}`), ctx(A))).json();
    expect(body.report).toEqual(stored);
    expect(body.report.disclaimer).toBe(stored.disclaimer);
    expect(body.label).toBe(LAB_LABEL);
    expect(body.backtest.proposed.return_pct).toBe(1.8);
    expect(body.backtest.proposed.worst_week_pct).toBe(-0.8);
  });

  test("the L10 gate is closed until counsel's L10 answer is recorded", () => {
    // Opening the gate is a code change in lib/lab.js, after counsel answers L10
    // and the answer is recorded in docs/legal/preliminary-answers.md.
    const doc = fs.readFileSync(path.join(__dirname, "..", "..", "docs", "legal", "preliminary-answers.md"), "utf8");
    const l10 = doc.split("\n").filter((l) => l.includes("**L10**"));
    expect(l10).toHaveLength(1);
    const answered = /counsel'?s answer/i.test(l10[0]);
    if (!answered) expect(L10_ANALYSIS_GATE_OPEN).toBe(false);
  });

  test("with the gate open the analysis is served (the path the L10 answer unlocks)", () => {
    put(A, report());
    put(A, { text: "the analysis" }, "analysis.json");
    expect(readRun(dir, A, true).run.analysis).toEqual({ text: "the analysis" });
    expect(readRun(dir, A, false).run.analysis).toBe(ANALYSIS_WITHHELD);
  });
});

describe("Scenario: path traversal", () => {
  test.each(["../secrets", "..", "..%2Fsecrets", "a/b", `${A}/../x`, "", "2026-10-12", `${A}\n`])(
    "id %j is 404 and no file is read",
    async (id) => {
      fs.writeFileSync(path.join(root, "report.json"), JSON.stringify(report()));
      fs.mkdirSync(path.join(root, "secrets"));
      fs.writeFileSync(path.join(root, "secrets", "report.json"), JSON.stringify(report()));
      const read = jest.spyOn(fs, "readFileSync");
      const { cookie } = await signedIn("mgr", "manager");
      const res = await one(req(cookie, "/api/lab/runs/x"), ctx(id));
      expect(res.status).toBe(404);
      expect(read).not.toHaveBeenCalled();
    },
  );

  test("a valid id with no run is 404; a malformed report is 500 without its content", async () => {
    const { cookie } = await signedIn("mgr", "manager");
    expect((await one(req(cookie, `/api/lab/runs/${A}`), ctx(A))).status).toBe(404);
    put(A, '{"secret": "do-not-echo", broken');
    const res = await one(req(cookie, `/api/lab/runs/${A}`), ctx(A));
    expect(res.status).toBe(500);
    expect(await res.text()).not.toContain("do-not-echo");
    expect(logs.join("\n")).toContain("lab run unreadable");
    expect(logs.join("\n")).not.toContain("do-not-echo");
  });
});

describe("Scenario: read-only", () => {
  test("no lab route accepts POST, PUT, PATCH or DELETE", () => {
    for (const mod of [listModule, oneModule]) {
      expect(typeof mod.GET).toBe("function");
      for (const m of ["POST", "PUT", "PATCH", "DELETE"]) expect(mod[m]).toBeUndefined();
    }
  });

  test("none writes: reading the runs leaves the folder and every file as it was", async () => {
    put(A, report());
    put(A, { text: "x" }, "analysis.json");
    const snapshot = () =>
      fs.readdirSync(dir, { recursive: true }).sort().map((f) => [f, fs.statSync(path.join(dir, f)).isFile() ? fs.readFileSync(path.join(dir, f), "utf8") : ""]);
    const { cookie } = await signedIn("mgr", "manager");
    const before = snapshot();
    const writes = [jest.spyOn(fs, "writeFileSync"), jest.spyOn(fs, "appendFileSync"), jest.spyOn(fs, "mkdirSync"), jest.spyOn(fs, "rmSync")];
    await list(req(cookie, "/api/lab/runs"));
    await one(req(cookie, `/api/lab/runs/${A}`), ctx(A));
    for (const w of writes) expect(w).not.toHaveBeenCalled();
    for (const w of writes) w.mockRestore();
    expect(snapshot()).toEqual(before);
  });
});
