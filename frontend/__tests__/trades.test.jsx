import { fireEvent, render, screen } from "@testing-library/react";
import TradeHistory, { tradeHistorySummary } from "@/components/TradeHistory";
import { closeLabel, filterTrades, mergeTrades, summarizeTrades } from "@/lib/trades";
import { prefillLive, summarise } from "@/lib/monitor";

const t = (seq, kind, extra = {}) => ({
  seq, kind, bot: "btc", unit: "BTC", at: `2026-10-0${Math.min(seq, 9)}T12:00:00Z`, instrument: `BTC-X-${seq}`,
  slot: { dte: 25, delta: 0.16 }, qty: 1, price: 0.01, reason: kind === "open" ? "entry" : "roi_target", spot: 85000, dvol: 36, ...extra,
});
const trades = [
  t(1, "open"),
  t(2, "open", { slot: { dte: 60, delta: 0.18 } }),
  t(3, "close", { pnl: 0.004, pnl_usd: 340, roi_pct: 30, hold_days: 5 }),
  t(4, "close", { pnl: -0.002, pnl_usd: -170, reason: "stop_loss", slot: { dte: 60, delta: 0.18 } }),
  t(5, "close", { pnl: 0.001, pnl_usd: 85, bot: "eth", unit: "ETH" }),
];

describe("trade statistics", () => {
  test("realized P&L, win rate, average win vs loss", () => {
    const s = summarizeTrades(trades);
    expect(s).toMatchObject({ opens: 2, closes: 3, realisedUsd: 255, wins: 2, losses: 1 });
    expect(s.winRate).toBeCloseTo(66.67, 1);
    expect(s.avgWinUsd).toBeCloseTo(212.5);
    expect(s.avgLossUsd).toBe(-170);
    expect(s.realisedByCoin).toEqual({ BTC: 0.002, ETH: 0.001 });
  });

  test("grouped by exit reason and by slot, worst first", () => {
    const s = summarizeTrades(trades);
    expect(s.byReason.map((g) => [g.key, g.closes, g.pnlUsd])).toEqual([
      ["Stop-loss", 1, -170],
      ["Take-profit", 2, 425],
    ]);
    expect(s.bySlot[0]).toMatchObject({ key: "BTC 60d · Δ0.18", pnlUsd: -170 });
  });

  test("no closes yet: no rates", () => {
    expect(summarizeTrades([t(1, "open")])).toMatchObject({ closes: 0, winRate: null, avgWinUsd: null, avgLossUsd: null });
  });

  test("filters, newest first", () => {
    expect(filterTrades(trades, { kind: "close" }).map((x) => x.seq)).toEqual([5, 4, 3]);
    expect(filterTrades(trades, { bot: "eth" }).map((x) => x.seq)).toEqual([5]);
    expect(filterTrades(trades, { reason: "Stop-loss" }).map((x) => x.seq)).toEqual([4]);
    expect(closeLabel("delta_exit")).toBe("Delta exit");
  });

  test("pages merge by journal sequence", () => {
    expect(mergeTrades(trades.slice(0, 2), trades.slice(1, 4)).map((x) => x.seq)).toEqual([1, 2, 3, 4]);
    expect(mergeTrades(trades, [])).toBe(trades);
  });
});

describe("TradeHistory", () => {
  test("summary tiles and rows; filters work", () => {
    render(<TradeHistory trades={trades} />);
    expect(screen.getByText("67%")).toBeInTheDocument(); // win rate
    expect(screen.getByText("5 rows")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/Type/), { target: { value: "close" } });
    expect(screen.getByText("3 rows")).toBeInTheDocument();
  });

  test("pages long histories", () => {
    const many = Array.from({ length: 120 }, (_, i) => t(i + 1, "close", { pnl: 0.001, pnl_usd: 1 }));
    render(<TradeHistory trades={many} />);
    fireEvent.click(screen.getByText("Show 50 more"));
    expect(screen.getByText("Show 20 more")).toBeInTheDocument();
  });

  test("empty and collapsed summary", () => {
    render(<TradeHistory trades={[]} />);
    expect(screen.getByText(/No opens or closes/)).toBeInTheDocument();
    expect(tradeHistorySummary(trades)).toBe("2 opens · 3 closes · realized $255 · 67% wins");
    expect(tradeHistorySummary([])).toBe("no trades yet");
  });
});

describe("history carried into the KPIs and the chart", () => {
  test("KPIs know since when the history runs (earliest bot)", () => {
    const bot = (since) => ({ name: "x", status: { history_since: since, event_counts: {}, pnl: [] } });
    expect(summarise([bot("2026-10-04T12:00:00Z"), bot("2026-10-03T11:00:00Z")]).since).toBe("2026-10-03T11:00:00Z");
    expect(summarise([bot(null)]).since).toBeNull();
  });

  test("Live starts from stored history, then this session", () => {
    const stored = [{ t: 1 }, { t: 2 }, { t: 3 }];
    expect(prefillLive(stored, [{ t: 3 }, { t: 4 }]).map((p) => p.t)).toEqual([1, 2, 3, 4]);
    expect(prefillLive(stored, []).map((p) => p.t)).toEqual([1, 2, 3]);
  });
});

describe("positionDelta", () => {
  it("is −Δ × qty for the short: a short call is negative, a short put positive", () => {
    const { positionDelta } = require("@/lib/trades");
    expect(positionDelta({ delta: 0.17, qty: 100 })).toBeCloseTo(-17, 10);
    expect(positionDelta({ delta: -0.2, qty: 162 })).toBeCloseTo(32.4, 10);
    expect(positionDelta({ qty: 1 })).toBeNull();
  });
});
