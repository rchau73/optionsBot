import { render, screen, within } from "@testing-library/react";
import { fetchBotState } from "@/lib/api/bots";
import { PositionsScreen } from "@/components/PositionsPage";
import BookPayoffPanel from "@/components/BookPayoffPanel";
import HedgeLine, { HedgeLines } from "@/components/HedgeLine";
import HedgeRows from "@/components/HedgeRows";
import OverviewTiles from "@/components/OverviewTiles";
import { sectionCounts } from "@/lib/details";
import { unmanagedPositions } from "@/lib/account";
import { HEDGE_STALE_SEC, bookChartLabel, bookChartPoints, hedgeShown, hedgeStaleAge, hedgeState, pausedWords, pricedAtIndex } from "@/lib/futuresHedge";
import { netDeltaLines } from "@/lib/tiles";

// The futures hedge on Overview, Details and Positions (#666). The shapes are
// those of GET /api/bots/<bot>/hedge; the −10 % row is the issue's scenario.

const NOW = Date.parse("2026-11-05T20:22:40Z");

const hedge = (over = {}) => ({
  available: true,
  enabled: true,
  halted: false,
  testnet: true,
  underlying: "BTC",
  instrument: "BTC-PERPETUAL",
  contract_usd: 10,
  perp_usd: -32310,
  perp_delta_coin: -0.42,
  perp_avg_price: 76100,
  perp_mark_price: 76030,
  perp_as_of: "2026-11-05T20:22:30Z",
  unrealised_coin: -0.0012,
  realised_coin: 0.002,
  fees_coin: 0.0005,
  funding_coin: 0.0001,
  options_delta_coin: 0.42,
  net_delta_coin: 0,
  band_coin: 0.0578,
  orders_last_hour: 1,
  max_orders_per_hour: 12,
  benchmark: { real_pnl_coin: -0.0012 },
  book_scenarios: [
    { move_pct: -10, spot: 68427, options_coin: -0.09784, options_usd: -6695, book_coin: -0.05062, book_usd: -3464 },
    { move_pct: -5, spot: 72228, options_coin: -0.0369, options_usd: -2665, book_coin: -0.0145, book_usd: -1050 },
    { move_pct: 5, spot: 79831, options_coin: 0.0134, options_usd: 1066, book_coin: -0.0068, book_usd: -549 },
    { move_pct: 10, spot: 83633, options_coin: 0.0144, options_usd: 1200, book_coin: -0.0242, book_usd: -2031 },
  ],
  ...over,
});

const bot = (over = {}) => ({
  name: "btc",
  status: { underlying: "BTC" },
  summary: { underlying: "BTC", spot: 76030, holding: { legs: 2 } },
  payoff: { bot_net_delta: 0.42 },
  positions: { strangles: [] },
  ...over,
});

