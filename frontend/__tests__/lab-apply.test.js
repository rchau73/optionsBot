/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST } from "@/app/api/lab/runs/[id]/apply/route";
import { GET as PREVIEW } from "@/app/api/lab/runs/[id]/preview/route";
import { AUDIT_ACTIONS, parseAuditQuery } from "@/lib/audit";
import { authKey } from "@/lib/auth/tokens";
import { LAB_LABEL, readRun } from "@/lib/lab";
import { APPROVER, NEXT_STEP, NOT_APPLIED, RECORD_AUDIT_ACTION, recordsDir, tokenId, writeRecord } from "@/lib/labapply";
import { buildPreview, CONFIRM_TOKEN_MINUTES, confirmToken, confirmTokenMatches } from "@/lib/labpreview";
import { signedIn, TEST_SECRET, useTestAuth } from "@/test/auth";

// #741: a manager's confirm records the change the preview showed, and applies nothing.

const ORIGIN = "http://localhost:3100";
const REPO = path.join(__dirname, "..", "..");
const MIN = 60 * 1000;
const FILLS = "backtest.slippage_pct"; // BTC's shipped value is 0.001; not a core policy
const CORE = "gamma_flip_buffer_sd"; // BTC's shipped value is 1.5

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

let root, dir, logs, seq;
/** A run id a few seconds old and unique in the test: a report folder's time. */
const freshId = () => {
  seq += 1;
  return new Date(Date.now() - seq * 1000).toISOString().replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z");
};
const put = (id, content) => {
  fs.mkdirSync(path.join(dir, id), { recursive: true });
  fs.writeFileSync(path.join(dir, id, "report.json"), typeof content === "string" ? content : JSON.stringify(content));
};
const newRun = (over) => {
  const id = freshId();
  put(id, report(over));
  return id;
};
/** The token a preview of run `id` gives `person` at `now`, whether or not the route would show it. */
const tokenOf = (id, person = "mgr", now = Date.now()) => confirmToken(buildPreview(readRun(dir, id).run, person, now), authKey(TEST_SECRET), now);
const post = (cookie, id, body, headers = {}) =>
  POST(
    new Request(`${ORIGIN}/api/lab/runs/${id}/apply`, {
      method: "POST",
      headers: { origin: ORIGIN, host: "localhost:3100", "content-type": "application/json", ...(cookie ? { cookie } : {}), ...headers },
      body: typeof body === "string" ? body : JSON.stringify(body),
    }),
    { params: Promise.resolve({ id }) },
  );
const recordFile = (id) => path.join(recordsDir(dir), `${id}.json`);
const records = () => (fs.existsSync(recordsDir(dir)) ? fs.readdirSync(recordsDir(dir)) : []);

/** A manager signed in, with the audit spied: { cookie, record }. */
async function manager(name = "mgr", role = "manager") {
  const { cookie, auth } = await signedIn(name, role);
  return { cookie, record: jest.spyOn(auth, "recordAction") };
}
const changeLines = (record) => record.mock.calls.filter((c) => c[1] === RECORD_AUDIT_ACTION);

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "lab-apply-"));
  dir = path.join(root, "compare");
  fs.mkdirSync(dir);
  process.env.COMPARE_DIR = dir;
  seq = 0;
  logs = [];
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  useTestAuth();
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(root, { recursive: true, force: true });
  delete process.env.COMPARE_DIR;
  delete process.env.ACCOUNTS_STATUS_PATH;
});

