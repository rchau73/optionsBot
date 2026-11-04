/**
 * @jest-environment node
 */
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { POST } from "@/app/api/lab/validate/route";
import labSettings from "@/content/lab-settings.json";
import { APPLIES_NOTHING, RULES_NOT_CHECKED, validateProposal } from "@/lib/labvalidate";
import { signedIn, useTestAuth } from "@/test/auth";

// #586: POST /api/lab/validate — may a proposed value be tried, and who must
// approve it. The bounds come from content/lab-settings.json, generated from
// the Go catalogue (tests/lab_settings_test.go checks them against Validate).

const ORIGIN = "http://localhost:3100";
const REPO = path.join(__dirname, "..", "..");

const req = (cookie, body, headers = {}) =>
  new Request(`${ORIGIN}/api/lab/validate`, {
    method: "POST",
    headers: { origin: ORIGIN, host: "localhost:3100", "content-type": "application/json", ...(cookie ? { cookie } : {}), ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });

let logs;
beforeEach(() => {
  logs = [];
  jest.spyOn(console, "error").mockImplementation((l) => logs.push(l));
  jest.spyOn(console, "log").mockImplementation((l) => logs.push(l));
  useTestAuth();
});
afterEach(() => jest.restoreAllMocks());

async function asManager(body) {
  const { cookie, auth } = await signedIn("mgr", "manager");
  const record = jest.spyOn(auth, "recordAction");
  const res = await POST(req(cookie, body));
  return { res, json: await res.json(), record };
}

describe("Scenario: value in range", () => {
  test("valid is true and the tip, range and current value are returned", async () => {
    const { res, json, record } = await asManager({ key: "roi_take_profit", value: 0.6 });
    expect(res.status).toBe(200);
    expect(json).toEqual(
      expect.objectContaining({
        key: "roi_take_profit",
        value: 0.6,
        underlying: "BTC",
        config: "config_btc.yaml",
        valid: true,
        reasons: [],
        current: 0.5,
        applied: APPLIES_NOTHING,
      }),
    );
    expect(json.tip).toEqual(expect.objectContaining({ kind: "setting", key: "roi_take_profit", meaning: expect.any(String), raise_risk: expect.any(String), raise_income: expect.any(String) }));
    // The audit line names the key and the verdict, never the value.
    expect(record).toHaveBeenCalledWith(expect.objectContaining({ username: "mgr" }), "lab_validate", { key: "roi_take_profit", underlying: "BTC", valid: true });
  });

  test("a value inside the range and the cross rules of each config", () => {
    for (const [body, current] of [
      [{ key: "delta_drift_threshold", value: 0.12 }, 0.1], // 75 % of 0.16, at the bound
      [{ key: "gex_direction", value: "sma", underlying: "eth" }, "sma"],
      [{ key: "dte_delta_matrix[].dte", value: 16 }, [25, 50]],
      [{ key: "backtest.margin_call_liquidation", value: false }, true],
    ]) {
      const r = validateProposal(body);
      expect([body.key, r.status, r.body.valid, r.body.reasons]).toEqual([body.key, 200, true, []]);
      expect(r.body.current).toEqual(current);
    }
  });
});

describe("Scenario: value breaks a cross rule", () => {
  test("a delta_drift_threshold above 75 % of an entry delta is refused with the rule in plain words", async () => {
    const { res, json, record } = await asManager({ key: "delta_drift_threshold", value: 0.13 });
    expect(res.status).toBe(200);
    expect(json.valid).toBe(false);
    expect(json.reasons).toEqual(["must be at most 75 % of every entry delta: the lowest is 0.16, so at most 0.12"]);
    expect(json.cross_rules).toEqual(["at most 75 % of every entry delta (config.MaxDriftToEntryRatio)"]);
    expect(record).toHaveBeenCalledWith(expect.anything(), "lab_validate", { key: "delta_drift_threshold", underlying: "BTC", valid: false });
  });

  test.each([
    ["delta_exit_threshold", 0.16, "must be above every entry delta: the highest is 0.16"],
    ["dte_delta_matrix[].deltas[]", 0.3, "must be below delta_exit_threshold (0.3), or a new leg would be exited at once"],
    ["dte_delta_matrix[].deltas[]", 0.13, "must keep delta_drift_threshold (0.1) at most 75 % of it, so at least 0.1333"],
    ["rollout_dte", 25, "must be below every slot's days to expiry: the lowest is 25"],
    ["dte_delta_matrix[].dte", 15, "must be above rollout_dte (15), or a new strangle would be rolled at once"],
  ])("%s = %s", (key, value, rule) => {
    const r = validateProposal({ key, value });
    expect(r.body.valid).toBe(false);
    expect(r.body.reasons).toEqual([rule]);
  });

  test("the margin bands' list rules are listed, said not checked, never claimed as passed", () => {
    // 35 duplicates an existing band start: one value cannot say which band it replaces.
    const r = validateProposal({ key: "iv_margin_bands[].min_iv_pct", value: 35 });
    expect(r.body).toEqual(
      expect.objectContaining({
        valid: true,
        cross_rules: ["the lowest band starts at 0", "no two bands start at the same percentile"],
        cross_rules_checked: false,
        cross_rules_note: RULES_NOT_CHECKED,
      }),
    );
    const checked = validateProposal({ key: "delta_exit_threshold", value: 0.35 }).body;
    expect([checked.cross_rules_checked, checked.cross_rules_note]).toEqual([true, null]);
    expect(validateProposal({ key: "roi_take_profit", value: 0.6 }).body.cross_rules_note).toBeNull();
  });

  test("the setting's own range, step and kind come first", () => {
    for (const [body, reason] of [
      [{ key: "gamma_flip_buffer_sd", value: 3.1 }, "must be at most 3 daily standard deviations"],
      [{ key: "gamma_flip_buffer_sd", value: -0.1 }, "must be at least 0 daily standard deviations"],
      [{ key: "gamma_flip_buffer_sd", value: 1.25 }, "must be on a step of 0.1"],
      [{ key: "repair_cooldown_hours", value: 1.5 }, "must be a whole number"],
      [{ key: "gex_direction", value: "up" }, "must be one of sma, bias"],
      [{ key: "max_mm_pct", value: "35" }, "must be a number"],
      [{ key: "backtest.margin_call_liquidation", value: "yes" }, "must be true or false"],
    ]) {
      const r = validateProposal(body);
      expect([body.key, r.body.valid, r.body.reasons]).toEqual([body.key, false, [reason]]);
    }
    // At the bounds and on the step (0.1 × 3 is 0.30000000000000004).
    expect(validateProposal({ key: "gamma_flip_buffer_sd", value: 3 }).body.valid).toBe(true);
    expect(validateProposal({ key: "gamma_flip_buffer_sd", value: 0.1 * 3 }).body.valid).toBe(true);
  });
});

describe("Scenario: core policy", () => {
  test("a key in a core-policy area says the product owner approves a new default", async () => {
    const { json } = await asManager({ key: "delta_exit_threshold", value: 0.35 });
    expect(json).toEqual(expect.objectContaining({ valid: true, core_policy: true, policy: "exits", policy_note: "changing the default needs the product owner" }));
    expect(json.tip.needs_product_owner).toBe(true);
  });

  test("a backtest-only key is not core policy", () => {
    const r = validateProposal({ key: "backtest.slippage_pct", value: 0.002 });
    expect(r.body).toEqual(expect.objectContaining({ core_policy: false, policy: "none", policy_note: null }));
  });
});

describe("Scenario: a user", () => {
  test("a signed-in user role is refused with 403 and nothing is audited", async () => {
    const { cookie, auth } = await signedIn("viewer", "user");
    const record = jest.spyOn(auth, "recordAction");
    const res = await POST(req(cookie, { key: "roi_take_profit", value: 0.6 }));
    expect(res.status).toBe(403);
    expect(record).not.toHaveBeenCalled();
  });

  test("no session is 401", async () => {
    expect((await POST(req(null, { key: "roi_take_profit", value: 0.6 }))).status).toBe(401);
  });
});

describe("refusals", () => {
  test("an unknown key is 404, a fixed key 400 saying it is not tunable", async () => {
    const { res } = await asManager({ key: "no_such_key", value: 1 });
    expect(res.status).toBe(404);
    const fixed = validateProposal({ key: "gex_strike_range_pct", value: 0.2 });
    expect(fixed.status).toBe(400);
    expect(fixed.body.error).toBe("gex_strike_range_pct is not tunable: it is fixed in code (0.15)");
  });

  test("a body without a key or a value, a bad underlying, malformed JSON, cross-origin and non-JSON are refused", async () => {
    for (const body of [{}, { key: "roi_take_profit" }, { value: 1 }, null, [], "x"]) expect(validateProposal(body).status).toBe(400);
    expect(validateProposal({ key: "roi_take_profit", value: 0.6, underlying: "SOL" })).toEqual({ status: 400, body: { error: "underlying must be one of BTC, ETH" } });
    const { cookie, auth } = await signedIn("mgr", "manager");
    const record = jest.spyOn(auth, "recordAction");
    expect((await POST(req(cookie, "{ broken"))).status).toBe(400);
    expect((await POST(req(cookie, { key: "roi_take_profit", value: 0.6 }, { origin: "http://evil.example" }))).status).toBe(403);
    expect((await POST(req(cookie, { key: "roi_take_profit", value: 0.6 }, { "content-type": "text/plain" }))).status).toBe(415);
    expect((await POST(req(cookie, { key: "roi_take_profit", value: "x".repeat(3000) }))).status).toBe(413);
    expect(record).not.toHaveBeenCalled();
  });
});

describe("Scenario: nothing applied", () => {
  test("valid requests change no config file, no settings data and call no bot", async () => {
    const files = ["config_btc.yaml", "config_btc.mvp.yaml", "config_eth.yaml", "frontend/content/lab-settings.json", "frontend/content/tips.json"];
    const hash = () => files.map((f) => crypto.createHash("sha256").update(fs.readFileSync(path.join(REPO, f))).digest("hex"));
    const before = hash();
    const settingsBefore = JSON.stringify(labSettings);
    const fetchSpy = jest.spyOn(global, "fetch").mockImplementation(() => {
      throw new Error("no bot call");
    });
    for (const body of [
      { key: "delta_drift_threshold", value: 0.05 },
      { key: "gex_direction", value: "bias", underlying: "ETH" },
      { key: "max_mm_pct", value: 40 },
    ]) {
      const { res, json } = await asManager(body);
      expect(res.status).toBe(200);
      expect(json.applied).toBe(APPLIES_NOTHING);
    }
    expect(hash()).toEqual(before);
    expect(JSON.stringify(labSettings)).toBe(settingsBefore);
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