describe("Scenario: the hedge is not 'not managed'", () => {
  const perp = { instrument: "BTC-PERPETUAL", currency: "BTC", kind: "future", size: -32310, total_pnl: -0.0012, delta: -0.42, initial_margin: 0.1, maintenance_margin: 0.05 };
  const account = (positions) => ({ snapshot: { positions } });

  test("Details shows the perpetual as the bot's futures hedge, and Not managed does not list it", () => {
    const bots = [bot({ hedge: hedge() })];
    render(<HedgeRows bots={bots} now={NOW} />);
    const row = screen.getByTestId("hedge-row-btc");
    expect(row).toHaveTextContent("BTC · Futures hedge · BTC-PERPETUAL");
    expect(row).toHaveTextContent("the bot's own position");
    expect(row).toHaveTextContent("−$32,310");
    expect(row).toHaveTextContent("short · 3,231 contracts");
    expect(unmanagedPositions(account([perp]), bots).rows).toEqual([]);
  });

  test("only what is beyond the hedge's own size stays listed, its share of the figures", () => {
    const rows = unmanagedPositions(account([{ ...perp, size: -42310 }]), [bot({ hedge: hedge() })]).rows;
    expect(rows).toHaveLength(1);
    expect(rows[0].size).toBe(-10000);
    expect(rows[0].delta).toBeCloseTo(-0.42 * (10000 / 42310), 6);
  });

  test.each([
    ["the hedge is off and flat", hedge({ enabled: false, perp_usd: 0 })],
    ["the hedge cannot be read", null],
  ])("the whole perpetual stays listed when %s", (_, h) => {
    expect(unmanagedPositions(account([perp]), [bot({ hedge: h })]).rows).toEqual([perp]);
  });

  test("the row names funding as left out when the benchmark is priced at the index, and a live price otherwise", () => {
    const { rerender } = render(<HedgeRows bots={[bot({ hedge: hedge({ benchmark: { pricing: "index" } }) })]} now={NOW} />);
    expect(screen.getByTestId("hedge-row-btc")).toHaveTextContent("left out on testnet");
    rerender(<HedgeRows bots={[bot({ hedge: hedge({ testnet: false, benchmark: { pricing: "exchange" } }) })]} now={NOW} />);
    expect(screen.getByTestId("hedge-row-btc")).not.toHaveTextContent("left out on testnet");
    expect(screen.getByTestId("hedge-row-btc")).toHaveTextContent("+0.0001 BTC");
  });

  test("the section bar has the hedge only while one is shown, amber unless it is simply on", () => {
    const base = { legs: 2, held: 0, trades: 0, orders: 0, stuck: 0, others: 0 };
    expect(sectionCounts(base).map((s) => s.id)).not.toContain("hedge");
    expect(sectionCounts({ ...base, hedges: 1, hedgeAttention: true })[1]).toEqual({ id: "hedge", label: "Futures hedge", n: 1, attention: true });
  });
});

describe("Scenario: Overview shows the net delta with the hedge", () => {
  test("net delta ≈ 0 with options and perpetual under it, and the hedge line", () => {
    render(
      <>
        <OverviewTiles bots={[bot({ hedge: hedge() })]} />
        <HedgeLines bots={[bot({ hedge: hedge() })]} now={NOW} />
      </>,
    );
    const tile = screen.getByRole("region", { name: "Net delta" });
    expect(tile).toHaveTextContent("0.0000 BTC");
    expect(tile).toHaveTextContent("options +0.4200 · hedge −0.4200");
    const line = screen.getByTestId("hedge-line-btc");
    expect(line).toHaveTextContent("Futures hedge ON");
    expect(line).toHaveTextContent("−$32,310 · −0.4200 BTC");
    expect(line).toHaveTextContent("P&L −0.0012 BTC (−$91) net of fees");
    expect(line).toHaveTextContent("priced at the index (testnet)");
    expect(line.closest("a")).toHaveAttribute("href", "/details#hedge");
  });

  test("a tiny net never reads as −0.0000", () => {
    expect(netDeltaLines([bot({ hedge: hedge({ net_delta_coin: -0.00001 }) })])[0].text).toBe("0.0000 BTC");
  });

  test("the phone's line carries the net delta, having no tile", () => {
    render(<HedgeLine bot={bot({ hedge: hedge({ net_delta_coin: -0.0006 }) })} now={NOW} net />);
    expect(screen.getByTestId("hedge-line-btc")).toHaveTextContent("net delta −0.0006 BTC");
  });
});

describe("Scenario: paused is visible on Overview", () => {
  test("the card shows the pause, the reason and that the GEX shed protects the book", () => {
    render(<HedgeLines bots={[bot({ hedge: hedge({ halted: true, halt_reason: "Deribit maintenance" }) })]} now={NOW} />);
    const line = screen.getByTestId("hedge-line-btc");
    expect(line).toHaveTextContent("BTC · Futures hedge paused");
    expect(line).toHaveTextContent("Deribit maintenance. The GEX shed protects the book meanwhile.");
  });

  test("a pause without a reason claims nothing about the shed: the kill switch leaves the bot idle", () => {
    const h = hedge({ halted: true });
    expect(hedgeState(h)).toBe("paused");
    expect(pausedWords(h)).toBeNull();
    render(<HedgeLines bots={[bot({ hedge: h })]} now={NOW} />);
    expect(screen.getByTestId("hedge-line-btc")).not.toHaveTextContent("GEX shed");
  });
});

