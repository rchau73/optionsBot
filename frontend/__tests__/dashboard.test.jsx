import { cloneElement } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import Monitor from "@/components/Monitor";
import RegimeChart from "@/components/RegimeChart";
import DashboardTiles from "@/components/DashboardTiles";
import { RegimeLine } from "@/components/BotDashboard";
import { AccountView } from "@/components/AccountPage";
import { useMonitor } from "@/hooks/useMonitor";
import { candleRows, gexFreshness, regimeBadge, regimeLevels, regimeText, strikeLines, trendBadge, yDomain } from "@/lib/regime";

// #394: the overview as a dashboard — regime chart, strangle cards, four
// tiles and the P&L charts per bot; Account as its own page.

jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/components/PnlChart", () => () => null);
jest.mock("@/components/OverviewPnl", () => () => null);
// jsdom has no layout: give Recharts a fixed size so the chart's SVG is drawn.
jest.mock("recharts", () => {
  const actual = jest.requireActual("recharts");
  return { ...actual, ResponsiveContainer: ({ children }) => cloneElement(children, { width: 800, height: 400 }) };
});

/** 30 daily candles ending today, rising from 90,000. */
const candles = () =>
  Array.from({ length: 30 }, (_, i) => {
    const d = new Date(Date.UTC(2026, 9, 1 + i));
    const close = 90000 + i * 300;
    return { date: d.toISOString().slice(0, 10), open: close - 200, high: close + 500, low: close - 600, close };
  });

const status = (extra = {}) => ({
  as_of: new Date().toISOString(),
  underlying: "BTC",
  environment: "testnet",
  market: { spot: 98700, gex_regime: "NEGATIVE/ACCELERATION", gamma_flip: 100000, spot_to_flip_pct: -1.3 },
  flip_buffer_pct: 2,
  direction: "sma",
  trend: "bear",
  sma_trend: "bear",
  gex_bias: "down",
  gex_as_of: new Date(Date.now() - 40_000).toISOString(),
  median_flip: 0,
  sma: {
    sma9: 97000, sma21: 95000, swing_high: 99500, swing_high_date: "2026-10-20", swing_low: 92000, swing_low_date: "2026-10-08",
    price: 98700, trend: "bear", reason: "SMA9 < SMA21 and price < SMA9", candles: candles(),
  },
  risk: {
    status: { limit_im_pct: 20, max_mm_pct: 50, reason: "DVOL band", frozen: false, regime_known: true, regime_negative: true },
    im_pct: 14.2, mm_pct: 30, as_of: new Date().toISOString(),
  },
  held_legs: [{ strangle_id: "s1", missing: "put", rule: "gex_shed", reason: "shed" }],
  open_legs: 2,
  open_strangles: 1,
  pending: 0,
  uptime_sec: 86400,
  account: { equity: 1.2 },
  ...extra,
});

const positions = {
  strangles: [
    { id: "s1", slot: { dte: 25, delta: 0.16 }, legs: [{ position_id: "p1", option_type: "call", strike: 110000, instrument: "BTC-27NOV26-110000-C", qty: 0.1 }] },
    { id: "s2", slot: { dte: 50, delta: 0.16 }, legs: [
      { position_id: "p2", option_type: "put", strike: 85000, instrument: "BTC-25DEC26-85000-P", qty: 0.1 },
      { position_id: "p3", option_type: "call", strike: 110000, instrument: "BTC-25DEC26-110000-C", qty: 0.1 },
    ] },
  ],
};

const month = (m, coin, usd, closes = 2) => ({ month: m, realised_coin: coin, realised_usd: usd, closes });
const summary = (coin = "BTC") => ({
  as_of: "2026-10-27T15:00:00Z",
  underlying: coin,
  spot: 98700,
  holding: { strangles: 2, legs: 3, calls: 2, puts: 1, coin },
  income: {
    coin,
    months: [month("2026-05", 0.01, 900), month("2026-06", 0.012, 1100), month("2026-07", -0.02, -1700), month("2026-08", 0.011, 1000), month("2026-09", 0.009, 850), month("2026-10", 0.004, 410, 1)],
    this_month: month("2026-10", 0.004, 410, 1),
    unrealised_coin: -0.001,
    unrealised_usd: -98.7,
  },
  worst_case: { available: true, move_pct: 7.9, coin, model: "Black–Scholes on each leg's IV", down: { spot: 90900, loss_coin: 0.05, loss_usd: 4935, pct_of_equity: 4.2 }, up: { spot: 106500, loss_coin: 0.03, loss_usd: 2961, pct_of_equity: 2.5 } },
  last_action: { at: "2026-10-27T14:02:00Z", kind: "open", instrument: "BTC-27NOV26-110000-C", qty: 0.1, rule: "entry", reason: "Sold a call" },
});

