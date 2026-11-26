/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as APPLY } from "@/app/api/lab/runs/[id]/apply/route";
import { POST as DROP } from "@/app/api/lab/runs/[id]/drop/route";
import { GET as PREVIEW } from "@/app/api/lab/runs/[id]/preview/route";
import { AUDIT_ACTIONS, parseAuditQuery } from "@/lib/audit";
import { authKey } from "@/lib/auth/tokens";
import { readRun } from "@/lib/lab";
import { DROP_AUDIT_ACTION, droppedDir, NOT_APPLIED, personHash, RECORD_AUDIT_ACTION, recordsDir } from "@/lib/labapply";
import { buildPreview, confirmToken } from "@/lib/labpreview";
import { signedIn, TEST_SECRET, useTestAuth } from "@/test/auth";

// #890: the preview says whether this person may confirm for this account
// (the apply route's own check), and a deliberate drop is recorded once.

const ORIGIN = "http://localhost:3100";
const REPO = path.join(__dirname, "..", "..");
const FILLS = "backtest.slippage_pct"; // BTC's shipped value is 0.001; not a core policy
const figures = { return_pct: 1.8, worst_week_pct: -0.8, max_drawdown_pct: 1.5, fees_usd: 400 };
const report = (over = {}) => ({
  underlying: "BTC",
  diffs: [{ key: FILLS, base: "0.001", proposed: "0.002" }],
  backtest: { ran: true, equity_usd: 100000, base: figures, proposed: figures },
  replay: { ran: false },
  ...over,
});
// The account registry (#655, #760): account A trades BTC, account B ETH.
const REGISTRY = [
  { account_id: "acct-a", label: "Account A", environment: "testnet", bots: [{ currency: "BTC" }] },
  { account_id: "acct-b", label: "Account B", environment: "testnet", bots: [{ currency: "ETH" }] },
];
const OTHER_ACCOUNT = "you do not manage account Account A (acct-a), which bot btc trades for: ask an admin to link it to you on the Users page";

let root, dir, seq;
const newRun = (over) => {
  seq += 1;
  const id = new Date(Date.now() - seq * 1000).toISOString().replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z");
  fs.mkdirSync(path.join(dir, id), { recursive: true });
  fs.writeFileSync(path.join(dir, id, "report.json"), JSON.stringify(report(over)));
  return id;
};
const ctx = (id) => ({ params: Promise.resolve({ id }) });
const preview = (cookie, id) => PREVIEW(new Request(`${ORIGIN}/api/lab/runs/${id}/preview`, { headers: cookie ? { cookie } : {} }), ctx(id));
const postTo = (handler, route) => (cookie, id, body = {}, headers = {}) =>
  handler(
    new Request(`${ORIGIN}/api/lab/runs/${id}/${route}`, {
      method: "POST",
      headers: { origin: ORIGIN, host: "localhost:3100", "content-type": "application/json", ...(cookie ? { cookie } : {}), ...headers },
      body: JSON.stringify(body),
    }),
    ctx(id),
  );
const drop = postTo(DROP, "drop");
const apply = postTo(APPLY, "apply");
const tokenOf = (id, person) => confirmToken(buildPreview(readRun(dir, id).run, person, Date.now()), authKey(TEST_SECRET), Date.now());
const files = (d) => (fs.existsSync(d) ? fs.readdirSync(d) : []);
const dropFile = (id) => path.join(droppedDir(dir), `${id}.json`);

/**
 * A person of `role` signed in, linked by an admin to `links` (null = no link),
 * with the audit spied: { cookie, record, username }.
 */
async function person(username, role, links = null) {
  const { cookie, auth } = await signedIn(username, role);
  if (links) {
    const { user: admin } = await signedIn("boss", "admin");
    expect(auth.setAccountLinks(admin, auth.store.userByName(username).id, links, REGISTRY)).toMatchObject({ ok: true });
  }
  return { cookie, username, record: jest.spyOn(auth, "recordAction") };
}
const linesOf = (record, action) => record.mock.calls.filter((c) => c[1] === action);

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "lab-drop-"));
  dir = path.join(root, "compare");
  fs.mkdirSync(dir);
  process.env.COMPARE_DIR = dir;
  process.env.ACCOUNTS_STATUS_PATH = path.join(root, "accounts_status.json");
  fs.writeFileSync(process.env.ACCOUNTS_STATUS_PATH, JSON.stringify({ version: 1, accounts: REGISTRY }));
  seq = 0;
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
  delete process.env.COMPARE_DIR;
  delete process.env.ACCOUNTS_STATUS_PATH;
});