describe("Scenario: off and flat shows nothing", () => {
  const off = hedge({ enabled: false, perp_usd: 0, perp_delta_coin: 0, net_delta_coin: 0.42 });

  test("no line, no row, no card, and the tile says options only", () => {
    const { container } = render(
      <>
        <HedgeLines bots={[bot({ hedge: off })]} now={NOW} />
        <HedgeRows bots={[bot({ hedge: off })]} now={NOW} />
        <PositionsScreen names={["btc"]} bot="btc" payoff={{ underlying: "BTC", strangles: [] }} hedge={off} />
      </>,
    );
    expect(container).not.toHaveTextContent("Futures hedge");
    expect(screen.queryByTestId("book-payoff")).not.toBeInTheDocument();
    render(<OverviewTiles bots={[bot({ hedge: off })]} />);
    expect(screen.getByRole("region", { name: "Net delta" })).toHaveTextContent("+0.4200 BTC");
    expect(screen.getByRole("region", { name: "Net delta" })).toHaveTextContent("options only");
  });

  test("nothing held reads nothing open, never a 0", () => {
    const line = netDeltaLines([bot({ hedge: off, summary: { underlying: "BTC", holding: { legs: 0 } } })])[0];
    expect(line.text).toBe("nothing open");
    expect(line.note).toBe("options only");
  });
});

describe("the other states", () => {
  test("off with a position open says so on every surface", () => {
    const h = hedge({ enabled: false });
    expect(hedgeState(h)).toBe("off_open");
    render(
      <>
        <HedgeLines bots={[bot({ hedge: h })]} now={NOW} />
        <HedgeRows bots={[bot({ hedge: h })]} now={NOW} />
        <PositionsScreen names={["btc"]} bot="btc" payoff={{ underlying: "BTC", strangles: [] }} hedge={h} />
      </>,
    );
    expect(screen.getByTestId("hedge-line-btc")).toHaveTextContent("Futures hedge OFF · position still open");
    expect(screen.getByTestId("hedge-row-btc")).toHaveTextContent("OFF · position still open");
    expect(screen.getByTestId("hedge-card")).toHaveTextContent("OFF · position still open");
  });

  test("an API error: the tile falls back to the options and names it, never a blank", () => {
    render(<OverviewTiles bots={[bot({ hedge: null, hedgeError: "502" })]} />);
    const tile = screen.getByRole("region", { name: "Net delta" });
    expect(tile).toHaveTextContent("+0.4200 BTC");
    expect(tile).toHaveTextContent("options only · hedge state unavailable");
  });

  test("an API error with no options reading is unknown, not 0", () => {
    const line = netDeltaLines([bot({ hedge: null, hedgeError: "502", payoff: null, payoffError: "timeout" })])[0];
    expect(line.text).toBe("unknown: timeout");
  });

  test("an API error shows on Details and Positions", () => {
    render(<HedgeRows bots={[bot({ hedge: null, hedgeError: "502" })]} now={NOW} />);
    expect(screen.getByRole("alert")).toHaveTextContent("BTC: hedge state unavailable (502).");
    render(<PositionsScreen names={["btc"]} bot="btc" payoff={{ underlying: "BTC", strangles: [] }} hedgeError="502" />);
    expect(screen.getByText(/Futures hedge: hedge state unavailable \(502\)/)).toBeInTheDocument();
  });

  test("a perpetual read over two cycles ago is dimmed with its age", () => {
    const h = hedge({ perp_as_of: new Date(NOW - (HEDGE_STALE_SEC + 40) * 1000).toISOString() });
    expect(hedgeStaleAge(hedge(), NOW)).toBeNull();
    expect(hedgeStaleAge(h, NOW)).toBeGreaterThan(HEDGE_STALE_SEC);
    expect(hedgeStaleAge({ ...h, enabled: false }, NOW)).toBeNull(); // read only while on
    render(<HedgeLines bots={[bot({ hedge: h })]} now={NOW} />);
    expect(screen.getByTestId("hedge-line-btc")).toHaveTextContent("as of 2m ago");
    expect(screen.getByTestId("hedge-line-btc").className).toContain("opacity-60");
  });

  test("the pricing line follows the benchmark's flag, else testnet", () => {
    expect(pricedAtIndex(hedge())).toBe(true);
    expect(pricedAtIndex(hedge({ testnet: false }))).toBe(false);
    expect(pricedAtIndex(hedge({ testnet: true, benchmark: { pricing: "exchange" } }))).toBe(false);
    expect(hedgeShown(null)).toBe(false);
  });

  test("the bot's poll carries the hedge, and keeps the bot when only the hedge fails", async () => {
    const realFetch = global.fetch;
    const answer = (url) => (url.endsWith("/hedge") ? { hedge: { enabled: true } } : { url });
    try {
      global.fetch = jest.fn(async (url) => ({ ok: true, status: 200, json: async () => answer(url) }));
      expect((await fetchBotState("btc")).hedge).toEqual({ enabled: true });
      global.fetch = jest.fn(async (url) =>
        url.endsWith("/hedge") ? { ok: false, status: 502, json: async () => ({ error: "bot btc is unreachable" }) } : { ok: true, status: 200, json: async () => ({ url }) },
      );
      const state = await fetchBotState("btc");
      expect(state.status).toEqual({ url: "/api/bots/btc/status" });
      expect(state.hedge).toBeNull();
      expect(state.hedgeError).toBe("bot btc is unreachable");
    } finally {
      global.fetch = realFetch;
    }
  });
});

