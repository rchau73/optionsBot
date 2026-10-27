import { render, screen } from "@testing-library/react";
import { describeEvent, smaLevels } from "@/lib/monitor";
import RegimeChart from "@/components/RegimeChart";

const status = {
  market: { spot: 82500 },
  sma: {
    sma9: 82756.2, sma21: 83741.4,
    swing_high: 86206.5, swing_high_date: "2026-10-04",
    swing_low: 82619.5, swing_low_date: "2026-10-08",
    price: 82500, trend: "bear", reason: "price < swing low",
  },
};

describe("smaLevels (#118)", () => {
  test("each level with its day and spot's distance to it", () => {
    const s = smaLevels(status);
    expect(s.trend).toBe("bear");
    expect(s.reason).toBe("price < swing low");
    expect(s.levels.map((l) => l.key)).toEqual(["sma9", "sma21", "swing_high", "swing_low"]);
    const high = s.levels[2];
    expect(high.date).toBe("2026-10-04");
    expect(high.distancePct).toBeCloseTo(((86206.5 - 82500) / 82500) * 100, 6);
    expect(s.levels[0].date).toBeNull();
  });

  test("unknown levels (0) are left out; no spot → no distance; no sma → null", () => {
    const s = smaLevels({ sma: { sma9: 100, sma21: 0, swing_high: 0, swing_low: 0, trend: "neutral", reason: "no confirmed swing yet" } });
    expect(s.levels).toHaveLength(1);
    expect(s.levels[0].distancePct).toBeNull();
    expect(smaLevels({})).toBeNull();
    expect(smaLevels(null)).toBeNull();
  });
});

// #394: the levels moved from the header chip onto the regime chart; its text alternative states them.
describe("the regime chart states the SMA levels", () => {
  test("spot against SMA9, SMA21 and the swings with their days, and the trend's reason", () => {
    render(<RegimeChart status={{ ...status, trend: "bear" }} coin="BTC" />);
    const text = screen.getByRole("figure").querySelector("figcaption").textContent;
    expect(text).toContain("trend bear: price < swing low");
    expect(text).toContain("below SMA9 $82,756");
    expect(text).toContain("below SMA21 $83,741");
    expect(text).toContain("swing high $86,207 (2026-10-04)");
    expect(text).toContain("swing low $82,620 (2026-10-08)");
  });
});

describe("describeEvent trend_changed", () => {
  const ev = (data) => ({ seq: 7, event: "trend_changed", at: "2026-10-16T12:00:00Z", data });

  test("old → new trend, the reason and the levels", () => {
    const line = describeEvent("eth", ev({
      from: "neutral", to: "bear", reason: "price < swing low", direction: "sma",
      spot: 2400, sma9: 2450, sma21: 2500, swing_high: 2700, swing_high_date: "2026-10-04", swing_low: 2410, swing_low_date: "2026-10-08",
    }));
    expect(line).toMatchObject({ label: "SMA trend bear", tone: "warn", id: "eth:7", event: "trend_changed" });
    expect(line.text).toBe("neutral → bear — price < swing low");
    expect(line.context).toEqual(["spot $2,400", "SMA9 $2,450", "SMA21 $2,500", "swing high $2,700 (2026-10-04)", "swing low $2,410 (2026-10-08)"]);
  });

  test("under the bias source the line says the SMA trend is only recorded", () => {
    const line = describeEvent("btc", ev({ from: "bear", to: "neutral", reason: "inside the swings, SMAs and price not aligned", direction: "bias" }));
    expect(line.tone).toBe("neutral");
    expect(line.text).toContain("(recorded; rules use the GEX bias)");
    expect(line.context).toEqual([]);
  });
});