describe("Scenario: A manager confirms an allowed change", () => {
  test("the preview's token records the change, says nothing was applied and writes one audit line", async () => {
    const id = newRun({ neighbours: { key: FILLS, cliffs: [] } });
    const { cookie, record } = await manager();
    const preview = await (await PREVIEW(new Request(`${ORIGIN}/api/lab/runs/${id}/preview`, { headers: { cookie } }), { params: Promise.resolve({ id }) })).json();
    expect(preview.confirm_token).toMatch(/^[0-9a-f]{64}$/);
    const configs = ["config_btc.yaml", "config_btc.mvp.yaml", "config_eth.yaml"].map((f) => path.join(REPO, f));
    const before = configs.map((f) => fs.readFileSync(f, "utf8"));
    const fetchSpy = jest.spyOn(global, "fetch").mockImplementation(async () => Response.json({}));

    const res = await post(cookie, id, { confirm_token: preview.confirm_token });
    expect(res.status).toBe(201);
    expect(res.headers.get("Cache-Control")).toBe("no-store");
    const body = await res.json();
    expect(body).toMatchObject({ applied: false, recorded: true, run_id: id, person: "mgr", next_step: NEXT_STEP, token_id: tokenId(preview.confirm_token) });
    expect(body.account).toEqual({ underlying: "BTC", config: "config_btc.yaml" });
    expect(body.changes).toEqual([{ key: FILLS, title: expect.any(String), old: 0.001, new: "0.002" }]);
    expect(body.message).toContain(`The result was ${LAB_LABEL}`);
    expect(body.message).toContain(NOT_APPLIED);
    // The risks sit next to the numbers, in percentages only, never an amount.
    expect(body.backtested.label).toBe(LAB_LABEL);
    expect(body.risks).toEqual([
      "backtest: worst week -0.80 %, largest drawdown 1.50 %, fees 0.40 % of the starting equity",
      "past data only: the market can move in ways these days did not show",
    ]);
    expect(JSON.stringify(body)).not.toMatch(/usd|\$/i);

    const line = `parameter change recorded: mgr, account BTC (config_btc.yaml), run ${id}, ${FILLS} 0.001 → 0.002, token ${body.token_id}; nothing applied to a running bot`;
    expect(body.audit_line).toBe(line);
    expect(changeLines(record)).toEqual([
      [
        expect.objectContaining({ username: "mgr" }),
        RECORD_AUDIT_ACTION,
        { run_id: id, underlying: "BTC", config: "config_btc.yaml", changes: [{ key: FILLS, old: 0.001, new: "0.002" }], token_id: body.token_id, applied: false, line },
      ],
    ]);
    expect(JSON.stringify(record.mock.calls)).not.toContain(preview.confirm_token);

    const stored = JSON.parse(fs.readFileSync(recordFile(id), "utf8"));
    expect(stored).toMatchObject({ run_id: id, person: "mgr", role: "manager", applied: false, recorded: true, token_id: body.token_id, line });
    expect(JSON.stringify(stored)).not.toContain(preview.confirm_token);
    expect(fs.statSync(recordFile(id)).mode & 0o777).toBe(0o600);
    expect(configs.map((f) => fs.readFileSync(f, "utf8"))).toEqual(before);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(logs.join("\n")).toContain("lab change recorded");
  });

  test("an admin may confirm too", async () => {
    const id = newRun();
    const { cookie } = await manager("boss", "admin");
    expect((await post(cookie, id, { confirm_token: tokenOf(id, "boss") })).status).toBe(201);
  });

  test("the Audit page knows the action: listed and accepted as a filter", () => {
    expect(AUDIT_ACTIONS).toContain(RECORD_AUDIT_ACTION);
    expect(parseAuditQuery(new URLSearchParams({ action: RECORD_AUDIT_ACTION }))).toMatchObject({ ok: true, query: { action: RECORD_AUDIT_ACTION } });
  });
});

describe("Scenario: A token is used once", () => {
  test("a second confirm of the same token is refused in plain words and writes no second record", async () => {
    const id = newRun();
    const { cookie, record } = await manager();
    const token = tokenOf(id);
    expect((await post(cookie, id, { confirm_token: token })).status).toBe(201);
    const first = fs.readFileSync(recordFile(id), "utf8");
    const res = await post(cookie, id, { confirm_token: token });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body).toMatchObject({ applied: false, recorded: false });
    expect(body.error).toMatch(/^this run's change was already recorded by mgr at .+: a confirm is used once$/);
    expect(fs.readFileSync(recordFile(id), "utf8")).toBe(first);
    expect(records()).toEqual([`${id}.json`]);
    expect(changeLines(record)).toHaveLength(1);
  });

  test("two confirms that race past the check still record once", () => {
    const rec = { run_id: "20261106T120000Z", person: "mgr" };
    writeRecord(recordsDir(dir), rec);
    expect(() => writeRecord(recordsDir(dir), { ...rec, person: "other" })).toThrow(expect.objectContaining({ code: "EEXIST" }));
    expect(JSON.parse(fs.readFileSync(recordFile(rec.run_id), "utf8")).person).toBe("mgr");
  });

  test("an audit line that cannot be written takes the record back, so the confirm can be retried", async () => {
    const id = newRun();
    const { cookie, record } = await manager();
    const token = tokenOf(id);
    record.mockImplementationOnce(() => {
      throw new Error("disk full");
    });
    const res = await post(cookie, id, { confirm_token: token });
    expect(res.status).toBe(500);
    expect(await res.json()).toMatchObject({ applied: false, recorded: false });
    expect(records()).toEqual([]);
    expect(logs.join("\n")).toContain(tokenId(token));
    expect((await post(cookie, id, { confirm_token: token })).status).toBe(201);
    expect(records()).toEqual([`${id}.json`]);
  });
});

