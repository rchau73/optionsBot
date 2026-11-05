/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST } from "@/app/api/lab/requests/route";
import { GET as listRuns } from "@/app/api/lab/runs/route";
import { LAB_LABEL } from "@/lib/lab";
import { checkRequest, createRequest, effectiveStatus, MAX_OPEN_PER_PERSON, MAX_SETTINGS, queueDir, readRequests, STATUS } from "@/lib/labrequests";
import { signedIn, useTestAuth } from "@/test/auth";

// #641: POST /api/lab/requests queues a comparison run of proposed values;
// the runner (cmd/labrun) takes it; GET /api/lab/runs lists it with its status.

const ORIGIN = "http://localhost:3100";
const REPO = path.join(__dirname, "..", "..");
// The request file the Go runner reads (tests/lab_requests_test.go reads it too).
const FIXTURE = JSON.parse(fs.readFileSync(path.join(REPO, "tests", "testdata", "lab_request.json"), "utf8"));
const RUN = "20261105T010000Z";

const req = (cookie, body, headers = {}) =>
  new Request(`${ORIGIN}/api/lab/requests`, {
    method: "POST",
    headers: { origin: ORIGIN, host: "localhost:3100", "content-type": "application/json", ...(cookie ? { cookie } : {}), ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
const getReq = (cookie) => new Request(`${ORIGIN}/api/lab/runs`, { headers: cookie ? { cookie } : {} });

let root, dir, queue;
const queueFiles = () => (fs.existsSync(queue) ? fs.readdirSync(queue).sort() : []);
const everyFile = (d = dir) =>
  fs.existsSync(d) ? fs.readdirSync(d, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? everyFile(path.join(d, e.name)) : [path.relative(dir, path.join(d, e.name))])) : [];

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "lab-requests-"));
  dir = path.join(root, "compare");
  queue = queueDir(dir);
  process.env.COMPARE_DIR = dir;
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
  delete process.env.COMPARE_DIR;
});

async function post(body, username = "mgr", role = "manager") {
  const { cookie, auth } = await signedIn(username, role);
  const record = jest.spyOn(auth, "recordAction");
  const res = await POST(req(cookie, body));
  return { res, json: await res.json(), record };
}

describe("Scenario: a manager requests a run", () => {
  test("a value inside the catalogue range is queued with status queued, and the audit line names the person", async () => {
    const { res, json, record } = await post({ settings: [{ key: "gamma_flip_buffer_sd", value: 1.2 }] });
    expect(res.status).toBe(201);
    expect(json).toEqual(expect.objectContaining({ queued: true, status: STATUS.queued, requester: "mgr", underlying: "BTC" }));
    expect(queueFiles()).toEqual([`${json.id}.json`]);
    const [stored] = readRequests(queue);
    expect(stored).toEqual(expect.objectContaining({ id: json.id, status: "queued", settings: [{ key: "gamma_flip_buffer_sd", value: 1.2 }] }));
    // The keys only, never the values.
    expect(record).toHaveBeenCalledWith(expect.objectContaining({ username: "mgr" }), "lab_request", { id: json.id, underlying: "BTC", keys: ["gamma_flip_buffer_sd"] });
    expect((fs.statSync(path.join(queue, `${json.id}.json`)).mode & 0o777).toString(8)).toBe("600");
  });

  test("the file has the shape the Go runner reads", () => {
    const written = createRequest(queue, "mgr", { underlying: "BTC", settings: [{ key: "gamma_flip_buffer_sd", value: 1.2 }] }, new Date("2026-11-04T23:00:00Z"));
    expect(Object.keys(written).sort()).toEqual(Object.keys(FIXTURE).sort());
    expect(written.id).toMatch(/^20261104T230000Z-[0-9a-f]{8}$/);
    expect({ ...written, id: FIXTURE.id }).toEqual(FIXTURE);
  });

  test("an indexed list key is checked as its catalogue key, an ETH request against the ETH config", () => {
    expect(checkRequest({ settings: [{ key: "dte_delta_matrix[0].dte", value: 30 }] })).toEqual({ underlying: "BTC", settings: [{ key: "dte_delta_matrix[0].dte", value: 30 }] });
    expect(checkRequest({ underlying: "eth", settings: [{ key: "gex_direction", value: "bias" }] }).underlying).toBe("ETH");
  });

  test(`at most ${MAX_OPEN_PER_PERSON} requests waiting per person; another person may still ask`, async () => {
    const body = { settings: [{ key: "roi_take_profit", value: 0.6 }] };
    for (let i = 0; i < MAX_OPEN_PER_PERSON; i++) expect((await post(body)).res.status).toBe(201);
    const { res, json, record } = await post(body);
    expect(res.status).toBe(429);
    expect(json.queued).toBe(false);
    expect(record).toHaveBeenCalledTimes(MAX_OPEN_PER_PERSON); // the earlier requests only (one shared auth)
    expect(queueFiles()).toHaveLength(MAX_OPEN_PER_PERSON);
    expect((await post(body, "other")).res.status).toBe(201);
  });
});

