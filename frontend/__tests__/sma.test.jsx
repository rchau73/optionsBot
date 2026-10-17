import { render, screen } from "@testing-library/react";
import { describeEvent, smaLevels } from "@/lib/monitor";
import { SmaLevels } from "@/components/Header";

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

describe("SmaLevels", () => {
  test("shows the levels, distances and swing days, with the reason on hover", () => {
    render(<SmaLevels sma={smaLevels(status)} />);
    const el = screen.getByTestId("sma-levels");
    expect(el.textContent).toContain("SMA9 $82,756");
    expect(el.textContent).toContain("SMA21 $83,741");
    expect(el.textContent).toContain("Swing high $86,207 (+4.5%) 10-04");
    expect(el.textContent).toContain("Swing low $82,620 (+0.1%) 10-08");
    expect(el.getAttribute("title")).toContain("SMA trend bear: price < swing low");
  });

  test("renders nothing without levels", () => {
    const { container } = render(<SmaLevels sma={null} />);
    expect(container).toBeEmptyDOMElement();
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