describe("Scenario: A core policy is blocked", () => {
  test("refused, naming the product owner, even with a token computed for it", async () => {
    const id = newRun({ diffs: [diff(CORE, "1.5", "1.2")] });
    const { cookie, record } = await manager();
    const res = await post(cookie, id, { confirm_token: tokenOf(id) });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body).toMatchObject({ approver: APPROVER, applied: false, recorded: false });
    expect(body.error).toBe(`${CORE}: a core policy, so a new default needs the product owner, not a manager's confirm`);
    expect(records()).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });
});

describe("Scenario: A user, or a manager of another account, is refused", () => {
  test("403 for the role user, 401 without a session; no record, no audit line", async () => {
    const id = newRun();
    const { cookie, record } = await manager("viewer", "user");
    expect((await post(cookie, id, { confirm_token: tokenOf(id, "viewer") })).status).toBe(403);
    expect((await post(null, id, { confirm_token: tokenOf(id) })).status).toBe(401);
    expect(records()).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });

  test("another manager posting someone else's token is refused: the token names the person it was shown to", async () => {
    const id = newRun();
    const { cookie, record } = await manager("mgr2");
    const res = await post(cookie, id, { confirm_token: tokenOf(id, "mgr") });
    expect(res.status).toBe(409);
    expect((await res.json()).error).toContain("does not match this run, your username and these values");
    expect(records()).toEqual([]);
    expect(changeLines(record)).toEqual([]);
  });

  /** Manager `name` linked by an admin to `accounts` of the registry (BTC → acct-a, ETH → acct-b, #760). */
  async function linkedManager(name, accounts) {
    const registry = [
      { account_id: "acct-a", label: "Account A", environment: "testnet", bots: [{ currency: "BTC" }] },
      { account_id: "acct-b", label: "Account B", environment: "testnet", bots: [{ currency: "ETH" }] },
    ];
    process.env.ACCOUNTS_STATUS_PATH = path.join(root, "accounts_status.json");
    fs.writeFileSync(process.env.ACCOUNTS_STATUS_PATH, JSON.stringify({ version: 1, accounts: registry }));
    const m = await manager(name);
    const { user: admin, auth } = await signedIn("boss", "admin");
    expect(auth.setAccountLinks(admin, auth.store.userByName(name).id, accounts, registry)).toMatchObject({ ok: true });
    return m;
  }

  test("a manager linked only to another account is refused 403 in plain words; no record, no audit line", async () => {
    const id = newRun(); // a BTC run: account acct-a
    const { cookie, record } = await linkedManager("ana", ["acct-b"]);
    const res = await post(cookie, id, { confirm_token: tokenOf(id, "ana") });
    expect(res.status).toBe(403);
    expect((await res.json()).error).toBe(
      "you do not manage account Account A (acct-a), which bot btc trades for: ask an admin to link it to you on the Users page",
    );
    expect(records()).toEqual([]);
    expect(changeLines(record)).toEqual([]);
  });

  test("a manager linked to the run's account may confirm", async () => {
    const id = newRun();
    const { cookie } = await linkedManager("ana", ["acct-a"]);
    expect((await post(cookie, id, { confirm_token: tokenOf(id, "ana") })).status).toBe(201);
  });
});

