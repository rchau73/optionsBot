import { describeEvent, filterRows, groupRows, legRows, mergeFeed, summarise } from "@/lib/monitor";
import { withStale } from "@/hooks/useMonitor";
import { bot, positions } from "@/test/fixtures";

describe("legRows / filter / group", () => {
  const eth = bot({ name: "eth", status: { underlying: "ETH", strategy_id: "short-strangle", market: { spot: 3500 } } });
  const rows = legRows([bot(), eth]);

  test("one row per open leg, with USD P&L at spot", () => {
    expect(rows).toHaveLength(4);
    const call = rows.find((r) => r.bot === "btc" && r.type === "call");
    expect(call).toMatchObject({ strike: 115000, slot: "45d · 0.16Δ", unit: "BTC", delta: 0.12 });
    expect(call.pnlUsd).toBeCloseTo(30);
  });

  test("filters combine", () => {
    expect(filterRows(rows, { bot: "btc", type: "put" })).toHaveLength(1);
    expect(filterRows(rows, { moneyness: "ITM" })).toHaveLength(0);
    expect(filterRows(rows, {})).toHaveLength(4);
  });

  test("groups total P&L; coin only when the group has one unit", () => {
    const byStrategy = groupRows(rows, "strategy");
    expect(byStrategy.map((g) => g.key)).toEqual(["BTC · short-strangle", "ETH · short-strangle"]);
    expect(byStrategy[0].pnl).toBeCloseTo(0.0005);
    const byType = groupRows(rows, "type");
    expect(byType).toHaveLength(2);
    expect(byType[0].unit).toBeNull(); // calls from BTC and ETH: no single coin
    expect(byType[0].pnlUsd).toBeCloseTo(30 + 0.0003 * 3500);
  });

  test("bots without positions produce no rows", () => {
    expect(legRows([{ name: "btc" }])).toEqual([]);
    expect(legRows([bot({ positions: { strangles: [] } })])).toEqual([]);
    expect(positions.strangles).toHaveLength(1); // fixture untouched
  });
});

describe("summarise", () => {
  test("adds counts and USD P&L across bots, skipping offline ones", () => {
    const k = summarise([bot(), bot({ name: "eth", stale: true }), { name: "down" }]);
    expect(k).toMatchObject({ bots: 3, online: 1, openLegs: 4, pending: 2, submitted: 8, closed: 2, halted: false });
    expect(k.totalUsd).toBeCloseTo(300);
  });

  test("flags a halted bot", () => {
    expect(summarise([bot({ status: { ...bot().status, halted: true } })]).halted).toBe(true);
  });
});

describe("staleness", () => {
  test("stale without a recent good update or on error", () => {
    const now = 100_000;
    expect(withStale({ updatedAt: now - 1000 }, now).stale).toBe(false);
    expect(withStale({ updatedAt: now - 6000 }, now).stale).toBe(true);
    expect(withStale({ updatedAt: now, error: "502" }, now).stale).toBe(true);
    expect(withStale({}, now).stale).toBe(true);
  });
});

describe("describeEvent", () => {
  const ev = (event, data, seq = 1) => ({ seq, event, at: "2026-10-02T12:00:00Z", data });
  const market = { spot: 100000, dvol: 55, moneyness: "OTM", distance_to_strike_pct: 12.3, strike_oi: 1500.4, strike_oi_rank: 2, gex_regime: "POSITIVE/PINNING" };

  test("closes are labelled by reason, toned by result, with P&L", () => {
    const line = describeEvent("btc", ev("closed", { close_reason: "roi_target", qty: 0.1, instrument: "X", fill_price: 0.005, pnl: 0.0015, pnl_usd: 150, slot: { dte: 45, delta: 0.16 }, market }));
    expect(line).toMatchObject({ label: "Take-profit", tone: "good", id: "btc:1" });
    expect(line.text).toContain("+0.00150 BTC");
    expect(line.text).toContain("[45d · 0.16Δ]");
    expect(line.context).toEqual(["spot $100,000", "DVOL 55.0", "OTM 12.3%", "OI 1500 (#2)", "POSITIVE/PINNING"]);
    expect(describeEvent("btc", ev("closed", { close_reason: "stop_loss", pnl: -1 })).tone).toBe("bad");
  });

  test("skips explain why; P&L lines are not shown in the feed", () => {
    expect(describeEvent("eth", ev("skipped", { skip_reason: "no_expiry", slot: { dte: 25, delta: 0.16 } })).text).toBe("[25d · 0.16Δ] not entered — no_expiry");
    expect(describeEvent("btc", ev("pnl", {}))).toBeNull();
  });

  test("every journal event type has a readable line", () => {
    for (const e of ["submitted", "amended", "cancelled", "filled", "reconciled"]) {
      const line = describeEvent("btc", ev(e, { instrument: "BTC-X", qty: 0.1, limit_price: 0.01, previous_price: 0.012, fill_price: 0.01 }));
      expect(line.text).toContain("BTC-X");
    }
  });

  test("mergeFeed de-duplicates and keeps newest first", () => {
    const a = { id: "btc:1", at: "2026-10-02T12:00:00Z" };
    const b = { id: "btc:2", at: "2026-10-02T12:00:05Z" };
    const merged = mergeFeed([a], [a, b, null]);
    expect(merged.map((e) => e.id)).toEqual(["btc:2", "btc:1"]);
    expect(mergeFeed([], [a, b], 1)).toHaveLength(1);
  });
});