describe("Scenario: a linked manager previews a valid change", () => {
  test("may_confirm is true, no reason, and confirm_token is present", async () => {
    const id = newRun();
    const { cookie } = await person("ana", "manager", ["acct-a"]);
    const res = await preview(cookie, id);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body).toMatchObject({ may_confirm: true, may_confirm_reason: null, confirmable: true });
    expect(body.confirm_token).toMatch(/^[0-9a-f]{64}$/);
  });
});

describe("Scenario: a manager not linked to the account previews it", () => {
  test("may_confirm is false with the plain-words reason, no token, and apply refuses with the same reason", async () => {
    const id = newRun();
    const { cookie, record } = await person("ana", "manager", ["acct-b"]);
    const body = await (await preview(cookie, id)).json();
    expect(body).toMatchObject({ may_confirm: false, may_confirm_reason: OTHER_ACCOUNT, confirm_token: null });
    // The run itself is confirmable: only this person may not.
    expect(body.confirmable).toBe(true);

    const res = await apply(cookie, id, { confirm_token: tokenOf(id, "ana") });
    expect(res.status).toBe(403);
    expect((await res.json()).error).toBe(body.may_confirm_reason);
    expect(files(recordsDir(dir))).toEqual([]);
    expect(linesOf(record, RECORD_AUDIT_ACTION)).toEqual([]);
  });
});

describe("Scenario: a user previews", () => {
  test("the route refuses as before and nothing is written", async () => {
    const id = newRun();
    const { cookie, record } = await person("viewer", "user");
    expect((await preview(cookie, id)).status).toBe(403);
    expect(record).not.toHaveBeenCalled();
    expect(files(recordsDir(dir))).toEqual([]);
    expect(files(droppedDir(dir))).toEqual([]);
  });
});

describe("the role × link matrix: preview, apply and drop agree", () => {
  // [who, role, links, allowed]: allowed means may_confirm true, apply 201, drop 201.
  const CASES = [
    ["an admin", "admin", null, true],
    ["a manager with no link", "manager", null, true],
    ["a manager linked to the run's account", "manager", ["acct-a"], true],
    ["a manager linked to both accounts", "manager", ["acct-a", "acct-b"], true],
    ["a manager linked only to another account", "manager", ["acct-b"], false],
  ];
  test.each(CASES)("%s", async (_, role, links, allowed) => {
    const p = await person(`p-${role}-${(links ?? []).join("-") || "none"}`, role, links);
    const id = newRun();
    const body = await (await preview(p.cookie, id)).json();
    expect(body.may_confirm).toBe(allowed);
    expect(body.confirm_token === null).toBe(!allowed);

    const applied = await apply(p.cookie, id, { confirm_token: tokenOf(id, p.username) });
    expect(applied.status).toBe(allowed ? 201 : 403);
    if (!allowed) expect((await applied.json()).error).toBe(body.may_confirm_reason);

    const other = newRun();
    const dropped = await drop(p.cookie, other);
    expect(dropped.status).toBe(allowed ? 201 : 403);
    if (!allowed) {
      expect((await dropped.json()).error).toBe(body.may_confirm_reason);
      expect(linesOf(p.record, DROP_AUDIT_ACTION)).toEqual([]);
    }
  });

  test("a user and no session are refused on all three, and nothing is written", async () => {
    const id = newRun();
    const u = await person("viewer", "user");
    for (const cookie of [u.cookie, null]) {
      const want = cookie ? 403 : 401;
      expect((await preview(cookie, id)).status).toBe(want);
      expect((await apply(cookie, id, { confirm_token: tokenOf(id, "viewer") })).status).toBe(want);
      expect((await drop(cookie, id)).status).toBe(want);
    }
    expect(u.record).not.toHaveBeenCalled();
    expect(files(recordsDir(dir))).toEqual([]);
    expect(files(droppedDir(dir))).toEqual([]);
  });

  test("a linked manager is refused when the monitor cannot tell the run's account (no registry file)", async () => {
    const id = newRun();
    const p = await person("ana", "manager", ["acct-a"]);
    fs.rmSync(process.env.ACCOUNTS_STATUS_PATH);
    const body = await (await preview(p.cookie, id)).json();
    expect(body.may_confirm).toBe(false);
    expect(body.may_confirm_reason).toMatch(/cannot tell which account bot btc trades for/);
    const res = await drop(p.cookie, id);
    expect(res.status).toBe(403);
    expect((await res.json()).error).toBe(body.may_confirm_reason);
  });
});

