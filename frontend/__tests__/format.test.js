import { ageSeconds, formatAge, formatCountdown, formatCoin, formatPct, formatPrice, formatTime, formatUSD, slotLabel } from "@/lib/format";

describe("format", () => {
  test("missing values render as a dash, never zero", () => {
    for (const fn of [formatUSD, formatCoin, formatPrice, formatPct]) {
      expect(fn(undefined)).toBe("—");
      expect(fn(NaN)).toBe("—");
    }
  });

  test("coin amounts are signed with units", () => {
    expect(formatCoin(0.00125, "BTC")).toBe("+0.00125 BTC");
    expect(formatCoin(-0.003, "ETH")).toBe("−0.00300 ETH");
    expect(formatCoin(0, "BTC")).toBe("0.00000 BTC");
  });

  test("USD, percentages and slots", () => {
    expect(formatUSD(1234.5)).toBe("$1,235");
    expect(formatUSD(-12.345, { cents: true })).toBe("-$12.35");
    expect(formatPct(12.345, 1, { signed: true })).toBe("+12.3%");
    expect(slotLabel({ dte: 45, delta: 0.16 })).toBe("45d · 0.16Δ");
    expect(slotLabel(null)).toBe("—");
  });

  test("ages treat Go's zero time as never", () => {
    const now = Date.parse("2026-10-02T12:00:10Z");
    expect(ageSeconds("2026-10-02T12:00:00Z", now)).toBe(10);
    expect(ageSeconds("0001-01-01T00:00:00Z", now)).toBeNull();
    expect(formatAge(null)).toBe("never");
    expect(formatAge(5)).toBe("5s ago");
    expect(formatAge(125)).toBe("2m ago");
    expect(formatTime("2026-10-02T12:34:56Z")).toBe("09:34:56 BRT"); // stored UTC, shown in BRT
  });
});

describe("formatCountdown", () => {
  const now = Date.parse("2026-10-05T12:00:00Z");
  test.each([
    ["2026-10-05T12:00:45Z", "in 45s"],
    ["2026-10-05T12:01:05Z", "in 1m 05s"],
    ["2026-10-05T12:00:00Z", "due"],
    ["2026-10-05T11:59:00Z", "due"],
    ["0001-01-01T00:00:00Z", "—"],
    [undefined, "—"],
    ["garbage", "—"],
  ])("%s → %s", (iso, want) => expect(formatCountdown(iso, now)).toBe(want));
});
