import { render, screen } from "@testing-library/react";
import Header from "@/components/Header";
import { loopStalled } from "@/lib/monitor";

const now = Date.parse("2026-10-03T21:16:00Z");
const status = (loopAt, extra = {}) => ({ as_of: "2026-10-03T21:16:00Z", environment: "testnet", loop_at: loopAt, eval_interval_ms: 40000, market: {}, ...extra });

describe("loopStalled", () => {
  test("stalled after 3 cycle intervals (at least 2 minutes)", () => {
    expect(loopStalled(status("2026-10-03T21:15:00Z"), now)).toBe(false); // 60 s
    expect(loopStalled(status("2026-10-03T21:13:59Z"), now)).toBe(true); // 121 s > max(120 s, 3 × 40 s)
    expect(loopStalled(status("2026-10-03T21:10:00Z", { eval_interval_ms: 300000 }), now)).toBe(false); // 6 min < 15 min
  });

  test("never for a halted bot, a bot that has not cycled yet, or no data", () => {
    expect(loopStalled(status("2026-10-03T20:48:45Z", { halted: true }), now)).toBe(false);
    expect(loopStalled(status("0001-01-01T00:00:00Z"), now)).toBe(false);
    expect(loopStalled(undefined, now)).toBe(false);
  });
});

test("the header flags a bot whose API answers but whose loop is stuck", () => {
  const bots = [{ name: "btc", stale: false, status: status("2026-10-03T20:48:45Z") }];
  render(<Header bots={bots} />);
  expect(screen.getByText("LOOP STALLED")).toBeInTheDocument();
});
