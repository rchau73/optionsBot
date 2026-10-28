import { fireEvent, render, screen, within } from "@testing-library/react";
import DetailsPage from "@/components/DetailsPage";
import DotStrip, { MAX_DOTS, dotsOf } from "@/components/DotStrip";
import MicroBar from "@/components/MicroBar";
import PositionsTable, { DeltaExitCell } from "@/components/PositionsTable";
import TradeHistory from "@/components/TradeHistory";
import { useMonitor } from "@/hooks/useMonitor";
import {
  approxUSD,
  coinDigits,
  countsLine,
  deltaExitRatio,
  expiryLabel,
  firstSentence,
  flipBarGeometry,
  formatCoinFirst,
  sectionCounts,
  shortLeg,
  strangleLabel,
  stuckOrders,
  workingOrderCount,
} from "@/lib/details";
import { consolidateRows, groupRows, legRows, summarise } from "@/lib/monitor";
import { track } from "@/lib/track";
import { bot, positions, status } from "@/test/fixtures";

// Details (pro) as its own page (#405): one-line rows, coin-first P&L, micro bars, compact trade log.

jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/lib/track", () => ({ track: jest.fn(() => true) }));

const withExit = (b, threshold = 0.3) => ({ ...b, summary: { delta_exit_threshold: threshold } });

/** A bot whose call has |Δ| = delta. */
function botWithDelta(delta) {
  const [call, put] = positions.strangles[0].legs;
  return withExit(bot({ positions: { ...positions, strangles: [{ ...positions.strangles[0], legs: [{ ...call, greeks: { ...call.greeks, delta } }, put] }] } }));
}

const trade = (seq, kind, extra = {}) => ({
  seq, kind, bot: "btc", unit: "BTC", at: `2026-10-0${seq}T12:00:00Z`, instrument: "BTC-27NOV26-90000-P",
  slot: { dte: 25, delta: 0.16 }, qty: 1, price: 0.01, reason: kind === "open" ? "entry" : "stop_loss", spot: 85000, dvol: 36, ...extra,
});

function mockMonitor(bots, extra = {}) {
  useMonitor.mockReturnValue({ names: bots.map((b) => b.name), bots, feed: [], history: [], trades: {}, error: null, pollMs: 1000, polledAt: Date.now(), ...extra });
}

beforeEach(() => {
  track.mockClear();
  window.localStorage.clear();
});

describe("coin-first formatting", () => {
  test("BTC with 5 decimals, ETH with 4, ≈ USD beside it", () => {
    expect(coinDigits("BTC")).toBe(5);
    expect(coinDigits("eth")).toBe(4);
    expect(coinDigits("SOL")).toBe(5);
    expect(formatCoinFirst(0.00125, "BTC")).toBe("+0.00125 BTC");
    expect(formatCoinFirst(-0.004, "ETH")).toBe("−0.0040 ETH");
    expect(formatCoinFirst(null, "BTC")).toBe("—");
    expect(approxUSD(1250.4)).toBe("≈ +$1,250");
    expect(approxUSD(-12)).toBe("≈ −$12");
    expect(approxUSD(undefined)).toBe("≈ —");
  });

  test("short leg names and expiries", () => {
    expect(shortLeg("ETH-27NOV26-2850-C")).toBe("27 Nov 2,850 C");
    expect(shortLeg("BTC-4OCT26-110000-P")).toBe("4 Oct 110,000 P");
    expect(shortLeg("XRP_USDC-27NOV26-2d5-C")).toBe("27 Nov 2.5 C");
    expect(shortLeg("odd")).toBe("odd");
    expect(expiryLabel("13NOV26")).toBe("13 Nov");
    expect(expiryLabel("NOPE")).toBeNull();
  });

  test("a strangle's group label", () => {
    const [row] = legRows([bot()]);
    expect(strangleLabel(row)).toBe("BTC · 45-day slot · expires 27 Dec · 44 days");
    expect(strangleLabel({ ...row, dte: 1.2 })).toMatch(/· 1 day$/);
  });

  test("first sentence of the plain words", () => {
    expect(firstSentence("Bought back the put. The slot is re-entered.")).toBe("Bought back the put.");
    expect(firstSentence("Sold the call at 0.30 delta")).toBe("Sold the call at 0.30 delta");
    expect(firstSentence("  ")).toBeNull();
    expect(firstSentence(undefined)).toBeNull();
  });
});

