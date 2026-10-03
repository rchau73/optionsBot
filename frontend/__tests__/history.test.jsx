import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import PnlChart, { tickLabel } from "@/components/PnlChart";
import { combineHistories } from "@/lib/monitor";

describe("combineHistories", () => {
  test("sums bots per aligned bucket, carrying a bot's last value forward", () => {
    const btc = { points: [
      { t: "2026-10-01T12:00:00Z", total_usd: 100, realised_usd: 50 },
      { t: "2026-10-01T12:05:00Z", total_usd: 120, realised_usd: 60 },
    ] };
    const eth = { points: [{ t: "2026-10-01T12:00:00Z", total_usd: -10, realised_usd: 0 }] }; // no point at 12:05
    const out = combineHistories([btc, eth]);
    expect(out).toEqual([
      { t: Date.parse("2026-10-01T12:00:00Z"), totalUsd: 90, realisedUsd: 50 },
      { t: Date.parse("2026-10-01T12:05:00Z"), totalUsd: 110, realisedUsd: 60 },
    ]);
  });

  test("a bot whose history starts later adds nothing before it starts", () => {
    const early = { points: [{ t: "2026-10-01T00:00:00Z", total_usd: 5 }] };
    const late = { points: [{ t: "2026-10-02T00:00:00Z", total_usd: 7 }] };
    expect(combineHistories([early, late]).map((p) => p.totalUsd)).toEqual([5, 12]);
    expect(combineHistories([])).toEqual([]);
    expect(combineHistories([{}])).toEqual([]);
  });
});

describe("tickLabel", () => {
  const t = Date.parse("2026-10-01T12:34:56Z");
  test("time for short ranges, date for long ones", () => {
    expect(tickLabel(t, "live")).toBe("12:34:56");
    expect(tickLabel(t, "1d")).toBe("12:34");
    expect(tickLabel(t, "1w")).toBe("10-01 12h");
  });
});

describe("PnlChart ranges", () => {
  const realFetch = global.fetch;
  afterEach(() => {
    global.fetch = realFetch;
  });

  test("Live uses the session data; other ranges load stored history per bot", async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ points: [] }),
    });
    render(<PnlChart live={[]} names={["btc", "eth"]} />);
    expect(screen.getByText("Collecting data…")).toBeInTheDocument();
    expect(global.fetch).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("tab", { name: "1w" }));
    await waitFor(() => expect(global.fetch).toHaveBeenCalledTimes(2));
    expect(global.fetch).toHaveBeenCalledWith("/api/bots/btc/pnl/history?range=1w", expect.anything());
    expect(await screen.findByText("No history for this range yet.")).toBeInTheDocument();
  });
});