describe("Scenario: the combined payoff includes the hedge", () => {
  test("the −10 % row shows the book with the hedge and the options alone", () => {
    render(<PositionsScreen names={["btc"]} bot="btc" payoff={{ underlying: "BTC", strangles: [] }} hedge={hedge()} />);
    const row = within(screen.getByTestId("book-row--10"));
    expect(row.getByText("−10 %")).toBeInTheDocument();
    expect(screen.getByTestId("book-row--10")).toHaveTextContent("−0.0506 BTC (−$3,464)");
    expect(screen.getByTestId("book-row--10")).toHaveTextContent("−0.0978 BTC (−$6,695)");
    expect(screen.getByText(/takes out the slope \(delta\), not the curve \(gamma\)/)).toBeInTheDocument();
    expect(screen.getByTestId("hedge-card")).toHaveTextContent("BTC-PERPETUAL");
  });

  test("the chart's label has the four moves and its line passes through zero", () => {
    const s = hedge().book_scenarios;
    render(<BookPayoffPanel h={hedge()} coin="BTC" />);
    const label = screen.getByRole("img").getAttribute("aria-label");
    expect(label).toBe(bookChartLabel(s));
    for (const v of ["−10 %: −$3,464 with the hedge, −$6,695 options only", "+10 %: −$2,031 with the hedge, +$1,200 options only"]) expect(label).toContain(v);
    expect(bookChartPoints(s).map((p) => p.move)).toEqual([-10, -5, 0, 5, 10]);
    expect(bookChartPoints(s)[2]).toEqual({ move: 0, book: 0, options: 0 });
  });

  test("a loss in the table is more than its colour: it keeps its minus sign", () => {
    render(<BookPayoffPanel h={hedge()} coin="BTC" />);
    expect(screen.getByTestId("book-row-10")).toHaveTextContent("−$2,031");
  });

  test("a leg without greeks says not yet, instead of an empty chart", () => {
    render(<BookPayoffPanel h={hedge({ book_scenarios: [] })} coin="BTC" />);
    expect(screen.getByText(/Not yet: a leg has no greeks/)).toBeInTheDocument();
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });
});