describe("MicroBar", () => {
  test("hidden from screen readers, clamped, diverging around zero, with a tick", () => {
    const { container } = render(<MicroBar value={150} min={-100} max={100} zero={0} tick={100} tone="profit" />);
    const bar = screen.getByTestId("micro-bar");
    expect(bar).toHaveAttribute("aria-hidden", "true");
    expect(bar).toHaveStyle({ width: "64px" });
    const fill = screen.getByTestId("micro-bar-fill");
    expect(fill).toHaveStyle({ left: "50%", width: "50%" }); // clamped at +100
    expect(fill).toHaveClass("bg-profit");
    expect(screen.getByTestId("micro-bar-tick")).toHaveClass("bg-warn");
    expect(container.textContent).toBe("");
  });

  test("a loss fills left of zero; no value draws no fill; width is settable", () => {
    const { rerender } = render(<MicroBar value={-50} min={-100} max={100} zero={0} tone="loss" />);
    expect(screen.getByTestId("micro-bar-fill")).toHaveStyle({ left: "25%", width: "25%" });
    rerender(<MicroBar value={null} width={120} />);
    expect(screen.queryByTestId("micro-bar-fill")).toBeNull();
    expect(screen.getByTestId("micro-bar")).toHaveStyle({ width: "120px" });
  });
});

describe("DotStrip", () => {
  test('role="img" named "5 of 9 won", one dot per close, won first', () => {
    render(<DotStrip wins={5} total={9} />);
    const strip = screen.getByRole("img", { name: "5 of 9 won" });
    const dots = [...strip.children].map((d) => d.dataset.won);
    expect(dots).toEqual(["1", "1", "1", "1", "1", "0", "0", "0", "0"]);
  });

  test("a long record is scaled to the maximum; nothing is no dot", () => {
    expect(dotsOf(30, 100)).toEqual({ won: 6, lost: MAX_DOTS - 6 });
    expect(dotsOf(0, 0)).toEqual({ won: 0, lost: 0 });
    expect(dotsOf(12, 5)).toEqual({ won: 5, lost: 0 });
    render(<DotStrip wins={0} total={0} />);
    expect(screen.getByRole("img", { name: "0 of 0 won" }).children).toHaveLength(0);
  });
});

