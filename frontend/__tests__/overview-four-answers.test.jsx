import { fireEvent, render, screen, within } from "@testing-library/react";
import Monitor from "@/components/Monitor";
import { useMonitor } from "@/hooks/useMonitor";
import { lastTradeLine } from "@/lib/overview";
import { openLines, usdFooter, worstLines } from "@/lib/tiles";

// #489: the four answers of the first five minutes (epic E3), pinned on the
// desktop Overview: what the bot holds (one row per strangle, in plain words),
// whether it makes money (the month net of fees, in coin and USD, a loss as
// visible as a gain), the worst case in money, and why it made its last trade
// (one click). A redesign that drops one of them fails here.

jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/components/OverviewPnl", () => () => null);

const status = (extra = {}) => ({
  as_of: new Date().toISOString(),
  underlying: "BTC",
  environment: "testnet",
  market: { spot: 95900, gex_regime: "POSITIVE/PINNING", gamma_flip: 90000, dvol: 45.2 },
  trend: "neutral",
  gex_as_of: new Date().toISOString(),
  risk: { status: { limit_im_pct: 20, max_mm_pct: 50, frozen: false, regime_known: true }, im_pct: 10, mm_pct: 5, as_of: new Date().toISOString() },
  held_legs: [],
  open_legs: 2,
  open_strangles: 1,
  ...extra,
});

const OPEN = { at: "2026-10-27T14:02:00Z", kind: "open", instrument: "BTC-27NOV26-85000-P", qty: 1, rule: "entry", reason: "Sold the put: the 25-day slot was vacant." };
const CLOSE = {
  at: "2026-10-28T09:12:00Z",
  kind: "close",
  instrument: "BTC-27NOV26-110000-C",
  qty: 1,
  rule: "take_profit",
  reason: "Bought back the call: its take-profit fired.",
  detail: "ROI 82 % ≥ 80 %",
  pnl_coin: -0.0012,
  pnl_usd: -115,
};

const summary = ({ realisedCoin = 0.004, realisedUSD = 410, legs = 2, lastAction = OPEN } = {}) => ({
  as_of: "2026-10-28T15:00:00Z",
  underlying: "BTC",
  spot: 95900,
  income: {
    coin: "BTC",
    months: [{ month: "2026-10", realised_coin: realisedCoin, realised_usd: realisedUSD, closes: 2 }],
    this_month: { month: "2026-10", realised_coin: realisedCoin, realised_usd: realisedUSD, closes: 2 },
    unrealised_coin: legs ? -0.001 : 0,
    unrealised_usd: legs ? -98.7 : 0,
  },
  worst_case: legs
    ? { available: true, move_pct: 7.9, down: { loss_coin: 0.05, loss_usd: 4935, pct_of_equity: 4.2 }, up: { loss_coin: 0.03, loss_usd: 2961, pct_of_equity: 2.5 } }
    : { available: true, move_pct: 7.9, down: { loss_coin: 0, loss_usd: 0, pct_of_equity: 0 }, up: { loss_coin: 0, loss_usd: 0, pct_of_equity: 0 } },
  holding: { strangles: legs ? 1 : 0, legs },
  last_action: lastAction,
});

const strangle = {
  strangle_id: "btc-a",
  slot: { dte: 25, delta: 0.16 },
  expiry: "2026-11-27T08:00:00Z",
  dte: 29.4,
  spot: 95900,
  legs: [
    { position_id: "p", instrument: "BTC-27NOV26-85000-P", option_type: "put", strike: 85000, qty: 1 },
    { position_id: "c", instrument: "BTC-27NOV26-110000-C", option_type: "call", strike: 110000, qty: 1 },
  ],
  missing: [],
  premium_coin: 0.01,
  unrealised_coin: 0.0062,
  unrealised_usd: 594.6,
  earned_pct: 62,
  break_even_low: 84000,
  break_even_high: 111000,
};
const payoff = (strangles) => ({ as_of: new Date().toISOString(), underlying: "BTC", spot: 95900, strangles });

const FIXTURES = {
  profit: () => ({ name: "btc", stale: false, status: status(), summary: summary(), payoff: payoff([strangle]) }),
  loss: () => ({ name: "btc", stale: false, status: status(), summary: summary({ realisedCoin: -0.004, realisedUSD: -410, lastAction: CLOSE }), payoff: payoff([strangle]) }),
  none: () => ({ name: "btc", stale: false, status: status({ open_legs: 0, open_strangles: 0 }), summary: summary({ legs: 0, lastAction: CLOSE }), payoff: payoff([]) }),
  stale: () => ({ name: "btc", stale: true, status: status({ as_of: new Date(Date.now() - 600_000).toISOString() }), summary: summary(), payoff: payoff([strangle]) }),
};

function renderOverview(bot) {
  useMonitor.mockReturnValue({ names: [bot.name], bots: [bot], feed: [], history: [], trades: {}, error: null, pollMs: 1000, polledAt: Date.now() });
  render(<Monitor />);
  return screen.getByTestId("desktop-monitor");
}

beforeEach(() => window.localStorage.clear());

