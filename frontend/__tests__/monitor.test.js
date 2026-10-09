import { consolidateRows, describeEvent, filterRows, flipBand, groupRows, legRows, mergeFeed, summarise } from "@/lib/monitor";
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

describe("consolidateRows", () => {
  const leg = (over) => ({
    key: "k", bot: "btc", unit: "BTC", slot: "60d · Δ0.18", instrument: "BTC-27NOV26-99000-C", side: "sell",
    type: "call", qty: 1.3, entry: 0.0125, pnl: 0.001, pnlUsd: 86, stopMark: 0.0375, markSource: "live", ...over,
  });

  it("merges positions of one bot, slot and strike", () => {
    const [row, ...rest] = consolidateRows([leg(), leg({ qty: 0.1, entry: 0.013, pnl: 0.0001, pnlUsd: 8.6, stopMark: 0.039 })]);
    expect(rest).toHaveLength(0);
    expect(row.positions).toBe(2);
    expect(row.qty).toBe(1.4);
    expect(row.entry).toBeCloseTo((1.3 * 0.0125 + 0.1 * 0.013) / 1.4, 10);
    expect(row.pnl).toBeCloseTo(0.0011, 10);
    expect(row.pnlUsd).toBeCloseTo(94.6, 6);
    expect(row.roiPct).toBeCloseTo((0.0011 / (1.3 * 0.0125 + 0.1 * 0.013)) * 100, 6);
    expect(row.stopMark).toBe(0.0375); // the first position to stop
  });

  it("keeps different slots, strikes, bots and sides apart", () => {
    const rows = consolidateRows([
      leg(),
      leg({ slot: "45d · Δ0.16" }),
      leg({ instrument: "BTC-27NOV26-100000-C" }),
      leg({ bot: "eth" }),
      leg({ side: "buy" }),
    ]);
    expect(rows).toHaveLength(5);
    expect(rows.every((r) => r.positions === 1)).toBe(true);
  });

  it("is not live when any merged position is not", () => {
    const [row] = consolidateRows([leg(), leg({ markSource: "last_cycle" })]);
    expect(row.markSource).toBe("last_cycle");
  });

  it("keeps missing P&L missing, never a silent 0", () => {
    const [row] = consolidateRows([leg({ pnl: null, pnlUsd: null }), leg({ pnl: null, pnlUsd: null })]);
    expect(row.pnl).toBeNull();
    expect(row.roiPct).toBeNull();
  });
});

describe("group header totals", () => {
  const leg = (over) => ({
    key: "k", bot: "btc", unit: "BTC", slot: "45d · Δ0.16", strategyId: "s", instrument: "BTC-27NOV26-100000-C", side: "sell",
    type: "call", qty: 1, entry: 0.01, spot: 100000, pnl: 0.002, pnlUsd: 200, delta: 0.16, gamma: 0.00002, theta: -30, vega: 80, ...over,
  });

  it("P&L % is the group's P&L over its premium", () => {
    const [g] = groupRows([leg(), leg({ instrument: "BTC-27NOV26-76000-P", type: "put", qty: 2, entry: 0.0125, pnl: -0.001, pnlUsd: -100 })]);
    // premium: 0.01×1 + 0.0125×2 = 0.035 BTC = $3,500; P&L $100
    expect(g.roiPct).toBeCloseTo((100 / 3500) * 100, 6);
  });

  it("greeks are per-option × qty, negative for shorts, positive for longs", () => {
    const [g] = groupRows([leg(), leg({ instrument: "BTC-27NOV26-76000-P", delta: -0.16, qty: 2 }), leg({ side: "buy", qty: 0.5 })]);
    expect(g.greeks.delta).toBeCloseTo(-0.16 * 1 + 0.16 * 2 + 0.16 * 0.5, 10);
    expect(g.greeks.theta).toBeCloseTo(30 * 1 + 30 * 2 - 30 * 0.5, 10); // shorts earn theta
    expect(g.greeks.vega).toBeCloseTo(-80 * 1 - 80 * 2 + 80 * 0.5, 10);
  });

  it("does not add BTC and ETH deltas; theta and vega (USD) still add", () => {
    const [g] = groupRows([leg(), leg({ bot: "eth", unit: "ETH" })], "type");
    expect(g.greeks.delta).toBeNull();
    expect(g.greeks.gamma).toBeNull();
    expect(g.greeks.theta).toBeCloseTo(60, 10);
  });
});

describe("closed events say why", () => {
  it("adds the delta and the journaled detail", () => {
    const line = describeEvent("eth", {
      seq: 1, at: "2026-10-06T22:09:44Z", event: "closed",
      data: { qty: 100, instrument: "ETH-27NOV26-3300-C", fill_price: 0.016, pnl: -0.03, pnl_usd: -81, delta: 0.1686,
        close_reason: "rebalance_legs", detail: "leg balance: call 262 vs put 162 — not a stop" },
    });
    expect(line.label).toBe("Balance legs");
    expect(line.text).toContain("Δ 0.169");
    expect(line.text).toContain("leg balance: call 262 vs put 162 — not a stop");
  });
});

describe("flipBand", () => {
  const st = (market, flip_buffer_pct) => ({ market, flip_buffer_pct });

  test("flip ± the bot's buffer, and where spot is", () => {
    const b = flipBand(st({ spot: 83000, gamma_flip: 83400, spot_to_flip_pct: -0.48 }, 1.91));
    expect(b.low).toBeCloseTo(83400 * 0.9809);
    expect(b.high).toBeCloseTo(83400 * 1.0191);
    expect(b).toMatchObject({ flip: 83400, spotToFlipPct: -0.48, bufferPct: 1.91, zone: "inside" });
    expect(flipBand(st({ spot: 80000, gamma_flip: 83400 }, 1.91)).zone).toBe("below");
    expect(flipBand(st({ spot: 90000, gamma_flip: 83400 }, 1.91)).zone).toBe("above");
  });

  test("no buffer: the flip alone; no flip: nothing", () => {
    expect(flipBand(st({ spot: 2490, gamma_flip: 2260 }, 0))).toMatchObject({ flip: 2260, low: null, high: null, zone: null });
    expect(flipBand(st({ spot: 2490 }, 3))).toBeNull();
    expect(flipBand(undefined)).toBeNull();
  });
});
