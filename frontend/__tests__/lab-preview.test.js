/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import * as previewModule from "@/app/api/lab/runs/[id]/preview/route";
import { ANALYSIS_WITHHELD, LAB_LABEL, readRun } from "@/lib/lab";
import { AUDIT_ACTIONS, parseAuditQuery } from "@/lib/audit";
import { buildPreview, confirmToken, NOTHING_APPLIED, PREVIEW_AUDIT_ACTION, PREVIEW_MAX_AGE_DAYS } from "@/lib/labpreview";
import { signedIn, useTestAuth } from "@/test/auth";

// #682: the apply preview, read-only on the configuration.

const { GET } = previewModule;
const DAY = 24 * 3600 * 1000;
const FILLS = "backtest.slippage_pct"; // BTC's shipped value is 0.001; not a core policy
const CORE = "gamma_flip_buffer_sd"; // BTC's shipped value is 1.5
const REPO = path.join(__dirname, "..", "..");
const RUN = "20261106T120000Z";
const RUN_MS = Date.parse("2026-11-06T12:00:00Z");

const figures = { return_pct: 1.8, worst_week_pct: -0.8, max_drawdown_pct: 1.5, fees_usd: 400 };
const diff = (key, base, proposed) => ({ key, base, proposed });
const report = (over = {}) => ({
  underlying: "BTC",
  diffs: [diff(FILLS, "0.001", "0.002")],
  backtest: { ran: true, equity_usd: 100000, base: figures, proposed: figures },
  replay: { ran: false },
  stress: { ran: true },
  ...over,
});

/** A run id a moment old: a report folder's time. */
const freshId = () => new Date().toISOString().replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z");

let root, dir, logs;
const put = (id, content, file = "report.json") => {
  fs.mkdirSync(path.join(dir, id), { recursive: true });
  fs.writeFileSync(path.join(dir, id, file), typeof content === "string" ? content : JSON.stringify(content));
};
const ask = (cookie, id) => GET(new Request(`http://monitor/api/lab/runs/${id}/preview`, { headers: cookie ? { cookie } : {} }), { params: Promise.resolve({ id }) });
const previewOf = (over, now = RUN_MS + DAY) => {
  put(RUN, report(over));
  return buildPreview(readRun(dir, RUN).run, "mgr", now);
};

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "lab-preview-"));
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

describe("Scenario: manager previews a non-core change", () => {
  test("old and new value, the account and the numbers labelled; a token; nothing changes", async () => {
    const id = freshId();
    const cliffs = [{ source: "backtest", figure: "worst_week_pct", detail: "a cliff" }];
    put(id, report({ neighbours: { key: FILLS, cliffs } }));
    const configs = ["config_btc.yaml", "config_btc.mvp.yaml", "config_eth.yaml"].map((f) => path.join(REPO, f));
    const before = configs.map((f) => fs.readFileSync(f, "utf8"));
    const { cookie } = await signedIn("mgr", "manager");
    const writes = [jest.spyOn(fs, "writeFileSync"), jest.spyOn(fs, "rmSync"), jest.spyOn(fs, "renameSync")];
    const res = await ask(cookie, id);
    expect(res.status).toBe(200);
    expect(res.headers.get("Cache-Control")).toBe("no-store");
    const body = await res.json();
    expect(body.changes).toEqual([expect.objectContaining({ key: FILLS, current: 0.001, base: "0.001", proposed: "0.002", core_policy: false, policy_note: null })]);
    expect(body.changes[0].title).toBeTruthy();
    expect(body.account).toEqual({ underlying: "BTC", config: "config_btc.yaml" });
    expect(body.person).toBe("mgr");
    expect(body.run_id).toBe(id);
    expect(body.backtested.label).toBe(LAB_LABEL);
    expect(body.backtested.backtest.proposed.return_pct).toBe(1.8);
    expect(body.fragility).toEqual({ checked: true, key: FILLS, cliffs, note: null });
    expect(body).toMatchObject({ needs_product_owner: false, stale: false, confirmable: true, reasons: [], applied: NOTHING_APPLIED });
    expect(body.confirm_token).toMatch(/^[0-9a-f]{64}$/);
    for (const w of writes) expect(w).not.toHaveBeenCalled();
    expect(configs.map((f) => fs.readFileSync(f, "utf8"))).toEqual(before);
    expect(fs.readdirSync(dir)).toEqual([id]);
  });

  test("the route exports GET and no state-changing method", () => {
    for (const m of ["POST", "PUT", "PATCH", "DELETE"]) expect(previewModule[m]).toBeUndefined();
  });

  test("a run without the fragility check says so", () => {
    expect(previewOf({}).fragility).toEqual({ checked: false, key: null, cliffs: null, note: "no fragility check was run for this setting" });
    expect(previewOf({ neighbours: { key: FILLS, cliffs: null } }).fragility.cliffs).toEqual([]);
  });

  test("the AI analysis stays withheld and its file is not served", async () => {
    const id = freshId();
    put(id, report());
    put(id, { text: "SECRET-ANALYSIS" }, "analysis.json");
    const { cookie } = await signedIn("mgr", "manager");
    const text = await (await ask(cookie, id)).text();
    expect(text).not.toContain("SECRET-ANALYSIS");
    expect(JSON.parse(text).analysis).toBe(ANALYSIS_WITHHELD);
  });
});

