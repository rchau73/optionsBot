/**
 * @jest-environment node
 */
import fs from "node:fs";
import path from "node:path";
import { GET as tipsRoute } from "@/app/api/help/tips/route";
import { findTip } from "@/lib/tips";
import glossary from "@/content/glossary.json";
import { signedIn, useTestAuth } from "@/test/auth";

// #588 (epic E11): the plain-words tips served read-only to any signed-in role.

const get = (cookie, query = "") => tipsRoute(new Request(`http://monitor/api/help/tips${query}`, { headers: cookie ? { cookie } : {} }));

beforeEach(() => useTestAuth());

describe("Scenario: a tip by key", () => {
  test("a setting's tip holds meaning, unit, risk effect and income effect", async () => {
    const { cookie } = await signedIn("viewer", "user");
    const res = await get(cookie, "?key=rollout_dte");
    expect(res.status).toBe(200);
    expect(await res.json()).toMatchObject({
      kind: "setting",
      key: "rollout_dte",
      unit: "days",
      meaning: expect.stringContaining("expiry"),
      raise_risk: expect.any(String),
      raise_income: expect.any(String),
      video: "video.change_setting",
      needs_product_owner: true,
    });
  });

  test("a setting with no video says null; a non-core setting does not need the product owner", async () => {
    const { cookie } = await signedIn("viewer", "user");
    const tip = await (await get(cookie, "?key=strategy_id")).json();
    expect(tip.video).toBeNull();
    expect(tip.needs_product_owner).toBe(false);
  });

  test("without a query every setting's tip is listed", async () => {
    const { cookie } = await signedIn("viewer", "user");
    const { tips } = await (await get(cookie)).json();
    const source = JSON.parse(fs.readFileSync(path.join(__dirname, "../content/tips.json"), "utf8"));
    expect(tips.map((t) => t.key)).toEqual(Object.keys(source).sort());
  });
});

describe("Scenario: a glossary term", () => {
  test("?term=delta answers the entry's short text and its risk and income line", async () => {
    const { cookie } = await signedIn("viewer", "user");
    const res = await get(cookie, "?term=delta");
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      kind: "term",
      term: "delta",
      short: glossary.delta.short,
      risk_income: glossary.delta.risk_income,
    });
  });
});

describe("Scenario: unknown key", () => {
  test.each(["?key=nope", "?key=", "?key=constructor", "?term=nope"])("%s is 404 with a message, never an empty tip", async (query) => {
    const { cookie } = await signedIn("viewer", "user");
    const res = await get(cookie, query);
    expect(res.status).toBe(404);
    expect((await res.json()).error).toMatch(/no tip/);
  });
});

describe("Scenario: no session", () => {
  test("401 for every form of the request", async () => {
    for (const query of ["", "?key=rollout_dte", "?term=delta"]) expect((await get(null, query)).status).toBe(401);
  });
});

describe("findTip", () => {
  test("a setting key, a glossary term, then nothing", () => {
    expect(findTip("max_mm_pct").kind).toBe("setting");
    expect(findTip("delta").kind).toBe("term");
    expect(findTip("no such thing")).toBeNull();
  });
});

describe("Scenario: drift — the copy the image reads", () => {
  test("content/tips.json equals docs/product/education/tips.json (cp docs/product/education/tips.json frontend/content/tips.json)", () => {
    const read = (rel) => fs.readFileSync(path.join(__dirname, rel), "utf8");
    expect(read("../content/tips.json")).toBe(read("../../docs/product/education/tips.json"));
  });
});