describe("Scenario: an open leg is one line, coin first, its |Δ| bar amber from 80 % of the exit", () => {
  const render1 = (b) => render(<PositionsTable groups={groupRows(consolidateRows(legRows([b])), "strangle")} statuses={{ btc: b.status }} />);

  test("one row per leg, P&L in its coin with ≈ USD, grouped by strangle with net greeks", () => {
    render1(botWithDelta(0.12));
    const group = screen.getByTestId("group-BTC · 45-day slot · expires 27 Dec · 44 days");
    expect(group.closest("table")).toHaveClass("whitespace-nowrap"); // one line per row
    const call = screen.getByTestId("leg-btc:45d · 0.16Δ:BTC-27DEC26-115000-C:sell");
    expect(call).toHaveTextContent("+0.00030 BTC ≈ +$30");
    expect(call.querySelectorAll("div")).toHaveLength(0); // nothing stacked under a value
    const head = within(group).getAllByRole("row")[0];
    expect(head).toHaveTextContent("+0.00050 BTC ≈ +$50");
    expect(head).toHaveTextContent("net Δ 0.002"); // −(0.12 × 0.1) − (−0.14 × 0.1)
    expect(head).toHaveTextContent("2.5"); // Θ sum: −(−12 − 13) × 0.1
  });

  test.each([
    [0.239, "0"],
    [0.24, "1"],
    [0.31, "1"],
    [-0.25, "1"],
  ])("|Δ| %s against 0.30 → amber %s", (delta, warn) => {
    render1(botWithDelta(delta));
    const cell = within(screen.getByTestId("leg-btc:45d · 0.16Δ:BTC-27DEC26-115000-C:sell")).getByTestId("delta-exit");
    expect(cell).toHaveAttribute("data-warn", warn);
    expect(within(cell).getByTestId("micro-bar")).toHaveAttribute("data-tone", warn === "1" ? "warn" : "neutral");
    expect(within(cell).getByTestId("micro-bar-tick")).toBeInTheDocument();
    expect(cell).toHaveTextContent(`${Math.abs(delta).toFixed(2)} / 0.30`);
  });

  test("deltaExitRatio, and no bar without a threshold (an older bot)", () => {
    expect(deltaExitRatio(-0.24, 0.3)).toBeCloseTo(0.8);
    expect(deltaExitRatio(0.1, 0)).toBeNull();
    expect(deltaExitRatio(undefined, 0.3)).toBeNull();
    render(<DeltaExitCell delta={0.2} threshold={null} />);
    expect(screen.queryByTestId("micro-bar")).toBeNull();
    expect(screen.getByTestId("delta-exit")).toHaveTextContent("0.20");
  });

  test("a loss is red, nothing hidden", () => {
    const b = withExit(bot());
    b.positions.strangles[0].legs[0] = { ...b.positions.strangles[0].legs[0], unrealised_pnl: -0.002, roi_pct: -120 };
    render1(b);
    const call = screen.getByTestId("leg-btc:45d · 0.16Δ:BTC-27DEC26-115000-C:sell");
    expect(within(call).getByText("−0.00200 BTC").closest(".text-loss")).not.toBeNull();
    expect(within(call).getByText("-120%")).toHaveClass("text-loss");
  });

  test("a missing leg is its own row in its strangle, linking to Missing legs", () => {
    const held = { "btc:st-1": [{ key: "btc:st-1:put", missing: "put", rule: "stopped" }] };
    render(<PositionsTable groups={groupRows(consolidateRows(legRows([bot()])), "strangle")} held={held} />);
    const row = screen.getByTestId("missing-btc:st-1:put");
    expect(row).toHaveTextContent("put not held: stop hold · Missing legs");
    expect(within(row).getByRole("link", { name: "Missing legs" })).toHaveAttribute("href", "#held");
  });

  test("a stale bot's rows are dimmed with their age; a down bot's group says so", () => {
    const groups = groupRows(consolidateRows(legRows([bot()])), "strangle");
    const { rerender } = render(<PositionsTable groups={groups} health={{ btc: { stale: true, ageSec: 30, status } }} />);
    const group = screen.getByTestId(`group-${groups[0].key}`);
    expect(group).toHaveClass("opacity-60");
    expect(group).toHaveTextContent("stale · last answer 30s ago");
    rerender(<PositionsTable groups={groups} health={{ btc: { stale: true, error: "unreachable" } }} />);
    expect(screen.getByTestId(`group-${groups[0].key}`)).toHaveTextContent("bot down: unreachable");
  });
});

describe("Scenario: a trade row opens its detail", () => {
  const trades = [trade(1, "open"), trade(2, "close", { pnl: -0.002, pnl_usd: -170, roi_pct: -200, detail: "stop_loss_200pct: mark 0.02 ≥ 2 × 0.01", plain: "Bought back the put. It lost twice its premium.", delta: -0.3, theta: -10, vega: 20 })];

  test("collapsed it is one line; clicked, the detail row shows the rule and its numbers and aria-expanded is true", () => {
    render(<TradeHistory trades={trades} />);
    const row = screen.getByTestId("trade-btc-2");
    expect(row).toHaveTextContent("27 Nov 90,000 P");
    expect(row).toHaveTextContent("Stop-loss Bought back the put.");
    expect(row).not.toHaveTextContent("It lost twice");
    expect(row).toHaveTextContent("−0.00200 BTC ≈ −$170");
    const caret = within(row).getByRole("button", { name: "Show details of the close of BTC-27NOV26-90000-P" });
    expect(caret).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(caret);
    expect(caret).toHaveAttribute("aria-expanded", "true");
    expect(caret).toHaveAccessibleName("Hide details of the close of BTC-27NOV26-90000-P");
    const detail = document.getElementById(caret.getAttribute("aria-controls"));
    expect(detail).toBeVisible();
    expect(detail).toHaveTextContent("Bought back the put. It lost twice its premium.");
    expect(within(detail).getByText("Stop-loss: stop_loss_200pct: mark 0.02 ≥ 2 × 0.01")).toHaveClass("font-mono");
    expect(detail).toHaveTextContent("BTC-27NOV26-90000-P · Δ -0.300 (position 0.30) · Θ -10.0 · Vega 20.0");
    expect(track).toHaveBeenCalledWith("details_trade_expand");
    fireEvent.click(caret);
    expect(detail).not.toBeVisible();
    expect(track).toHaveBeenCalledTimes(1); // closing is not counted
  });
});

