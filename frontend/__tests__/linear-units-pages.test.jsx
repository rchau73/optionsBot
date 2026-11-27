import { fireEvent, render, screen, within } from "@testing-library/react";
import DashboardTiles from "@/components/DashboardTiles";
import FirstDayPanel from "@/components/FirstDayPanel";
import HedgeLine from "@/components/HedgeLine";
import HedgeRows from "@/components/HedgeRows";
import { IncomeView } from "@/components/IncomePage";
import LastTrade from "@/components/LastTrade";
import StrangleRow from "@/components/StrangleRow";

// The SOL and XRP bots' amounts are in USDC on every page (#837, review of
// #906): the coin names the market, never the money. The shapes are those the
// bot serves (/api/status `unit`, /api/income `unit`, /api/hedge in coin at
// the mark); BTC keeps its coin.

const NOW = Date.parse("2026-11-26T12:00:00Z");
// an amount labelled with the coin: "+0.05 SOL", "12.5 SOL"
const SOL_AMOUNT = /\d SOL\b/;

const solStatus = { underlying: "SOL", unit: "USDC", environment: "testnet", market: { spot: 140 } };

describe("Scenario: the Income page of a SOL bot is in USDC", () => {
  const month = { month: "2026-11", closes: 2, premium_coin: 12.5, buyback_coin: 3, fees_coin: 0.5, realised_coin: 9, premium_usd: 12.5, buyback_usd: 3, fees_usd: 0.5, realised_usd: 9 };
  const income = {
    coin: "SOL",
    unit: "USDC",
    months: [month],
    unrealised_coin: 4.5,
    unrealised_usd: 4.5,
    summary: { total_6m: { coin: 9, usd: 9 }, average_month: { coin: 9, usd: 9 }, up_months: 1, down_months: 0, return_pct: 0.18, worst_week: { known: false } },
  };

  test("the title names the coin, the toggle and every amount say USDC", () => {
    render(<IncomeView bot="sol" income={income} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Income · SOL");
    const toggle = screen.getByRole("group", { name: "Show amounts in" });
    expect(within(toggle).getByRole("button", { name: "USDC" })).toHaveAttribute("aria-pressed", "true");
    expect(within(toggle).queryByRole("button", { name: "SOL" })).toBeNull();
    const row = screen.getByRole("row", { name: /Nov/ });
    expect(row).toHaveTextContent("+12.50 USDC");
    expect(row).toHaveTextContent("+9.00 USDC");
    expect(screen.getByRole("region", { name: "Last 6 months, after fees" })).toHaveTextContent("+9.00 USDC");
    expect(document.body.textContent).not.toMatch(SOL_AMOUNT);
    expect(screen.getByText(/a dollar coin/)).toBeInTheDocument();
  });

  test("an older bot without the field, and BTC, keep the coin", () => {
    const { unit, ...older } = income;
    render(<IncomeView bot="sol" income={{ ...older }} />);
    expect(within(screen.getByRole("group", { name: "Show amounts in" })).getByRole("button", { name: "SOL" })).toBeInTheDocument();
    expect(unit).toBe("USDC");
  });
});

describe("Scenario: the hedge of a SOL bot shows its money in USDC", () => {
  const hedge = {
    available: true,
    enabled: true,
    testnet: true,
    underlying: "SOL",
    instrument: "SOL_USDC-PERPETUAL",
    perp_usd: -700,
    perp_size: -5,
    size_unit: "SOL",
    perp_delta_coin: -5,
    perp_mark_price: 140,
    perp_as_of: "2026-11-26T11:59:50Z",
    unrealised_coin: 0.05,
    realised_coin: 0.1,
    fees_coin: 0.01,
    funding_coin: 0,
    net_delta_coin: 0.2,
    band_coin: 1.7,
    orders_last_hour: 1,
    max_orders_per_hour: 12,
    benchmark: { real_pnl_coin: 0.1, unit: "USDC", pricing: "exchange" },
  };
  const bot = { name: "sol", status: solStatus, hedge };

  test("Details: P&L, realised and unrealised in USDC at the mark; the deltas stay in SOL", () => {
    render(<HedgeRows bots={[bot]} now={NOW} />);
    const row = screen.getByTestId("hedge-row-sol");
    expect(row).toHaveTextContent("+7.00 USDC"); // 0.05 SOL × 140
    expect(row).toHaveTextContent("+12.60 USDC"); // (0.1 − 0.01) × 140
    expect(row).toHaveTextContent("−5.0000 SOL"); // the perpetual's delta is a quantity of SOL
    expect(row).not.toHaveTextContent("≈");
  });

  test("Overview line: the P&L in USDC, once", () => {
    render(<HedgeLine bot={bot} now={NOW} />);
    const line = screen.getByTestId("hedge-line-sol");
    expect(line).toHaveTextContent("P&L +14.00 USDC net of fees");
    expect(line).not.toHaveTextContent("+0.1000 SOL");
  });
});

describe("Scenario: the overview of a SOL bot is in USDC", () => {
  const summary = {
    underlying: "SOL",
    as_of: "2026-11-26T12:00:00Z",
    holding: { legs: 2 },
    income: { this_month: { closes: 2, realised_coin: 9, realised_usd: 9 }, months: [], unrealised_coin: 4.5, unrealised_usd: 4.5 },
    worst_case: { available: true, move_pct: 12, down: { loss_coin: 0.5, loss_usd: 70, pct_of_equity: 10 }, up: { loss_coin: 0.2, loss_usd: 28 } },
    last_action: { at: "2026-11-26T11:00:00Z", kind: "close", instrument: "SOL_USDC-25DEC26-80-C", qty: 10, reason: "take-profit", pnl_coin: 3.25, pnl_usd: 3.25 },
  };
  const bot = { name: "sol", status: solStatus, summary };

  test("the four tiles: USDC amounts, no ≈ $ copy of them, the move names SOL", () => {
    render(<DashboardTiles bot={bot} />);
    const month = screen.getByRole("region", { name: "This month" });
    expect(month).toHaveTextContent("+9.00 USDC");
    expect(month).not.toHaveTextContent("≈");
    expect(screen.getByRole("region", { name: "Open now" })).toHaveTextContent("+4.50 USDC");
    const worst = screen.getByRole("region", { name: "Worst case" });
    expect(worst).toHaveTextContent("−70.00 USDC");
    expect(worst).toHaveTextContent("a very bad day (SOL −12");
    expect(worst).not.toHaveTextContent("of account");
    expect(document.body.textContent).not.toMatch(SOL_AMOUNT);
  });

  test("a strangle row and the last trade's result are in USDC", () => {
    render(<StrangleRow sp={{ strangle_id: "s1", slot: { dte: 30 }, unrealised_coin: 4.5, unrealised_usd: 4.5 }} coin="SOL" unit="USDC" onOpen={() => {}} />);
    expect(screen.getByTestId("strangle-row-s1")).toHaveTextContent("+4.50 USDC");
    render(<LastTrade bot={bot} coin="SOL" unit="USDC" />);
    fireEvent.click(screen.getByRole("button", { name: "Why?" }));
    expect(screen.getByTestId("last-trade-why")).toHaveTextContent("+3.25 USDC");
  });

  test("the first-day panel gives the balance in USDC", () => {
    const fresh = { name: "sol", status: { ...solStatus, account: { equity: 5000 } }, summary: { holding: { legs: 0 } } };
    render(<FirstDayPanel bot={fresh} />);
    expect(screen.getByRole("heading", { level: 2 })).toHaveTextContent("Your SOL bot started");
    expect(screen.getByText(/Balance 5,000.00 USDC \(practice on testnet\)/)).toBeInTheDocument();
  });

  test("BTC keeps its coin and its ≈ $", () => {
    const btc = { name: "btc", status: { underlying: "BTC", unit: "BTC" }, summary: { ...summary, underlying: "BTC", income: { ...summary.income, this_month: { closes: 2, realised_coin: 0.004, realised_usd: 410 } } } };
    render(<DashboardTiles bot={btc} />);
    const month = screen.getByRole("region", { name: "This month" });
    expect(month).toHaveTextContent("+0.00400 BTC");
    expect(month).toHaveTextContent("≈ +$410");
  });
});