describe("Scenario: a profitable month", () => {
  test("the result net of fees in coin and USD, the worst case in money, the positions and a Why on the last trade", () => {
    const page = renderOverview(FIXTURES.profit());
    // (2) the month, net of fees, coin first and USD on the footer.
    const earned = within(page).getByRole("region", { name: "Coins earned this month" });
    expect(earned).toHaveTextContent("net of fees");
    expect(within(earned).getByText("+0.00400 BTC")).toHaveClass("text-profit");
    expect(earned).toHaveTextContent("≈ +$410 at today's prices");
    // (3) the worst case in money: coin, USD and % of the account.
    const worst = within(page).getByRole("region", { name: "Worst case" });
    expect(worst).toHaveTextContent("−0.05000 BTC");
    expect(worst).toHaveTextContent("≈ −$4,935 at today's prices");
    expect(worst).toHaveTextContent("−4.2% of account");
    // (1) the positions, one row per strangle, in plain words.
    const row = within(page).getByTestId("strangle-row-btc-a");
    expect(row).toHaveAccessibleName(/BTC 25 d slot, expiry .*, \d+ d left, P&L \+0\.00620 BTC/);
    // (4) the last trade, its recorded reason one click away.
    const last = within(page).getByRole("region", { name: "BTC · last trade" });
    expect(last).toHaveTextContent("sold 1.00 × BTC-27NOV26-85000-P");
    expect(within(last).queryByText(OPEN.reason)).toBeNull();
    const why = within(last).getByRole("button", { name: "Why?" });
    expect(why).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(why);
    expect(why).toHaveAttribute("aria-expanded", "true");
    expect(within(last).getByTestId("last-trade-why")).toHaveTextContent(OPEN.reason);
    expect(within(last).getByRole("link", { name: "All actions, explained →" })).toHaveAttribute("href", "/activity?bot=btc");
  });
});

describe("Scenario: a losing month", () => {
  test("the loss has the same size and prominence as a gain, with its sign", () => {
    const gain = within(renderOverview(FIXTURES.profit())).getByText("+0.00400 BTC");
    const gainClasses = [...gain.classList].filter((c) => c !== "text-profit");
    document.body.innerHTML = "";
    const page = renderOverview(FIXTURES.loss());
    const earned = within(page).getByRole("region", { name: "Coins earned this month" });
    const loss = within(earned).getByText("−0.00400 BTC");
    expect(loss).toHaveClass("text-loss");
    expect([...loss.classList].filter((c) => c !== "text-loss")).toEqual(gainClasses);
    expect(earned).toHaveTextContent("≈ −$410 at today's prices");
  });

  test("a losing last trade: Why? shows its reason, rule numbers and past result net of fees, in coin and USD", () => {
    const page = renderOverview(FIXTURES.loss());
    const last = within(page).getByRole("region", { name: "BTC · last trade" });
    expect(last).toHaveTextContent("bought back 1.00 × BTC-27NOV26-110000-C");
    fireEvent.click(within(last).getByRole("button", { name: "Why?" }));
    const why = within(last).getByTestId("last-trade-why");
    expect(why).toHaveTextContent(CLOSE.reason);
    expect(why).toHaveTextContent(CLOSE.detail);
    expect(why).toHaveTextContent("Result of that trade (past, net of fees): −0.00120 BTC · −$115");
    expect(within(why).getByText("−0.00120 BTC")).toHaveClass("text-loss");
  });
});

describe("Scenario: no open positions or stale data", () => {
  test("nothing held: plain words instead of zeros, the last trade still explained", () => {
    const page = renderOverview(FIXTURES.none());
    expect(page).toHaveTextContent(/Nothing held: the bot opens a strangle when a slot is vacant/);
    for (const name of ["Open now", "Worst case"]) {
      const tile = within(page).getByRole("region", { name });
      expect(within(tile).getByTestId("tile-line-btc")).toHaveTextContent("nothing open");
      expect(tile).not.toHaveTextContent(/0\.00000|\$0\b/);
    }
    expect(within(page).getByRole("region", { name: "BTC · last trade" })).toHaveTextContent("bought back");
  });

  test("stale data: the band says how old it is", () => {
    const page = renderOverview(FIXTURES.stale());
    expect(within(page).getByTestId("regime-line-btc")).toHaveTextContent(/stale · updated 10m ago/);
  });

  test("a summary that did not load: the last trade is unknown with the reason, never blank", () => {
    const bot = { ...FIXTURES.profit(), summary: null, summaryError: "bot unreachable" };
    const page = renderOverview(bot);
    expect(within(page).getByRole("region", { name: "BTC · last trade" })).toHaveTextContent("Last trade unknown: bot unreachable.");
  });
});

describe("the helpers behind the four answers", () => {
  test("lastTradeLine: unknown, none, open and close", () => {
    expect(lastTradeLine({ summary: null })).toEqual({ unknown: "not loaded yet" });
    expect(lastTradeLine({ summary: { last_action: null } })).toEqual({ none: true });
    expect(lastTradeLine({ summary: { last_action: { ...OPEN, reason: "" } } })).toMatchObject({ what: "sold 1.00 × BTC-27NOV26-85000-P", reason: "entry", pnlCoin: null });
    expect(lastTradeLine({ summary: { last_action: CLOSE } })).toMatchObject({ reason: CLOSE.reason, detail: CLOSE.detail, pnlCoin: -0.0012, pnlUSD: -115 });
  });

  test("open and worst-case lines say nothing open for a flat bot; the footer too, unless another bot holds something", () => {
    const flat = FIXTURES.none();
    const held = { ...FIXTURES.profit(), name: "eth" };
    expect(openLines([flat])[0]).toMatchObject({ text: "nothing open", none: true });
    expect(usdFooter(worstLines([flat]))).toBe("nothing open");
    expect(usdFooter(worstLines([flat, held]))).toBe("≈ −$4,935 at today's prices");
    // No holding field (an older bot): the figure is shown as before.
    const old = { ...FIXTURES.profit(), summary: { ...summary(), holding: undefined } };
    expect(openLines([old])[0].text).toBe("−0.00100 BTC");
  });
});