describe("lib/details", () => {
  test("countsLine: one text line, no tiles", () => {
    const kpi = summarise([bot()]);
    expect(countsLine(kpi, Date.parse("2026-10-02T12:34:56Z"))).toBe(
      "1/1 bots online · 4 orders sent · 2 filled · 1 close · 3 entries skipped · since start · updated 09:34:56 BRT",
    );
    expect(countsLine({ ...kpi, since: "2026-10-01T12:00:00Z", submitted: 1, skipped: 1, closed: 2 }, 0)).toBe(
      "1/1 bots online · 1 order sent · 2 filled · 2 closes · 1 entry skipped · since 2026-10-01 · not updated yet",
    );
  });

  test("section counts flag what needs attention", () => {
    const s = sectionCounts({ legs: 4, held: 1, trades: 9, orders: 2, stuck: 0, others: 0 });
    expect(s.map((x) => [x.id, x.n, x.attention])).toEqual([
      ["positions", 4, false],
      ["held", 1, true],
      ["trades", 9, false],
      ["orders", 2, false],
      ["others", 0, false],
    ]);
    expect(sectionCounts({ legs: 0, held: 0, trades: 0, orders: 1, stuck: 1, others: 2 }).filter((x) => x.attention).map((x) => x.id)).toEqual(["orders", "others"]);
  });

  test("working and stuck orders", () => {
    const b = bot();
    expect(workingOrderCount([b])).toBe(1);
    expect(stuckOrders([b])).toBe(0);
    expect(stuckOrders([b], Date.now() + 120_000)).toBe(1);
    expect(workingOrderCount([{ name: "x" }])).toBe(0);
  });

  test("flip bar geometry: band, flip tick and spot dot within the bar", () => {
    const g = flipBarGeometry(2545, 2476, 2.6);
    expect(g.bandLeft).toBeGreaterThan(0);
    expect(g.bandLeft + g.bandWidth).toBeLessThan(100);
    expect(g.flip).toBeGreaterThan(g.bandLeft);
    expect(g.spot).toBeGreaterThan(g.flip);
    expect(g.spot).toBeLessThan(100);
    expect(g.above).toBe(true); // 2545 > 2476 × 1.026 = 2540.4: spot is above the band
    expect(flipBarGeometry(2400, 2476, 2.6).below).toBe(true);
    expect(flipBarGeometry(null, 2476, 2)).toBeNull();
    expect(flipBarGeometry(2500, 0, 2)).toBeNull();
  });
});

