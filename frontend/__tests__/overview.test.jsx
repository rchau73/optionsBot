import { fireEvent, render, screen, within } from "@testing-library/react";
import FirstDayPanel from "@/components/FirstDayPanel";
import { BotSummary } from "@/components/InvestorSummary";
import Monitor from "@/components/Monitor";
import PhoneOverview, { RangeBar } from "@/components/PhoneOverview";
import { useMonitor } from "@/hooks/useMonitor";
import {
  activitySummary,
  firstDaySteps,
  isFirstDay,
  monthBars,
  monthSoFar,
  moveSince,
  ordersSummary,
  plainFreeze,
  positionsSummary,
  rangeBar,
  startedAgo,
  stateWords,
  warningChips,
  worstCaseLine,
} from "@/lib/overview";

// Phone overview (#320), first-day panel (#321), panels folded for a user
// (#288). The charts and the poller are stubbed: Monitor reads its bots from
// the mocked useMonitor.
jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/components/PnlChart", () => () => null);
jest.mock("@/components/CoinPnlChart", () => () => null);

const month = (m, coin, usd, closes = 2) => ({ month: m, realised_coin: coin, realised_usd: usd, closes });

// GET /api/summary (internal/api/summary.go).
const summary = (coin = "BTC", overrides = {}) => ({
  as_of: "2026-10-19T12:00:00Z",
  underlying: coin,
  environment: "testnet",
  spot: 100000,
  holding: {
    strangles: 2,
    legs: 4,
    calls: 2,
    puts: 2,
    nearest_expiry: "2026-10-30T08:00:00Z",
    nearest: { expiry: "2026-10-30T08:00:00Z", put_strike: 90000, call_strike: 110000 },
    coin,
  },
  income: {
    coin,
    months: [
      month("2026-05", 0.01, 900),
      month("2026-06", 0.012, 1100),
      month("2026-07", -0.02, -1700),
      month("2026-08", 0.011, 1000),
      month("2026-09", 0.009, 850),
      month("2026-10", 0.004, 410, 1),
    ],
    this_month: month("2026-10", 0.004, 410, 1),
    unrealised_coin: -0.001,
    unrealised_usd: -100,
  },
  worst_case: {
    available: true,
    move_pct: 7.9,
    coin,
    down: { spot: 92100, loss_coin: 0.05, loss_usd: 4605, pct_of_equity: 4.2 },
    up: { spot: 107900, loss_coin: 0.03, loss_usd: 3237, pct_of_equity: 2.5 },
  },
  last_action: {
    at: "2026-10-19T14:02:00Z",
    kind: "close",
    instrument: "BTC-30OCT26-90000-P",
    qty: 0.5,
    rule: "roi_target",
    reason: "Bought back the put: it kept most of its premium (take-profit)",
    pnl_coin: 0.002,
    spot: 98000,
  },
  ...overrides,
});

const status = (coin, extra = {}) => ({
  as_of: new Date().toISOString(),
  underlying: coin,
  environment: "testnet",
  uptime_sec: 20 * 60,
  market: { spot: 100000, gex_regime: "POSITIVE/PINNING" },
  account: { equity: 1.25 },
  risk: { status: { frozen: false }, entries_allowed: true },
  open_legs: 4,
  open_strangles: 2,
  pending: 1,
  ...extra,
});

const twoBots = () => [
  { name: "btc", stale: false, status: status("BTC"), summary: summary("BTC") },
  { name: "eth", stale: false, status: status("ETH"), summary: summary("ETH", { spot: 3500, holding: { ...summary("ETH").holding, nearest: { expiry: "2026-10-30T08:00:00Z", put_strike: 3200, call_strike: 3800 } } }) },
];

const brandNew = (extra = {}) => ({
  name: "btc",
  stale: false,
  status: status("BTC", { open_legs: 0, open_strangles: 0, pending: 0, ...extra }),
  summary: summary("BTC", { holding: { strangles: 0, legs: 0, calls: 0, puts: 0, nearest: null, coin: "BTC" }, last_action: null }),
});