describe("Scenario: A tampered or expired token", () => {
  test.each([
    ["a token for another run", (id) => tokenOf(newRun())],
    ["a changed value", (id) => {
      const token = tokenOf(id);
      put(id, report({ diffs: [diff(FILLS, "0.001", "0.003")] }));
      return token;
    }],
    ["an expired token", (id) => tokenOf(id, "mgr", Date.now() - (CONFIRM_TOKEN_MINUTES + 1) * MIN)],
    ["a token flipped in one character", (id) => {
      const t = tokenOf(id);
      return `${t[0] === "0" ? "1" : "0"}${t.slice(1)}`;
    }],
    ["no hex at all", () => "not a token"],
  ])("%s: refused, nothing recorded", async (_, make) => {
    const id = newRun();
    const token = make(id);
    const { cookie, record } = await manager();
    const res = await post(cookie, id, { confirm_token: token });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.error).toBe(`the confirm token does not match this run, your username and these values, or it is older than ${CONFIRM_TOKEN_MINUTES} minutes: open the preview again`);
    expect(body).toMatchObject({ applied: false, recorded: false });
    expect(records()).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });

  test.each([
    ["issued now", 0, true],
    ["10 minutes old", 10 * MIN, true],
    [`${CONFIRM_TOKEN_MINUTES} minutes old`, CONFIRM_TOKEN_MINUTES * MIN, false],
  ])("a token %s", (_, age, ok) => {
    const now = Date.parse("2026-11-06T12:02:30Z");
    put("20261106T120000Z", report());
    const p = buildPreview(readRun(dir, "20261106T120000Z").run, "mgr", now);
    expect(confirmTokenMatches(confirmToken(p, "k".repeat(32), now - age), p, "k".repeat(32), now)).toBe(ok);
  });
});

describe("the server re-checks what the preview checks", () => {
  test.each([
    ["a value out of range", { diffs: [diff(FILLS, "0.001", "not a number")] }, "this run cannot be confirmed: "],
    ["a run that started from another value", { diffs: [diff(FILLS, "0.005", "0.002")] }, "the run started from 0.005, the account now has 0.001"],
    ["a run with no numbers for the proposal", { backtest: { ran: true, equity_usd: 1, base: figures, proposed: { error: "boom" } } }, "no backtested numbers"],
    ["a run that changed nothing", { diffs: [] }, "there is nothing to confirm"],
  ])("%s is refused even with a valid token", async (_, over, reason) => {
    const id = newRun(over);
    const { cookie, record } = await manager();
    const p = buildPreview(readRun(dir, id).run, "mgr", Date.now());
    const token = p ? confirmToken(p, authKey(TEST_SECRET), Date.now()) : "a".repeat(64);
    const res = await post(cookie, id, { confirm_token: token });
    expect(res.status).toBe(409);
    expect((await res.json()).error).toContain(reason);
    expect(records()).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });

  test("a run whose request is not done is refused", async () => {
    const id = newRun();
    const requests = path.join(dir, "requests");
    fs.mkdirSync(requests);
    const reqId = "20261106T120000Z-0123abcd";
    fs.writeFileSync(path.join(requests, `${reqId}.json`), JSON.stringify({ id: reqId, status: "running", run_id: id }));
    const { cookie } = await manager();
    const res = await post(cookie, id, { confirm_token: tokenOf(id) });
    expect(res.status).toBe(409);
    expect((await res.json()).error).toBe("the run is not finished (its request is running): wait until it is done");
    expect(records()).toEqual([]);
  });

  test.each([
    ["no body", ""],
    ["no token", {}],
    ["malformed JSON", "{ broken"],
  ])("%s answers 400 and says what to send", async (_, body) => {
    const id = newRun();
    const { cookie, record } = await manager();
    const res = await post(cookie, id, body);
    expect(res.status).toBe(400);
    expect((await res.json()).error).toContain("confirm_token");
    expect(records()).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });

  test.each([
    ["an id that is not a run", "../secrets", {}, 404],
    ["no such run", "20261106T120000Z", {}, 404],
    ["cross-origin", "20261106T120000Z", { origin: "http://evil.example" }, 403],
    ["not JSON", "20261106T120000Z", { "content-type": "text/plain" }, 415],
  ])("%s", async (_, id, headers, status) => {
    const { cookie, record } = await manager();
    expect((await post(cookie, id, { confirm_token: "a".repeat(64) }, headers)).status).toBe(status);
    expect(records()).toEqual([]);
    expect(record).not.toHaveBeenCalled();
  });

  test("a body over 1 KB is refused", async () => {
    const id = newRun();
    const { cookie } = await manager();
    expect((await post(cookie, id, { confirm_token: "a".repeat(2000) })).status).toBe(413);
  });

  test("an unreadable report answers 500 without its content", async () => {
    const id = freshId();
    put(id, "{ SECRET broken");
    const { cookie } = await manager();
    const res = await post(cookie, id, { confirm_token: "a".repeat(64) });
    expect(res.status).toBe(500);
    expect(await res.text()).not.toContain("SECRET");
  });
});