describe("Details (pro) page", () => {
  const twoBots = () => [withExit(bot()), withExit(bot({ name: "eth", status: { ...status, underlying: "ETH", market: { ...status.market, spot: 2500 } }, positions: { strategy_id: "x", strangles: [] }, orders: { pending: [] } }), 0.3)];

  test("the title, one line of counts and the sticky section bar with its counts", () => {
    mockMonitor(twoBots());
    render(<DetailsPage />);
    expect(screen.getByRole("heading", { level: 1, name: "Details (pro)" })).toBeInTheDocument();
    expect(screen.getByTestId("details-counts")).toHaveTextContent(/^2\/2 bots online · 8 orders sent · 4 filled · 2 closes · 6 entries skipped · since start · updated/);
    const bar = screen.getByRole("navigation", { name: "Sections" });
    expect(bar).toHaveClass("sticky");
    const links = within(bar).getAllByRole("link");
    expect(links.map((l) => [l.getAttribute("href"), l.textContent])).toEqual([
      ["#positions", "Positions2"],
      ["#held", "Missing legs0"],
      ["#trades", "Trades0"],
      ["#orders", "Working orders1"],
      ["#others", "Not managed0"],
    ]);
    for (const id of ["positions", "held", "trades", "orders", "others"]) expect(document.getElementById(id)).not.toBeNull();
    fireEvent.click(links[1]);
    expect(track).toHaveBeenCalledWith("details_section_jump", { section: "held" });
  });

  test("missing legs > 0 gets an amber badge", () => {
    const bots = twoBots();
    bots[0] = { ...bots[0], status: { ...bots[0].status, held_legs: [{ strangle_id: "st-1", missing: "put", rule: "stopped", reason: "stopped" }] } };
    mockMonitor(bots);
    render(<DetailsPage />);
    const held = within(screen.getByRole("navigation", { name: "Sections" })).getByRole("link", { name: /Missing legs/ });
    expect(held.querySelector("[data-attention]")).toHaveAttribute("data-attention", "1");
    expect(held.querySelector("[data-attention]")).toHaveClass("text-warn");
  });

  test("Scenario: no working orders — the section is one line", () => {
    const bots = twoBots();
    bots[0] = { ...bots[0], orders: { pending: [] } };
    mockMonitor(bots);
    render(<DetailsPage />);
    const orders = document.getElementById("orders");
    expect(orders.tagName).toBe("SECTION");
    expect(orders).toHaveTextContent("Working orders (placed, not filled yet): none");
    expect(orders.querySelector("table")).toBeNull();
    expect(within(orders).getByText(/Working orders/)).toHaveAttribute("title", expect.stringMatching(/Limit orders resting on Deribit/));
  });

  test("working orders: the table, its explanation in the title's tooltip", () => {
    mockMonitor(twoBots());
    render(<DetailsPage />);
    const orders = document.getElementById("orders");
    expect(within(orders).getByRole("heading", { name: "Working orders (placed, not filled yet)" })).toHaveAttribute("title", expect.stringMatching(/fill timeout/));
    expect(within(orders).getByText("BTC-30OCT26-110000-C")).toBeInTheDocument();
  });

  test("positions grouped by strangle by default; the filters stay in the panel header", () => {
    mockMonitor(twoBots());
    render(<DetailsPage />);
    const panel = document.getElementById("positions");
    expect(within(panel).getByTestId("group-BTC · 45-day slot · expires 27 Dec · 44 days")).toBeInTheDocument();
    const header = panel.querySelector("header");
    expect(within(header).getByLabelText(/Group by/)).toHaveValue("strangle");
    expect(within(header).getByLabelText(/^Bot/)).toBeInTheDocument();
    expect(within(header).getByLabelText(/Moneyness/)).toBeInTheDocument();
  });

  test("loading: skeleton rows until the first answer", () => {
    useMonitor.mockReturnValue({ names: null, bots: [], feed: [], history: [], trades: {}, error: null, pollMs: 1000, polledAt: 0 });
    render(<DetailsPage />);
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
    expect(screen.getByTestId("details-counts")).toHaveTextContent("not updated yet");
  });

  test("a bot down says so", () => {
    mockMonitor([withExit(bot()), { name: "eth", stale: true, error: "bot eth is unreachable" }]);
    render(<DetailsPage />);
    expect(within(document.getElementById("positions")).getByRole("alert")).toHaveTextContent("ETH: bot down (bot eth is unreachable)");
  });

  test("trades from every bot, coin first", () => {
    mockMonitor(twoBots(), { trades: { btc: [trade(1, "close", { pnl: 0.001, pnl_usd: 85 })], eth: [trade(2, "close", { bot: "eth", unit: "ETH", pnl: 0.02, pnl_usd: 50 })] } });
    render(<DetailsPage />);
    const summary = screen.getByTestId("trades-summary");
    expect(summary).toHaveTextContent("+0.00100 BTC≈ +$85");
    expect(summary).toHaveTextContent("+0.0200 ETH≈ +$50");
  });
});