describe("Scenario: core policy", () => {
  test("the answer says a product owner approval is needed and holds no token", async () => {
    const id = freshId();
    put(id, report({ diffs: [diff(CORE, "1.5", "1.2")] }));
    const { cookie } = await signedIn("mgr", "manager");
    const body = await (await ask(cookie, id)).json();
    expect(body).toMatchObject({ needs_product_owner: true, confirm_token: null, confirmable: false, stale: false });
    expect(body.changes[0]).toMatchObject({ key: CORE, current: 1.5, proposed: "1.2", core_policy: true, policy_note: "changing the default needs the product owner" });
    expect(body.reasons.join(" ")).toContain("needs the product owner");
  });

  test("one core setting among others blocks the whole run", () => {
    const p = previewOf({ diffs: [diff(FILLS, "0.001", "0.002"), diff(CORE, "1.5", "1.2")] });
    expect(p.needs_product_owner).toBe(true);
    expect(p.confirmable).toBe(false);
  });
});

describe("Scenario: stale run", () => {
  test("a current value that no longer matches: stale, no token", async () => {
    const id = freshId();
    put(id, report({ diffs: [diff(FILLS, "0.005", "0.002")] }));
    const { cookie } = await signedIn("mgr", "manager");
    const body = await (await ask(cookie, id)).json();
    expect(body).toMatchObject({ stale: true, confirm_token: null });
    expect(body.reasons).toEqual([`${FILLS}: the run started from 0.005, the account now has 0.001`]);
  });

  test("a run older than the limit: stale, no token", async () => {
    const id = "20200101T000000Z";
    put(id, report());
    const { cookie } = await signedIn("mgr", "manager");
    const body = await (await ask(cookie, id)).json();
    expect(body).toMatchObject({ stale: true, confirm_token: null, reasons: [`the run is older than ${PREVIEW_MAX_AGE_DAYS} days`] });
  });

  test.each([
    ["exactly at the limit", PREVIEW_MAX_AGE_DAYS * DAY, false],
    ["one second past it", PREVIEW_MAX_AGE_DAYS * DAY + 1000, true],
  ])("a run %s", (_, age, stale) => {
    const p = previewOf({}, RUN_MS + age);
    expect(p.stale).toBe(stale);
    expect(p.confirmable).toBe(!stale);
  });

  test("the same number written differently is not stale", () => {
    expect(previewOf({ diffs: [diff(FILLS, "0.0010", "0.002")] }).stale).toBe(false);
  });

  test.each([
    ["a key of no setting", [diff("no_such_key", "1", "2")]],
    ["a key fixed in code", [diff("entry_delta", "1", "2")]],
    ["a list key whose element cannot be told", [diff("dte_delta_matrix[0].deltas[0]", "0.16", "0.14")]],
    ["an index past the list", [diff("dte_delta_matrix[9].dte", "25", "30")]],
    ["a value the base did not have", [diff(FILLS, "—", "0.002")]],
    ["a proposed value now refused", [diff(FILLS, "0.001", "not a number")]],
  ])("%s fails closed", (_, diffs) => {
    const p = previewOf({ diffs });
    expect(p.stale).toBe(true);
    expect(p.confirmable).toBe(false);
  });

  test("an unknown account fails closed", () => {
    const p = previewOf({ underlying: "DOGE" });
    expect(p.account).toEqual({ underlying: "DOGE", config: null });
    expect(p.confirmable).toBe(false);
  });

  test("a list element is read by its index", () => {
    const p = previewOf({ diffs: [diff("dte_delta_matrix[1].dte", "50", "45")] });
    expect(p.changes[0].current).toBe(50);
    expect(p.stale).toBe(false);
    expect(p.needs_product_owner).toBe(true);
  });

  test("a run with no numbers for the proposal is not confirmable, and not called stale", () => {
    const p = previewOf({ backtest: { ran: true, equity_usd: 1, base: figures, proposed: { error: "boom" } } });
    expect(p.stale).toBe(false);
    expect(p.confirmable).toBe(false);
    expect(p.reasons).toEqual(["the run has no backtested numbers for the proposed value"]);
  });
});

