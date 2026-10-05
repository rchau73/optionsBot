import { formatDateTime, formatTime } from "@/lib/format";
import { DISPLAY_TZ_LABEL, localClock, localDateTime, localParts } from "@/lib/time";

// The bot stores and serves UTC; the monitor shows BRT (UTC−3, no daylight
// saving). These must hold whatever time zone the machine running them is in.
describe("display time zone (BRT)", () => {
  test("UTC instants shown three hours earlier", () => {
    expect(DISPLAY_TZ_LABEL).toBe("BRT");
    expect(localClock("2026-10-05T11:14:45Z")).toBe("08:14:45");
    expect(localDateTime("2026-10-05T11:14:45Z")).toBe("2026-10-05 08:14:45 BRT");
  });

  test("crossing midnight changes the date", () => {
    expect(localDateTime("2026-10-05T02:30:00Z")).toBe("2026-10-04 23:30:00 BRT");
    expect(localParts(Date.parse("2027-01-01T00:00:00Z"))).toMatchObject({ year: "2026", month: "12", day: "31", hour: "21" });
  });

  test("offsets in the API's own format are read correctly", () => {
    expect(formatDateTime("2026-10-05T08:14:44.343-03:00")).toBe("2026-10-05 08:14:44 BRT");
  });

  test("no daylight saving: January and July are both UTC−3", () => {
    expect(localClock("2027-01-15T12:00:00Z")).toBe("09:00:00");
    expect(localClock("2027-07-15T12:00:00Z")).toBe("09:00:00");
  });

  test("missing and invalid values", () => {
    expect(formatTime(undefined)).toBe("—");
    expect(formatTime("garbage")).toBe("—");
    expect(formatDateTime("0001-01-01T00:00:00Z")).toBe("—");
    expect(localParts("garbage")).toBeNull();
  });
});