describe("Scenario: an invalid value creates nothing", () => {
  test("a value outside the allowed range gives the reason and writes no request file", async () => {
    const { res, json, record } = await post({ settings: [{ key: "gamma_flip_buffer_sd", value: 3.1 }] });
    expect(res.status).toBe(400);
    expect(json).toEqual(
      expect.objectContaining({ queued: false, key: "gamma_flip_buffer_sd", reasons: ["must be at most 3 daily standard deviations"], error: "gamma_flip_buffer_sd: must be at most 3 daily standard deviations" }),
    );
    expect(record).not.toHaveBeenCalled();
    expect(everyFile()).toEqual([]);
  });

  test.each([
    ["no settings", {}, 400],
    ["an empty list", { settings: [] }, 400],
    ["a cross rule broken", { settings: [{ key: "delta_drift_threshold", value: 0.13 }] }, 400],
    ["a fixed key", { settings: [{ key: "gex_shed_confirm_snapshots", value: 3 }] }, 400],
    ["an unknown key", { settings: [{ key: "no_such_setting", value: 1 }] }, 404],
    ["a key that is not a YAML key", { settings: [{ key: "../config_btc.yaml", value: 1 }] }, 400],
    ["one key twice", { settings: [{ key: "roi_take_profit", value: 0.6 }, { key: "roi_take_profit", value: 0.7 }] }, 400],
    ["too many values", { settings: Array.from({ length: MAX_SETTINGS + 1 }, (_, i) => ({ key: `k${i}`, value: 1 })) }, 400],
    ["an unknown underlying", { underlying: "SOL", settings: [{ key: "roi_take_profit", value: 0.6 }] }, 400],
    ["one bad value among good ones", { settings: [{ key: "roi_take_profit", value: 0.6 }, { key: "repair_cooldown_hours", value: 1.5 }] }, 400],
  ])("%s", async (_, body, status) => {
    const { res, json } = await post(body);
    expect([res.status, json.queued, typeof json.error]).toEqual([status, false, "string"]);
    expect(everyFile()).toEqual([]);
  });

  test("not JSON, too large or cross-origin is refused before anything is read", async () => {
    const { cookie } = await signedIn("mgr", "manager");
    expect((await POST(req(cookie, "{"))).status).toBe(400);
    expect((await POST(req(cookie, "x".repeat(5000)))).status).toBe(413);
    expect((await POST(req(cookie, "{}", { "content-type": "text/plain" }))).status).toBe(415);
    expect((await POST(req(cookie, { settings: [{ key: "roi_take_profit", value: 0.6 }] }, { origin: "https://evil.example" }))).status).toBe(403);
    expect(everyFile()).toEqual([]);
  });
});

describe("Scenario: a user cannot request", () => {
  test("a signed-in user gets 403 and nothing is queued", async () => {
    const { res, record } = await post({ settings: [{ key: "gamma_flip_buffer_sd", value: 1.2 }] }, "viewer", "user");
    expect(res.status).toBe(403);
    expect(record).not.toHaveBeenCalled();
    expect(everyFile()).toEqual([]);
  });

  test("no session gets 401", async () => {
    expect((await POST(req(null, { settings: [{ key: "gamma_flip_buffer_sd", value: 1.2 }] }))).status).toBe(401);
    expect(everyFile()).toEqual([]);
  });
});

// The runner's side (tests/lab_requests_test.go): the request file with the status it writes.
function runnerWrote(over) {
  fs.mkdirSync(queue, { recursive: true });
  const r = { ...FIXTURE, ...over };
  fs.writeFileSync(path.join(queue, `${r.id}.json`), JSON.stringify(r));
  return r;
}