describe("Scenario: wrong role", () => {
  test("403 for the role user, 401 without a session, no audit line", async () => {
    const id = freshId();
    put(id, report());
    const { cookie, auth } = await signedIn("viewer", "user");
    const record = jest.spyOn(auth, "recordAction");
    expect((await ask(cookie, id)).status).toBe(403);
    expect((await ask(null, id)).status).toBe(401);
    expect(record).not.toHaveBeenCalled();
  });
});

describe("Scenario: audit", () => {
  test("one line naming the person, the account, the run and both values", async () => {
    const id = freshId();
    put(id, report());
    const { cookie, auth } = await signedIn("mgr", "manager");
    const record = jest.spyOn(auth, "recordAction");
    const body = await (await ask(cookie, id)).json();
    const line = `parameter change previewed: mgr, account BTC (config_btc.yaml), run ${id}, ${FILLS} 0.001 → 0.002`;
    expect(body.audit_line).toBe(line);
    expect(record).toHaveBeenCalledTimes(1);
    expect(record).toHaveBeenCalledWith(expect.objectContaining({ username: "mgr" }), "parameter_change_previewed", {
      run_id: id,
      underlying: "BTC",
      config: "config_btc.yaml",
      changes: [{ key: FILLS, current: 0.001, proposed: "0.002" }],
      needs_product_owner: false,
      stale: false,
      line,
    });
    expect(JSON.stringify(record.mock.calls)).not.toContain(body.confirm_token);
    expect(logs.join("\n")).toContain("lab apply previewed");
  });

  test("the Audit page knows the action: listed and accepted as a filter", () => {
    expect(AUDIT_ACTIONS).toContain(PREVIEW_AUDIT_ACTION);
    const parsed = parseAuditQuery(new URLSearchParams({ action: PREVIEW_AUDIT_ACTION }));
    expect(parsed).toMatchObject({ ok: true, query: { action: PREVIEW_AUDIT_ACTION } });
  });

  test.each([
    ["an id that is not a run", "../secrets", null, 404],
    ["no such run", RUN, null, 404],
    ["a malformed report", RUN, "{ broken", 500],
    ["a run that changed nothing", RUN, JSON.stringify(report({ diffs: [] })), 409],
  ])("%s writes no audit line", async (_, id, content, status) => {
    if (content) put(id, content);
    const { cookie, auth } = await signedIn("mgr", "manager");
    const record = jest.spyOn(auth, "recordAction");
    expect((await ask(cookie, id)).status).toBe(status);
    expect(record).not.toHaveBeenCalled();
  });
});

describe("the confirm token", () => {
  const KEY = "k".repeat(32);

  test("depends on the secret, the person, each value and the time window", () => {
    const p = previewOf({});
    const token = confirmToken(p, KEY, RUN_MS);
    expect(confirmToken(p, KEY, RUN_MS)).toBe(token);
    expect(confirmToken(p, "j".repeat(32), RUN_MS)).not.toBe(token);
    expect(confirmToken({ ...p, person: "other" }, KEY, RUN_MS)).not.toBe(token);
    expect(confirmToken(previewOf({ diffs: [diff(FILLS, "0.001", "0.003")] }), KEY, RUN_MS)).not.toBe(token);
    expect(confirmToken(p, KEY, RUN_MS + 5 * 60 * 1000)).not.toBe(token);
  });
});
