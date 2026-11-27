// The SOL and XRP bots' amounts are in USDC (#837): /api/status's `unit`
// names it, the leg rows and the KPI strip read it, and a USDC amount is
// already dollars (never × spot). BTC and ETH keep their coin and × spot.
import fs from "fs";
import path from "path";
import { LINEAR_COINS, formatMoney, settlementUnit, summaryUnit, toUSD, unitOf, worstValue } from "@/lib/details";
import { legRows, summarise } from "@/lib/monitor";

const leg = { position_id: "p1", instrument: "SOL_USDC-25DEC26-80-C", option_type: "call", qty: 10, unrealised_pnl: 4.5 };
const bot = (name, status) => ({ name, status, positions: { strangles: [{ id: "s1", slot: { dte: 30, delta: 0.16 }, legs: [leg] }] } });

describe("the unit of a bot's amounts", () => {
  test("SOL and XRP bots read USDC from /api/status", () => {
    expect(unitOf(bot("sol", { underlying: "SOL", unit: "USDC" }))).toBe("USDC");
    expect(unitOf(bot("xrp", { underlying: "XRP", unit: "USDC" }))).toBe("USDC");
  });

  test("BTC keeps its coin; an older bot without the field falls back to its underlying, then its name", () => {
    expect(unitOf(bot("btc", { underlying: "BTC", unit: "BTC" }))).toBe("BTC");
    expect(unitOf(bot("eth", { underlying: "ETH" }))).toBe("ETH");
    expect(unitOf({ name: "eth" })).toBe("ETH");
    expect(unitOf(null)).toBe("");
  });

  test("a USDC amount is already USD; a coin amount is × spot; nothing is guessed", () => {
    expect(toUSD(4.5, "USDC", 70)).toBe(4.5);
    expect(toUSD(4.5, "USDC", undefined)).toBe(4.5);
    expect(toUSD(0.01, "BTC", 100000)).toBe(1000);
    expect(toUSD(0.01, "BTC", undefined)).toBeNull();
    expect(toUSD(null, "USDC", 70)).toBeNull();
  });
});

describe("pages that list bots", () => {
  test("a SOL leg row is in USDC and its USD figure is the USDC figure", () => {
    const [row] = legRows([bot("sol", { underlying: "SOL", unit: "USDC", market: { spot: 70 } })]);
    expect(row.unit).toBe("USDC");
    expect(row.pnlUsd).toBe(4.5);
  });

  test("a BTC leg row is unchanged: BTC, × spot", () => {
    const [row] = legRows([bot("btc", { underlying: "BTC", unit: "BTC", market: { spot: 100000 } })]);
    expect(row.unit).toBe("BTC");
    expect(row.pnlUsd).toBe(450000);
  });

  test("the KPI strip adds a SOL bot's USDC P&L as dollars beside BTC's at spot", () => {
    const pnl = (t) => [{ slot: null, realised: t, unrealised: 0, total: t }];
    const kpi = summarise([
      { name: "btc", status: { underlying: "BTC", unit: "BTC", market: { spot: 100000 }, pnl: pnl(0.001) } },
      { name: "sol", status: { underlying: "SOL", unit: "USDC", market: { spot: 70 }, pnl: pnl(12) } },
    ]);
    expect(kpi.totalUsd).toBeCloseTo(112, 9);
    expect(kpi.realisedUsd).toBeCloseTo(112, 9);
  });
});

describe("the settlement unit without /api/status (review of #906, round 2)", () => {
  test("LINEAR_COINS is the Go map linearCoins of internal/config/settlement.go", () => {
    const src = fs.readFileSync(path.join(__dirname, "..", "..", "internal", "config", "settlement.go"), "utf8");
    const m = src.match(/linearCoins = map\[string\]bool\{([^}]*)\}/);
    expect(m).not.toBeNull();
    const goCoins = [...m[1].matchAll(/"([A-Z]+)":\s*true/g)].map((x) => x[1]).sort();
    expect([...LINEAR_COINS].sort()).toEqual(goCoins);
  });

  test("SOL and XRP settle in USDC, BTC and ETH in themselves; an unknown coin stays as given", () => {
    expect(settlementUnit("SOL")).toBe("USDC");
    expect(settlementUnit("xrp")).toBe("USDC");
    expect(settlementUnit("BTC")).toBe("BTC");
    expect(settlementUnit("ETH")).toBe("ETH");
    expect(settlementUnit("")).toBe("");
    expect(unitOf({ name: "sol" })).toBe("USDC"); // status not in yet: the name's settlement unit
  });

  test("a summary's unit: its income's unit, else its underlying's", () => {
    expect(summaryUnit({ underlying: "SOL", income: { coin: "SOL", unit: "USDC" } })).toBe("USDC");
    expect(summaryUnit({ underlying: "XRP" })).toBe("USDC");
    expect(summaryUnit({ underlying: "BTC", income: { coin: "BTC", unit: "BTC" } })).toBe("BTC");
    expect(summaryUnit({ underlying: "ETH", income: { coin: "ETH" } })).toBe("ETH");
    expect(summaryUnit(null)).toBe("");
  });

  test("USDC in cents, a coin with 5 decimals as before; a worst case in USDC is its dollar figure", () => {
    expect(formatMoney(9, "USDC")).toBe("+9.00 USDC");
    expect(formatMoney(-0.004, "BTC")).toBe("−0.00400 BTC");
    expect(formatMoney(0.004, "ETH")).toBe("+0.00400 ETH");
    expect(worstValue(0.5, 70, "USDC")).toBe(70);
    expect(worstValue(0.05, 4935, "BTC")).toBe(0.05);
  });
});