const payoff = { as_of: new Date().toISOString(), underlying: "BTC", spot: 98700, strangles: [] };

const bot = (name = "btc", coin = "BTC") => ({ name, stale: false, status: status({ underlying: coin }), positions, summary: summary(coin), payoff });

function mockBots(bots) {
  useMonitor.mockReturnValue({ names: bots.map((b) => b.name), bots, feed: [], history: [], trades: {}, error: null, pollMs: 1000, polledAt: Date.now() });
}

beforeEach(() => window.localStorage.clear());

describe("Scenario: the Overview reads as a dashboard", () => {
  test("one band per bot (price chart, strangle rows) under the four tiles; no Activity or Account panel", () => {
    mockBots([bot("btc", "BTC"), bot("eth", "ETH")]);
    render(<Monitor role="user" />);
    const desktop = screen.getByTestId("desktop-monitor");
    for (const coin of ["BTC", "ETH"]) {
      const dash = within(desktop).getByRole("region", { name: `${coin} dashboard` });
      expect(within(dash).getByTestId(`band-chart-${coin}`)).toBeInTheDocument();
      expect(within(dash).getByRole("region", { name: `${coin} · strangles` })).toBeInTheDocument();
    }
    for (const tile of ["Coins earned this month", "Open now", "Worst case", "Margin used"]) {
      expect(within(desktop).getAllByRole("region", { name: tile })).toHaveLength(1); // one tile, a line per bot
    }
    const titles = within(desktop)
      .getAllByRole("heading", { level: 2 })
      .map((h) => h.textContent);
    expect(titles.some((t) => /^Activity$/.test(t) || /Account & collateral/.test(t))).toBe(false);
    expect(within(desktop).queryByTestId("activity-list")).toBeNull();
    expect(within(desktop).getByRole("button", { name: /Open positions/ })).toHaveAttribute("aria-expanded", "false");
    expect(within(desktop).getByRole("link", { name: "Details (pro) →" })).toHaveAttribute("href", "/?view=details");
  });

  test("the dashboards come before Details (pro)", () => {
    mockBots([bot()]);
    render(<Monitor role="manager" />);
    const dash = screen.getByRole("region", { name: "BTC dashboard" });
    const details = document.getElementById("details");
    expect(dash.compareDocumentPosition(details) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

describe("Scenario: everything about the regime is on the chart", () => {
  test("daily candles, SMA9, SMA21, the swing walls, the flip with its shed band and the held strikes", () => {
    const { container } = render(<RegimeChart status={status()} positions={positions} coin="BTC" />);
    const svg = container.querySelector("svg.recharts-surface");
    expect(svg).not.toBeNull();
    const words = [...svg.querySelectorAll("text")].map((t) => t.textContent);
    expect(words).toEqual(
      expect.arrayContaining(["flip $100,000", "swing high 10-20", "swing low 10-08", "floor 50d", "ceiling 25d/50d", "now $98,700"]),
    );
    expect(container.querySelectorAll(".recharts-reference-area").length).toBe(1); // the shed band
    expect(container.querySelectorAll(".recharts-line").length).toBe(2); // SMA9, SMA21
    expect(container.querySelectorAll(".recharts-bar-rectangle").length).toBe(30); // one candle per day
  });

  test("the legend toggles each line kind, and the text alternative states regime, trend, spot vs flip and SMAs", () => {
    const { container } = render(<RegimeChart status={status()} positions={positions} coin="BTC" />);
    const layers = screen.getByRole("group", { name: "BTC chart layers" });
    const sma9 = within(layers).getByRole("button", { name: "SMA9" });
    expect(sma9).toHaveAttribute("aria-pressed", "true");
    expect(sma9).toHaveClass("min-h-11");
    fireEvent.click(sma9);
    expect(sma9).toHaveAttribute("aria-pressed", "false");
    expect(container.querySelectorAll(".recharts-line").length).toBe(1);
    fireEvent.click(within(layers).getByRole("button", { name: "Held strikes" }));
    expect([...container.querySelectorAll("text")].map((t) => t.textContent)).not.toContain("floor 50d");
    // No median flip button outside the bias source.
    expect(within(layers).queryByRole("button", { name: /Median flip/ })).toBeNull();

    const alt = screen.getByRole("img").getAttribute("aria-label");
    expect(alt).toContain("BTC regime NEGATIVE (confirmed), put side shed");
    expect(alt).toContain("trend bear: SMA9 < SMA21 and price < SMA9");
    expect(alt).toContain("spot $98,700 below the flip $100,000 (-1.3%), shed band $98,000–$102,000");
    expect(alt).toContain("above SMA9 $97,000");
  });

  test("under the bias source the median flip is drawn and toggleable", () => {
    const { container } = render(<RegimeChart status={status({ direction: "bias", median_flip: 99000 })} positions={positions} coin="BTC" />);
    expect([...container.querySelectorAll("text")].map((t) => t.textContent)).toContain("median flip");
    expect(screen.getByRole("button", { name: "Median flip (bias)" })).toHaveAttribute("aria-pressed", "true");
  });

  test("no candles says unknown with the reason, never a flat zero chart", () => {
    const s = status();
    s.sma = { ...s.sma, candles: undefined };
    render(<RegimeChart status={s} coin="BTC" />);
    expect(screen.getByText(/Candles unknown: the bot has not reported its daily price history/)).toBeInTheDocument();
  });

  test("the badges name the regime (confirmed or live) and the trend with its reason", () => {
    render(<RegimeLine bot={bot()} coin="BTC" />);
    const line = screen.getByTestId("regime-line-btc");
    expect(line).toHaveTextContent("BTC");
    expect(line).toHaveTextContent("$98,700");
    expect(within(line).getByText("NEGATIVE · confirmed")).toBeInTheDocument();
    expect(within(line).getByText("puts shed")).toBeInTheDocument();
    expect(within(line).getByText("trend bear")).toBeInTheDocument();
    expect(within(line).getByText(/^GEX \d+s ago$/)).toBeInTheDocument();
    const why = within(line).getByRole("button", { name: "Why the BTC trend is bear" });
    expect(why).toHaveClass("min-h-11");
    fireEvent.click(why);
    expect(screen.getByRole("tooltip")).toHaveTextContent("SMA9 < SMA21 and price < SMA9");
  });

  test("a live regime the policy has not confirmed says live", () => {
    const b = bot();
    b.status = status({ market: { ...b.status.market, gex_regime: "POSITIVE/PINNING" } });
    render(<RegimeLine bot={b} coin="BTC" />);
    expect(screen.getByText("POSITIVE · live")).toBeInTheDocument();
  });
});

describe("the four stat tiles", () => {
  test("numbers with a sign, a unit and a time; the sentences in each tile's tooltip", () => {
    render(<DashboardTiles bot={bot()} />);
    const month = screen.getByRole("region", { name: "This month" });
    expect(month).toHaveTextContent("+0.00400 BTC");
    expect(month).toHaveTextContent(/≈ \+\$410 · 1–27 Oct · as of \d\d:\d\d/);
    expect(within(month).getByRole("img", { name: "Last 6 months: 5 up, July down" })).toBeInTheDocument();
    fireEvent.click(within(month).getByRole("button", { name: "About This month" }));
    expect(within(month).getByRole("tooltip")).toHaveTextContent(/after Deribit's fees/);

    expect(screen.getByRole("region", { name: "Open now" })).toHaveTextContent("−0.00100 BTC");
    const worst = screen.getByRole("region", { name: "Worst case" });
    expect(worst).toHaveTextContent("−0.05000 BTC");
    expect(worst).toHaveTextContent("≈ −$4,935 · −4.2% of account · a very bad day (BTC −7.9%)");
    expect(within(worst).getByRole("link", { name: /Five days in money/ })).toHaveAttribute("href", "/worst-case?bot=btc");

    const margin = screen.getByRole("region", { name: "Margin used" });
    expect(margin).toHaveTextContent("71% of limit");
    expect(margin).toHaveTextContent("IM 14.2% of 20% limit");
    expect(within(margin).getByRole("meter")).toHaveAttribute("aria-valuenow", "14.2");
    expect(within(margin).getByTestId("limit-marker")).toBeInTheDocument();
    expect(margin).toHaveTextContent("MM 30.0% of 50% cap"); // over half its cap
  });

  test("missing data says unknown with the reason, never 0", () => {
    const b = bot();
    b.summary = null;
    b.summaryError = "bot unreachable";
    b.status = status({ risk: { status: { limit_im_pct: 20, max_mm_pct: 50 }, error: "no margin data" } });
    render(<DashboardTiles bot={b} />);
    for (const tile of ["This month", "Open now", "Worst case"]) {
      expect(screen.getByRole("region", { name: tile })).toHaveTextContent("unknown: bot unreachable");
    }
    expect(screen.getByRole("region", { name: "Margin used" })).toHaveTextContent("unknown: no margin data");
    expect(screen.getByRole("region", { name: "Margin used" })).not.toHaveTextContent("MM");
  });
});

describe("Scenario: Account is a page in the menu", () => {
  test("the balances, margin policy, IM/MM meters and Rebalance appear on their own page", () => {
    const b = bot();
    b.account = {
      snapshot: {
        as_of: new Date().toISOString(),
        margin_model: "cross_pm",
        cross_collateral: true,
        totals: { equity_usd: 150000, margin_balance_usd: 150000, initial_margin_usd: 30000, maintenance_margin_usd: 21000, im_pct: 20, mm_pct: 14 },
        assets: [{ currency: "BTC", balance: 1.2, equity: 1.25, margin_balance: 1.25, im_pct: 24, mm_pct: 16 }],
        positions: [],
      },
    };
    render(<AccountView bots={[b]} polledAt={Date.now()} />);
    expect(screen.getByRole("heading", { level: 1, name: "Account & collateral (shared by all bots)" })).toBeInTheDocument();
    expect(screen.getByText("Equity (all collateral)").nextSibling).toHaveTextContent("$150,000");
    expect(screen.getByRole("meter", { name: "IM used" })).toBeInTheDocument();
    expect(screen.getByRole("meter", { name: /MM used/ })).toBeInTheDocument();
    expect(screen.getByText("Margin policy per bot")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Rebalance" })).toBeEnabled();
    expect(screen.getByRole("region", { name: "Not managed by the bot" })).toBeInTheDocument();
  });
});

describe("lib/regime", () => {
  test("candleRows: SMAs of the completed closes before each day; bad candles dropped", () => {
    const s = status();
    const rows = candleRows(s);
    expect(rows).toHaveLength(30);
    expect(rows[8].sma9).toBeNull();
    const want9 = s.sma.candles.slice(20, 29).reduce((a, c) => a + c.close, 0) / 9;
    expect(rows[29].sma9).toBeCloseTo(want9, 6);
    expect(rows[29].range).toEqual([rows[29].low, rows[29].high]);
    expect(candleRows({ sma: { candles: [{ date: "2026-10-01", close: 0 }, { date: "2026-10-02", close: 5 }] } })).toEqual([
      { date: "2026-10-02", open: 5, high: 5, low: 5, close: 5, range: [5, 5], up: true, sma9: null, sma21: null },
    ]);
    expect(candleRows({})).toEqual([]);
  });

  test("strikeLines: floor/ceiling with their slots, one line per strike", () => {
    expect(strikeLines(positions)).toEqual([
      { price: 85000, side: "put", label: "floor 50d" },
      { price: 110000, side: "call", label: "ceiling 25d/50d" },
    ]);
    expect(strikeLines(null)).toEqual([]);
  });

  test("regimeLevels, yDomain", () => {
    const l = regimeLevels(status({ direction: "bias", median_flip: 99000 }));
    expect(l.flip).toEqual({ price: 100000, low: 98000, high: 102000, bufferPct: 2 });
    expect(l.medianFlip).toBe(99000);
    expect(l.swingHigh).toEqual({ price: 99500, date: "2026-10-20", label: "swing high 10-20" });
    expect(regimeLevels(status()).medianFlip).toBeNull(); // sma source
    const [lo, hi] = yDomain([{ low: 100, high: 110 }], [120, null, 0]);
    expect(lo).toBeLessThan(100);
    expect(hi).toBeGreaterThan(120);
    expect(yDomain([], [])).toBeNull();
  });

  test("badges and freshness: unknown says why", () => {
    expect(regimeBadge({})).toMatchObject({ word: null, unknown: "unknown: no GEX snapshot yet" });
    expect(regimeBadge(status()).shed).toEqual(["put"]);
    expect(trendBadge({})).toEqual({ trend: null, reason: "unknown: no trend reported yet", source: null });
    expect(trendBadge(status({ direction: "bias", trend: "bear" })).reason).toMatch(/^GEX bias down/);
    expect(gexFreshness({ gex_as_of: null })).toEqual({ ageSec: null, text: "GEX unknown: no snapshot yet", stale: true });
    expect(gexFreshness({ gex_as_of: new Date(Date.now() - 600_000).toISOString() }).stale).toBe(true);
    expect(regimeText({}, "ETH")).toBe("ETH regime unknown: no GEX snapshot yet; trend unknown: no trend reported yet; spot unknown.");
  });
});