describe("Scenario: a manager drops a proposal", () => {
  test("the record exists once, one audit line names the person, account and run; nothing applied", async () => {
    const id = newRun();
    const { cookie, record } = await person("ana", "manager", ["acct-a"]);
    const configs = ["config_btc.yaml", "config_btc.mvp.yaml", "config_eth.yaml"].map((f) => path.join(REPO, f));
    const before = configs.map((f) => fs.readFileSync(f, "utf8"));
    const fetchSpy = jest.spyOn(global, "fetch").mockImplementation(async () => Response.json({}));

    const res = await drop(cookie, id);
    expect(res.status).toBe(201);
    expect(res.headers.get("Cache-Control")).toBe("no-store");
    const body = await res.json();
    expect(body).toMatchObject({ applied: false, dropped: true, run_id: id, person: personHash("ana"), account: { underlying: "BTC", config: "config_btc.yaml" } });
    expect(body.message).toContain(NOT_APPLIED);

    expect(files(droppedDir(dir))).toEqual([`${id}.json`]);
    const stored = JSON.parse(fs.readFileSync(dropFile(id), "utf8"));
    expect(stored).toEqual({
      run_id: id,
      dropped_at: expect.any(String),
      person: personHash("ana"),
      role: "manager",
      account: { underlying: "BTC", config: "config_btc.yaml" },
      applied: false,
      dropped: true,
    });
    expect(stored.person).toMatch(/^[0-9a-f]{8}$/);
    // No name, no setting value, no amount in the record.
    const text = JSON.stringify(stored);
    for (const leak of ['"ana"', FILLS, "0.002", "0.001", "usd"]) expect([leak, text.includes(leak)]).toEqual([leak, false]);
    expect(fs.statSync(dropFile(id)).mode & 0o777).toBe(0o600);

    const line = `parameter change dropped: ana, account BTC (config_btc.yaml), run ${id}; nothing applied to a running bot`;
    expect(linesOf(record, DROP_AUDIT_ACTION)).toEqual([
      [expect.objectContaining({ username: "ana" }), DROP_AUDIT_ACTION, { run_id: id, underlying: "BTC", config: "config_btc.yaml", applied: false, line }],
    ]);
    expect(record).toHaveBeenCalledTimes(1);
    expect(files(recordsDir(dir))).toEqual([]);
    expect(configs.map((f) => fs.readFileSync(f, "utf8"))).toEqual(before);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  test("the cmd/labstats hash of a person is the same 8 characters", () => {
    // sha256("ana")[:8], as compare.ManagerHash computes it (tests/lab_stats_test.go pins the Go side).
    expect(personHash("ana")).toBe("24d4b96f");
  });

  test("the Audit page knows the action: listed and accepted as a filter", () => {
    expect(AUDIT_ACTIONS).toContain(DROP_AUDIT_ACTION);
    expect(parseAuditQuery(new URLSearchParams({ action: DROP_AUDIT_ACTION }))).toMatchObject({ ok: true, query: { action: DROP_AUDIT_ACTION } });
  });

  test("an audit line that cannot be written takes the record back, so the drop can be retried", async () => {
    const id = newRun();
    const { cookie, record } = await person("ana", "manager");
    record.mockImplementationOnce(() => {
      throw new Error("disk full");
    });
    expect((await drop(cookie, id)).status).toBe(500);
    expect(files(droppedDir(dir))).toEqual([]);
    expect((await drop(cookie, id)).status).toBe(201);
    expect(files(droppedDir(dir))).toEqual([`${id}.json`]);
  });
});

describe("Scenario: drop twice, or drop an applied run", () => {
  test("a second drop says it is already recorded and writes nothing new", async () => {
    const id = newRun();
    const { cookie, record } = await person("ana", "manager");
    expect((await drop(cookie, id)).status).toBe(201);
    const first = fs.readFileSync(dropFile(id), "utf8");
    const res = await drop(cookie, id);
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body).toMatchObject({ applied: false, dropped: false });
    expect(body.error).toBe(`this run's proposal was already dropped by ${personHash("ana")} at ${JSON.parse(first).dropped_at}: a drop is recorded once`);
    expect(fs.readFileSync(dropFile(id), "utf8")).toBe(first);
    expect(linesOf(record, DROP_AUDIT_ACTION)).toHaveLength(1);
  });

  test("a run with a recorded change cannot be dropped", async () => {
    const id = newRun();
    const { cookie, record } = await person("ana", "manager");
    expect((await apply(cookie, id, { confirm_token: tokenOf(id, "ana") })).status).toBe(201);
    const res = await drop(cookie, id);
    expect(res.status).toBe(409);
    expect((await res.json()).error).toMatch(/^this run's change was already recorded by ana at .+: a confirmed run cannot be dropped$/);
    expect(files(droppedDir(dir))).toEqual([]);
    expect(linesOf(record, DROP_AUDIT_ACTION)).toEqual([]);
  });

  test("a dropped run cannot be confirmed", async () => {
    const id = newRun();
    const { cookie, record } = await person("ana", "manager");
    expect((await drop(cookie, id)).status).toBe(201);
    const res = await apply(cookie, id, { confirm_token: tokenOf(id, "ana") });
    expect(res.status).toBe(409);
    expect((await res.json()).error).toMatch(/^this run's proposal was dropped by [0-9a-f]{8} at .+: a dropped proposal cannot be confirmed$/);
    expect(files(recordsDir(dir))).toEqual([]);
    expect(linesOf(record, RECORD_AUDIT_ACTION)).toEqual([]);
  });

  test.each([
    ["a run that changed nothing", () => newRun({ diffs: [] }), 409, "there is no proposal to drop"],
    ["a run whose request is still running", () => {
      const id = newRun();
      fs.mkdirSync(path.join(dir, "requests"), { recursive: true });
      const reqId = "20261106T120000Z-0123abcd";
      fs.writeFileSync(path.join(dir, "requests", `${reqId}.json`), JSON.stringify({ id: reqId, status: "running", run_id: id }));
      return id;
    }, 409, "the run is not finished (its request is running): wait until it is done"],
    ["an id that is not a run", () => "../secrets", 404, "no such run"],
    ["no such run", () => "20261106T120000Z", 404, "no such run"],
  ])("%s is refused and writes nothing", async (_, make, status, reason) => {
    const id = make();
    const { cookie, record } = await person("ana", "manager");
    const res = await drop(cookie, id);
    expect(res.status).toBe(status);
    expect((await res.json()).error).toContain(reason);
    expect(files(droppedDir(dir))).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });

  test.each([
    ["cross-origin", { origin: "http://evil.example" }, 403],
    ["not JSON", { "content-type": "text/plain" }, 415],
  ])("%s is refused", async (_, headers, status) => {
    const id = newRun();
    const { cookie, record } = await person("ana", "manager");
    expect((await drop(cookie, id, {}, headers)).status).toBe(status);
    expect(files(droppedDir(dir))).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });

  test("a body over 1 KB is refused", async () => {
    const id = newRun();
    const { cookie } = await person("ana", "manager");
    expect((await drop(cookie, id, { pad: "a".repeat(2000) })).status).toBe(413);
  });
});