function mockBots(bots, feed = []) {
  useMonitor.mockReturnValue({ names: bots.map((b) => b.name), bots, feed, history: [], trades: {}, error: null, pollMs: 1000, polledAt: Date.now() });
}

beforeEach(() => window.localStorage.clear());

/** The titles of the panel toggles (a button holding the panel's h2) that are open or folded. */
const panelTitles = (container, expanded) =>
  within(container)
    .queryAllByRole("button", { expanded })
    .map((b) => b.querySelector("h2")?.textContent)
    .filter(Boolean);

describe("Scenario: phone overview", () => {
  test("one bot with a BTC | ETH switch, the month's result as the headline and the four cards", () => {
    mockBots(twoBots());
    render(<Monitor role="user" />);
    const phone = screen.getByTestId("phone-overview");
    // Phone only: hidden from 640 px; the desktop monitor is hidden under it.
    expect(phone.parentElement).toHaveClass("sm:hidden");
    expect(screen.getByTestId("desktop-monitor")).toHaveClass("hidden", "sm:block");

    const coin = within(phone).getByRole("group", { name: "Coin" });
    const btc = within(coin).getByRole("button", { name: "BTC" });
    const eth = within(coin).getByRole("button", { name: "ETH" });
    expect(btc).toHaveAttribute("aria-pressed", "true");
    expect(eth).toHaveAttribute("aria-pressed", "false");
    expect(btc).toHaveClass("min-h-11", "min-w-11"); // ≥ 44 px

    const h1 = within(phone).getByRole("heading", { level: 1 });
    expect(h1).toHaveTextContent("This month: +0.00400 BTC after fees");
    expect(within(phone).getByText(/About \+\$410 · 1–19 Oct · running normally/)).toBeInTheDocument();
    for (const card of ["What it holds", "Making or losing", "Worst case right now", "Last action · 11:02"]) {
      expect(within(phone).getByRole("region", { name: card })).toBeInTheDocument();
    }
    expect(within(phone).getByText(/Not investment advice/)).toBeInTheDocument();

    // One bot at a time: switching shows ETH and records ?bot= in the URL.
    fireEvent.click(eth);
    expect(within(phone).getByRole("button", { name: "ETH" })).toHaveAttribute("aria-pressed", "true");
    expect(within(phone).getByRole("heading", { level: 1 })).toHaveTextContent("+0.00400 ETH");
    expect(window.location.search).toBe("?bot=eth");
  });

  test("?bot= picks the bot shown", () => {
    mockBots(twoBots());
    render(<Monitor role="user" bot="eth" />);
    expect(within(screen.getByTestId("phone-overview")).getByRole("button", { name: "ETH" })).toHaveAttribute("aria-pressed", "true");
  });

  test("the cards' figures and links", () => {
    render(<PhoneOverview bots={twoBots()} selected="btc" />);
    const making = screen.getByRole("region", { name: "Making or losing" });
    expect(making).toHaveTextContent("This month+0.00400 BTC");
    expect(making).toHaveTextContent("Open now−0.00100 BTC");
    // Six month bars; the loss is hatched and named in the label, not colour alone.
    const bars = within(making).getByRole("img", { name: "Last 6 months: 5 up, July down" });
    const loss = bars.querySelectorAll("[data-loss]");
    expect(loss).toHaveLength(1);
    expect(loss[0]).toHaveClass("hatch-loss");
    expect(within(making).getByRole("link", { name: /Month by month/ })).toHaveAttribute("href", "/income?bot=btc");

    const worst = screen.getByRole("region", { name: "Worst case right now" });
    expect(worst).toHaveClass("border-loss/60");
    expect(worst).toHaveTextContent("A very bad day (BTC −7.9 %) would cost about 0.05000 BTC ($4,605), 4.2 % of the account.");
    expect(within(worst).getByRole("link", { name: /What the bot does then/ })).toHaveAttribute("href", "/worst-case?bot=btc");

    const last = screen.getByRole("region", { name: /Last action/ });
    expect(last).toHaveTextContent("Bought back the put: it kept most of its premium (take-profit)");
    expect(last).toHaveTextContent("BTC +2.0 % since · result +0.00200 BTC");
    expect(within(last).getByRole("button", { name: /All actions, explained/ })).toBeInTheDocument();
  });

  test("the menu opens the other views and Details", () => {
    const onDetails = jest.fn();
    render(<PhoneOverview bots={twoBots()} selected="btc" onDetails={onDetails} />);
    const menu = screen.getByRole("button", { name: "Menu" });
    expect(menu).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(menu);
    const nav = screen.getByRole("navigation", { name: "Views" });
    expect(within(nav).getByRole("link", { name: "Income, month by month" })).toHaveAttribute("href", "/income?bot=btc");
    expect(within(nav).getByRole("link", { name: "Worst case right now" })).toHaveAttribute("href", "/worst-case?bot=btc");
    fireEvent.click(within(nav).getByRole("button", { name: "Details" }));
    expect(onDetails).toHaveBeenCalledWith(true);
  });

  test("Details on a phone shows the full monitor", () => {
    mockBots(twoBots());
    render(<Monitor role="user" />);
    fireEvent.click(screen.getByRole("button", { name: "Menu" }));
    fireEvent.click(screen.getByRole("button", { name: "Details" }));
    expect(screen.getByTestId("desktop-monitor")).not.toHaveClass("hidden");
  });

  test("a warning is shown in the phone header and the headline", () => {
    const bots = twoBots();
    bots[0].status = { ...bots[0].status, halted: true };
    render(<PhoneOverview bots={bots} selected="btc" />);
    expect(within(screen.getByTestId("phone-warnings")).getByText("HALTED")).toBeInTheDocument();
    expect(screen.getByText(/halted: kill switch/)).toBeInTheDocument();
  });

  test("no close this month says not yet, never 0", () => {
    const bots = twoBots();
    bots[0].summary = summary("BTC", { income: { ...summary().income, this_month: month("2026-10", 0, 0, 0) } });
    render(<PhoneOverview bots={bots} selected="btc" />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("This month: not yet: no close this month");
    expect(screen.getByRole("heading", { level: 1 })).not.toHaveTextContent(/0\.0/);
  });

  test("worst case unknown says why", () => {
    const bots = twoBots();
    bots[0].summary = summary("BTC", { worst_case: { available: false, reason: "no DVOL reading" } });
    render(<PhoneOverview bots={bots} selected="btc" />);
    expect(screen.getByRole("region", { name: "Worst case right now" })).toHaveTextContent("Unknown: no DVOL reading.");
  });
});

describe("Scenario: range bar", () => {
  test("the bar shows the put and call strikes with a marker at today's price", () => {
    render(<PhoneOverview bots={twoBots()} selected="btc" />);
    const holds = screen.getByRole("region", { name: "What it holds" });
    expect(holds).toHaveTextContent("4 positions. Nearest pays if BTC stays between $90,000 and $110,000 until 2026-10-30.");
    const bar = within(holds).getByTestId("range-bar");
    expect(bar).toHaveAccessibleName("Put $90,000, now $100,000, call $110,000");
    expect(bar).toHaveTextContent("$90,000now $100,000$110,000");
  });

  test("band and marker positions", () => {
    const b = rangeBar({ put_strike: 90000, call_strike: 110000 }, 100000);
    expect(b.marker).toBeCloseTo(50, 6);
    expect(b.bandLeft).toBeGreaterThan(0);
    expect(b.bandLeft + b.bandWidth).toBeLessThan(100);
    expect(b.inside).toBe(true);
    const out = rangeBar({ put_strike: 90000, call_strike: 110000 }, 85000);
    expect(out.inside).toBe(false);
    expect(out.marker).toBeLessThan(out.bandLeft);
    expect(rangeBar(null, 100000)).toBeNull();
    expect(rangeBar({ put_strike: 90000, call_strike: 110000 }, null)).toBeNull();
  });

  test("a one-legged nearest strangle says which side is missing instead of a band", () => {
    const bots = twoBots();
    bots[0].summary = summary("BTC", { holding: { ...summary().holding, nearest: { expiry: "2026-10-30T08:00:00Z", put_strike: 90000, call_strike: null } } });
    render(<PhoneOverview bots={bots} selected="btc" />);
    const holds = screen.getByRole("region", { name: "What it holds" });
    expect(holds).toHaveTextContent("Nearest holds only its put at $90,000 until 2026-10-30; its call side is missing.");
    expect(within(holds).queryByTestId("range-bar")).toBeNull();
  });

  test("RangeBar marks spot outside the band in the loss colour", () => {
    render(<RangeBar bar={rangeBar({ put_strike: 90000, call_strike: 110000 }, 85000)} />);
    expect(screen.getByTestId("range-bar").querySelector(".bg-loss")).not.toBeNull();
  });
});

describe("Scenario: desktop unchanged", () => {
  test("the desktop monitor shows every bot's summary, from 640 px", () => {
    mockBots(twoBots());
    render(<Monitor role="manager" />);
    const desktop = screen.getByTestId("desktop-monitor");
    expect(desktop).toHaveClass("sm:block");
    expect(within(desktop).getByRole("region", { name: "BTC · investor summary" })).toBeInTheDocument();
    expect(within(desktop).getByRole("region", { name: "ETH · investor summary" })).toBeInTheDocument();
    expect(within(desktop).getByTestId("bot-btc")).toBeInTheDocument();
    expect(within(desktop).getByTestId("bot-eth")).toBeInTheDocument();
  });
});

describe("#321 Scenario: brand-new bot", () => {
  test("'Your BTC bot started 20 minutes ago' and the four steps, the first done and the second current", () => {
    render(<BotSummary bot={brandNew({ market: { spot: 100000 } })} />);
    const panel = screen.getByRole("region", { name: "BTC · first day" });
    expect(within(panel).getByRole("heading", { name: "Your BTC bot started 20 minutes ago" })).toBeInTheDocument();
    const steps = within(panel).getAllByRole("listitem");
    expect(steps.map((s) => s.dataset.state)).toEqual(["done", "now", "next", "then"]);
    expect(steps[0]).toHaveTextContent("Connected to your account");
    expect(steps[0]).toHaveTextContent("Balance 1.2500 BTC (practice on testnet). It can trade options only; it can't move money.");
    expect(steps[1]).toHaveAttribute("aria-current", "step");
    expect(steps[1]).toHaveTextContent("Now · about 1 more minute");
    expect(steps[1]).toHaveClass("border-testnet");
    expect(steps[2]).toHaveClass("border-dashed");
    expect(within(panel).getByText(/how it earns, in 60 seconds/)).toBeInTheDocument();
    expect(panel).not.toHaveTextContent(/video/i);
  });

  test("frozen: the step says why in plain words", () => {
    render(<FirstDayPanel bot={brandNew({ risk: { status: { frozen: true, freeze_reason: "gamma regime not confirmed yet (needs 2 daily closes)" } } })} />);
    const steps = screen.getAllByRole("listitem");
    expect(steps[1]).toHaveTextContent("Now · waiting: the market's mood must hold for 2 days");
  });

  test("market read: the second step is done and first positions are next up", () => {
    const steps = firstDaySteps(brandNew());
    expect(steps.map((s) => s.state)).toEqual(["done", "done", "now", "then"]);
    expect(steps[2].when).toBe("Now · usually within the hour");
  });

  test("live: no 'practice' wording; unknown balance says so", () => {
    expect(firstDaySteps(brandNew({ environment: "live" }))[0].text).toMatch(/^Balance 1\.2500 BTC\. /);
    expect(firstDaySteps(brandNew({ account: {} }))[0].text).toMatch(/^Balance not read yet\./);
  });

  test("the phone overview shows the first-day panel as its headline", () => {
    render(<PhoneOverview bots={[brandNew()]} selected="btc" />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Your BTC bot started 20 minutes ago");
  });
});

describe("#321 Scenario: first fill", () => {
  test("the summary block replaces the first-day panel", () => {
    const { rerender } = render(<BotSummary bot={brandNew()} />);
    expect(screen.getByRole("region", { name: "BTC · first day" })).toBeInTheDocument();
    const filled = brandNew();
    filled.summary = summary("BTC", {
      holding: { strangles: 1, legs: 1, calls: 1, puts: 0, coin: "BTC" },
      last_action: { ...summary().last_action, kind: "open", rule: "entry", reason: "Sold a call" },
    });
    rerender(<BotSummary bot={filled} />);
    expect(screen.queryByRole("region", { name: "BTC · first day" })).toBeNull();
    expect(screen.getByRole("region", { name: "BTC · investor summary" })).toBeInTheDocument();
  });
});

describe("#321 Scenario: flat later is not a first day", () => {
  test("a bot with closes in its journal and no open position shows the normal empty state", () => {
    const flat = brandNew();
    flat.summary = { ...flat.summary, last_action: summary().last_action };
    expect(isFirstDay(flat)).toBe(false);
    render(<BotSummary bot={flat} />);
    expect(screen.getByRole("region", { name: "BTC · investor summary" })).toHaveTextContent("Nothing open right now");
  });

  test("no summary yet, or a failure, is never a first day", () => {
    expect(isFirstDay({ name: "btc" })).toBe(false);
    expect(isFirstDay({ ...brandNew(), summaryError: "down" })).toBe(false);
    expect(isFirstDay({ ...brandNew(), error: "down" })).toBe(false);
  });
});

describe("#288 Scenario: first visit as a user", () => {
  test("the investor summary and the KPI strip are visible; every detail panel is folded with its one-line summary", () => {
    mockBots(twoBots());
    render(<Monitor role="user" />);
    const desktop = screen.getByTestId("desktop-monitor");
    expect(within(desktop).getByRole("region", { name: "BTC · investor summary" })).toBeInTheDocument();
    expect(panelTitles(desktop, false)).toEqual([
      "Account & collateral (shared by all bots)",
      "Open positions",
      "Missing legs: why repair holds them",
      "Not managed by the bot (still uses margin)",
      "Trade history & profitability",
      "Activity",
      "P&L (USD, all bots)",
      "P&L in coin (per bot)",
      "Working orders (placed, not filled yet)",
    ]);
    expect(panelTitles(desktop, true)).toHaveLength(0);
    // The KPI strip is outside every panel: visible.
    expect(within(desktop).getByText("Bots online")).toBeVisible();
    expect(within(desktop).getByText("8 legs, 4 strangles")).toBeInTheDocument();
    expect(within(desktop).getByText("2 working")).toBeInTheDocument();
    expect(within(desktop).getByText("nothing yet")).toBeInTheDocument();
  });
});

describe("#288 Scenario: a saved choice wins", () => {
  test("Trade history opened before in this browser is open on reload", () => {
    window.localStorage.setItem("monitor.panel.trades", "1");
    mockBots(twoBots());
    render(<Monitor role="user" />);
    const desktop = screen.getByTestId("desktop-monitor");
    expect(panelTitles(desktop, true)).toEqual(["Trade history & profitability"]);
  });
});

describe("#288 Scenario: managers unchanged", () => {
  test.each(["manager", "admin", null])("%s with no saved choices: every panel is open", (role) => {
    mockBots(twoBots());
    render(<Monitor role={role} />);
    const desktop = screen.getByTestId("desktop-monitor");
    expect(panelTitles(desktop, false)).toHaveLength(0);
    expect(panelTitles(desktop, true)).toHaveLength(9);
  });
});

describe("#288 Scenario: warnings never folded away", () => {
  test("LOOP STALLED shows in the header while the user's panels are folded", () => {
    const bots = twoBots();
    bots[0].status = { ...bots[0].status, loop_at: new Date(Date.now() - 3600_000).toISOString(), eval_interval_ms: 40000 };
    mockBots(bots);
    render(<Monitor role="user" />);
    const desktop = screen.getByTestId("desktop-monitor");
    expect(panelTitles(desktop, false)).toHaveLength(9);
    expect(within(within(desktop).getByTestId("bot-btc")).getByText("LOOP STALLED")).toBeInTheDocument();
    // And on the phone header.
    expect(within(screen.getByTestId("phone-warnings")).getByText("LOOP STALLED")).toBeInTheDocument();
  });
});

describe("overview helpers", () => {
  test("startedAgo", () => {
    expect(startedAgo(20)).toBe("less than a minute");
    expect(startedAgo(60)).toBe("1 minute");
    expect(startedAgo(20 * 60)).toBe("20 minutes");
    expect(startedAgo(2 * 3600)).toBe("2 hours");
    expect(startedAgo(3 * 86400)).toBe("3 days");
    expect(startedAgo(undefined)).toBeNull();
  });

  test("plainFreeze", () => {
    expect(plainFreeze("gamma regime not confirmed yet (needs 2 daily closes)")).toBe("waiting: the market's mood must hold for 2 days");
    expect(plainFreeze("DVOL moved to 50–70 (IV percentile 62), not yet confirmed")).toMatch(/expected swing changed/);
    expect(plainFreeze("something new")).toBe("waiting: something new");
    expect(plainFreeze("")).toBe("waiting: new trades are paused");
  });

  test("monthBars label and loss flags", () => {
    const { bars, label } = monthBars(summary().income);
    expect(label).toBe("Last 6 months: 5 up, July down");
    expect(bars.filter((b) => b.loss).map((b) => b.month)).toEqual(["2026-07"]);
    expect(Math.max(...bars.map((b) => b.height))).toBe(100);
    const quiet = monthBars({ months: [month("2026-09", 0, 0, 0), month("2026-10", 0.001, 100, 1)] });
    expect(quiet.label).toBe("Last 2 months: 1 up, none down, 1 without closes");
    expect(quiet.bars[0].height).toBe(0);
  });

  test("worstCaseLine takes the worse side", () => {
    expect(worstCaseLine(summary().worst_case)).toEqual({ move: -7.9, lossCoin: 0.05, lossUSD: 4605, pct: 4.2 });
    const up = { ...summary().worst_case, up: { loss_coin: 0.07, loss_usd: 7000, pct_of_equity: null } };
    expect(worstCaseLine(up)).toEqual({ move: 7.9, lossCoin: 0.07, lossUSD: 7000, pct: null });
    expect(worstCaseLine({ available: false })).toEqual({ unknown: "not computed yet" });
  });

  test("monthSoFar, moveSince, stateWords, warningChips", () => {
    expect(monthSoFar("2026-10-19T12:00:00Z")).toBe("1–19 Oct");
    expect(monthSoFar("2026-11-01T02:00:00Z")).toBe("1 Nov");
    expect(monthSoFar(undefined)).toBeNull();
    expect(moveSince(100, 102)).toBeCloseTo(2, 9);
    expect(moveSince(undefined, 102)).toBeNull();
    expect(stateWords({ status: status("BTC") })).toBe("running normally");
    expect(stateWords({ status: status("BTC"), stale: true, error: "x" })).toBe("offline");
    expect(stateWords({ status: status("BTC", { risk: { status: { frozen: true } } }) })).toBe("new trades paused");
    expect(warningChips({ status: status("BTC", { risk: { im_over_limit: true } }), stale: true })).toEqual(["STALE", "IM over limit"]);
  });

  test("folded panel summaries", () => {
    expect(positionsSummary([{ status: { open_legs: 1, open_strangles: 1 } }])).toBe("1 leg, 1 strangle");
    expect(positionsSummary([])).toBe("none open");
    expect(ordersSummary([{ status: { pending: 0 } }])).toBe("none");
    expect(activitySummary([{ at: "2026-10-19T17:02:00Z", bot: "btc", label: "Filled" }])).toBe("last: 14:02 BTC Filled");
    expect(activitySummary([])).toBe("nothing yet");
  });
});