describe("Scenario: the runner finishes a request", () => {
  test("a done request's run is in the runs list with percentages only, labelled backtested; waiting ones are listed with their status", async () => {
    runnerWrote({ status: "done", run_id: RUN, updated_at: "2026-11-05T01:00:00.000Z" });
    const waiting = runnerWrote({ id: "20261105T000000Z-0000000a", requested_at: new Date().toISOString(), updated_at: new Date().toISOString() });
    const running = runnerWrote({ id: "20261105T000500Z-0000000b", status: "running", requested_at: new Date().toISOString() });
    fs.mkdirSync(path.join(dir, RUN));
    fs.writeFileSync(
      path.join(dir, RUN, "report.json"),
      JSON.stringify({
        underlying: "BTC",
        proposed: "config_btc.yaml with gamma_flip_buffer_sd=1.2",
        diffs: [{ key: "gamma_flip_buffer_sd", base: "1.5", proposed: "1.2" }],
        backtest: { ran: true, equity_usd: 100000, base: { return_pct: 1, net_usd: 1000, fees_usd: 100 }, proposed: { return_pct: 1.2, net_usd: 1200, fees_usd: 120 } },
        replay: { ran: false },
      }),
    );
    const { cookie } = await signedIn("mgr", "manager");
    const res = await listRuns(getReq(cookie));
    expect(res.status).toBe(200);
    const { runs, requests } = await res.json();
    expect(runs.map((r) => [r.id, r.label])).toEqual([[RUN, LAB_LABEL]]);
    expect(runs[0].backtest.proposed).toEqual({ return_pct: 1.2, worst_week_pct: null, max_drawdown_pct: null, fees_pct: 0.12 });
    expect(JSON.stringify(runs)).not.toMatch(/net_usd|1200/);
    expect(requests.map((r) => [r.id, r.status])).toEqual([
      [running.id, "running"],
      [waiting.id, "queued"],
    ]);
    expect(requests[1]).toEqual(expect.objectContaining({ requester: "mgr", underlying: "BTC", settings: FIXTURE.settings, run_id: null, reason: null }));
  });

  test("a done request whose report is not readable yet stays in the requests, done with its run id", async () => {
    runnerWrote({ status: "done", run_id: RUN });
    const { cookie } = await signedIn("mgr", "manager");
    const { runs, requests } = await (await listRuns(getReq(cookie))).json();
    expect(runs).toEqual([]);
    expect(requests.map((r) => [r.status, r.run_id])).toEqual([["done", RUN]]);
  });

  test("a user cannot list runs or requests", async () => {
    const { cookie } = await signedIn("viewer", "user");
    expect((await listRuns(getReq(cookie))).status).toBe(403);
  });
});

describe("Scenario: a failed run says so", () => {
  test("its status is failed with the short reason; one queued over a day ago shows expired and no longer counts", async () => {
    runnerWrote({ status: "failed", reason: "no config for SOL (config_sol.yaml)" });
    const old = runnerWrote({ id: "20261101T000000Z-0000000c", requester: "mgr", requested_at: "2026-11-01T00:00:00.000Z" });
    const { cookie } = await signedIn("mgr", "manager");
    const { requests } = await (await listRuns(getReq(cookie))).json();
    expect(requests.map((r) => [r.id, r.status, r.reason])).toEqual([
      [FIXTURE.id, "failed", "no config for SOL (config_sol.yaml)"],
      [old.id, "expired", null],
    ]);
    expect(effectiveStatus(old, Date.parse(old.requested_at) + 24 * 3600 * 1000)).toBe("queued");
    // GET writes nothing: the expired file is the runner's to mark.
    expect(readRequests(queue).find((r) => r.id === old.id).status).toBe("queued");
  });

  test("a half-written or foreign file in the queue is left out", async () => {
    fs.mkdirSync(queue, { recursive: true });
    fs.writeFileSync(path.join(queue, "20261105T000000Z-0000000d.json"), "{");
    fs.writeFileSync(path.join(queue, "notes.json"), "{}");
    expect(readRequests(queue)).toEqual([]);
  });
});

describe("Scenario: nothing is applied", () => {
  test("a request writes its queue file and nothing else: no config, no setting", async () => {
    const configs = ["config_btc.yaml", "config_eth.yaml", "config_btc.mvp.yaml"].map((f) => fs.readFileSync(path.join(REPO, f), "utf8"));
    const { json } = await post({ settings: [{ key: "gamma_flip_buffer_sd", value: 1.2 }, { key: "gex_direction", value: "sma" }] });
    expect(everyFile()).toEqual([path.join("requests", `${json.id}.json`)]);
    expect(["config_btc.yaml", "config_eth.yaml", "config_btc.mvp.yaml"].map((f) => fs.readFileSync(path.join(REPO, f), "utf8"))).toEqual(configs);
  });

  test("no route, queue code or runner source writes a config file", () => {
    for (const file of ["frontend/app/api/lab/requests/route.js", "frontend/lib/labrequests.js", "cmd/labrun/main.go", "internal/compare/requests.go", "scripts/lab_runner.sh"]) {
      const src = fs.readFileSync(path.join(REPO, file), "utf8");
      expect([file, /writeFile\w*\([^)]*config_|WriteFile\([^)]*config_|>\s*config_/.test(src)]).toEqual([file, false]);
    }
  });
});
