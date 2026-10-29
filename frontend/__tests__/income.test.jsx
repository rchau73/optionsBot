import { fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import IncomePage, { IncomeView } from "@/components/IncomePage";
import { BotSummary } from "@/components/InvestorSummary";
import { fetchBotIncome } from "@/lib/api/bots";
import { ALLOWED_ENDPOINTS } from "@/lib/bots";
import { formatAmount, incomeTableRows, returnLine, upDownLine, worstMonthMove, worstWeekPct } from "@/lib/income";

// A month as GET /api/income serves it (internal/api/income.go): coin parts,
// their USD at each close's spot, the recorded spot and holding alone.
const month = (m, premium, buyback, fees, spot, extra = {}) => ({
  month: m,
  closes: 2,
  premium_coin: premium,
  buyback_coin: buyback,
  fees_coin: fees,
  realised_coin: premium - buyback - fees,
  premium_usd: premium * spot,
  buyback_usd: buyback * spot,
  fees_usd: fees * spot,
  realised_usd: (premium - buyback - fees) * spot,
  spot_start: spot,
  spot_end: spot,
  spot_days: 30,
  hold_usd: 0,
  ...extra,
});

const fixture = (overrides = {}) => ({
  coin: "BTC",
  months: [
    month("2026-05", 0.03, 0.01, 0.001, 60000),
    month("2026-06", 0.03, 0.005, 0.001, 60000),
    month("2026-07", 0.02, 0.05, 0.002, 60000, { spot_end: 51600, spot_days: 5, hold_usd: -16800 }),
    month("2026-08", 0.03, 0.01, 0.001, 60000),
    { ...month("2026-09", 0, 0, 0, 60000), closes: 0, hold_usd: null, hold_reason: "no spot recorded in the P&L history this month" },
    month("2026-10", 0.02, 0.004, 0.001, 63000, { hold_usd: 6000 }),
  ],
  unrealised_coin: 0.001,
  unrealised_usd: 63,
  summary: {
    from: "2026-05-01T00:00:00Z",
    to: "2026-10-24T12:00:00Z",
    label: "testnet",
    total_6m: { coin: 0.009, usd: 600 },
    equity: 2,
    equity_note: "margin balance in BTC",
    return_pct: 0.45,
    average_month: { coin: 0.0018, usd: 120 },
    active_months: 5,
    up_months: 4,
    down_months: 1,
    worst_month: { month: "2026-07", coin: -0.032, usd: -1920, spot_move_pct: -14, spot_days: 5 },
    worst_week: { known: true, start: "2026-07-10T00:00:00Z", end: "2026-07-17T00:00:00Z", coin: -0.04, pct: -2 },
    days: 177,
    days_covered: 177,
    missing_days: [],
  },
  ...overrides,
});

function Page({ income = fixture(), ...props }) {
  const [unit, setUnit] = useState("coin");
  return <IncomeView bot="btc" income={income} unit={unit} onUnit={setUnit} {...props} />;
}

const table = () => screen.getByRole("region", { name: "Each month, where the money came from" });
const row = (label) => within(table()).getByText(label).closest("tr");

describe("Scenario: a losing month is as visible as a winning one", () => {
  test("the loss month shows a minus sign, '· loss' and the same type size", () => {
    render(<Page />);
    const jul = row("Jul 2026");
    expect(jul).toHaveTextContent("· loss");
    expect(jul).toHaveClass("bg-loss/10");
    const loss = within(jul).getByText("−0.03200 BTC");
    const gain = within(row("Aug 2026")).getByText("+0.01900 BTC");
    expect(loss).toHaveClass("text-loss");
    expect(gain).toHaveClass("text-profit");
    const strip = (n) => [...n.classList].filter((c) => !/^text-(loss|profit)$/.test(c)).sort();
    expect(strip(loss)).toEqual(strip(gain));
    expect(row("Aug 2026")).not.toHaveTextContent("· loss");
  });

  test("the worst-month tile names it, with the coin's recorded move", () => {
    render(<Page />);
    const tile = screen.getByRole("region", { name: "Worst month" });
    expect(tile).toHaveTextContent("−0.03200 BTC");
    expect(tile).toHaveTextContent("Jul 2026");
    expect(tile).toHaveTextContent("July: BTC fell 14.0 % in 5 days");
    expect(tile).toHaveClass("border-loss/60");
  });

  test("worst month and worst week are loss-styled only when negative (review of #349)", () => {
    const s = fixture().summary;
    render(
      <Page
        income={fixture({
          summary: { ...s, worst_month: { ...s.worst_month, coin: 0.004, usd: 240 }, worst_week: { ...s.worst_week, coin: 0.001, pct: 0.05 } },
        })}
      />,
    );
    expect(screen.getByRole("region", { name: "Worst month" })).not.toHaveClass("border-loss/60");
    expect(screen.getByRole("region", { name: "Worst week" })).not.toHaveClass("border-loss/60");
  });
});

describe("Scenario: the columns add up", () => {
  test("paid to you − paid to buy back − fees equals the result, every month, both units", () => {
    for (const unit of ["coin", "usd"]) {
      for (const r of incomeTableRows(fixture(), unit).filter((x) => x.closes)) {
        // buy-backs and fees are shown negative: the row adds up as shown
        expect(r.premium + r.buyback + r.fees).toBeCloseTo(r.result, 9);
      }
    }
  });

  test("the table shows the costs as negatives", () => {
    render(<Page />);
    const aug = row("Aug 2026");
    expect(aug).toHaveTextContent("+0.03000 BTC");
    expect(aug).toHaveTextContent("−0.01000 BTC");
    expect(aug).toHaveTextContent("−0.00100 BTC");
  });
});

describe("Scenario: USD toggle", () => {
  test("pressing USD shows every amount in USD and holding alone as the coin's USD change", () => {
    render(<Page />);
    const coinButton = screen.getByRole("button", { name: "BTC" });
    const usdButton = screen.getByRole("button", { name: "USD" });
    expect(coinButton).toHaveAttribute("aria-pressed", "true");
    // In coin, holding alone neither gains nor loses.
    expect(within(row("Jul 2026")).getAllByText(/BTC/).at(-1)).toHaveTextContent("0.00000 BTC");

    fireEvent.click(usdButton);
    expect(usdButton).toHaveAttribute("aria-pressed", "true");
    expect(coinButton).toHaveAttribute("aria-pressed", "false");
    const aug = row("Aug 2026");
    expect(aug).toHaveTextContent("+$1,800");
    expect(aug).toHaveTextContent("−$600");
    expect(aug).toHaveTextContent("−$60");
    expect(aug).toHaveTextContent("+$1,140");
    expect(aug).not.toHaveTextContent("BTC");
    expect(row("Jul 2026")).toHaveTextContent("−$16,800");
    expect(row("Oct 2026")).toHaveTextContent("+$6,000");
    // No history: unknown with the reason, never $0.
    expect(row("Sep 2026")).toHaveTextContent("unknown: no spot recorded");
    expect(screen.getByRole("region", { name: "Last 6 months, after fees" })).toHaveTextContent("+$600");
  });
});

describe("page states", () => {
  test("the tiles, the header and the footnotes", () => {
    render(<Page />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Income · BTC");
    expect(screen.getByRole("link", { name: "← Overview" })).toHaveAttribute("href", "/");
    expect(screen.getByText("TESTNET")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Last 6 months, after fees" })).toHaveTextContent("+0.00900 BTC+0.5 % more BTC than holding");
    expect(screen.getByRole("region", { name: "Average month" })).toHaveTextContent("4 months up, 1 down");
    expect(screen.getByRole("region", { name: "Worst week" })).toHaveTextContent("−2.0 % of the account");
    expect(row("Oct 2026")).toHaveTextContent("(so far)");
    expect(row("Sep 2026")).toHaveTextContent("no closes");
    expect(within(table()).getAllByRole("row")[1]).toHaveTextContent("Oct 2026");
    expect(screen.getByText(/Months are UTC calendar months/)).toBeInTheDocument();
    expect(screen.getByText(/not investment advice/)).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "How the income works" })).toHaveTextContent("is the month's result");
  });

  test("before any close and without history: not yet, never 0", () => {
    const income = fixture({
      months: fixture().months.map((m) => ({ ...m, closes: 0, hold_usd: null, hold_reason: "no spot recorded in the P&L history this month" })),
      summary: {
        ...fixture().summary,
        total_6m: { coin: 0, usd: 0 },
        return_pct: null,
        equity: null,
        equity_note: "no P&L history line in the window carries Deribit's margin balance",
        average_month: null,
        worst_month: null,
        up_months: 0,
        down_months: 0,
        worst_week: { known: false, coin: 0, pct: null, start: null, end: null },
        missing_days: ["2026-05-01"],
      },
    });
    render(<Page income={income} />);
    expect(screen.getByRole("region", { name: "Average month" })).toHaveTextContent("not yet");
    expect(screen.getByRole("region", { name: "Worst month" })).toHaveTextContent("not yet");
    expect(screen.getByRole("region", { name: "Worst week" })).toHaveTextContent("not yet: needs 7 days");
    expect(screen.getByRole("region", { name: "Last 6 months, after fees" })).toHaveTextContent("% of the account unknown");
    expect(screen.getByText(/No P&L history on 1 day/)).toBeInTheDocument();
  });

  test("loading, failure and no bot", () => {
    const { rerender } = render(<IncomeView bot="btc" income={null} loading />);
    expect(screen.getByText("Loading the income…")).toBeInTheDocument();
    rerender(<IncomeView bot="btc" income={null} error="bot btc is unreachable" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Income unavailable: bot btc is unreachable");
    expect(screen.queryByRole("table")).toBeNull();
    rerender(<IncomeView bot={null} income={null} />);
    expect(screen.getByRole("alert")).toHaveTextContent("No bot chosen");
  });

  test("a failed refresh keeps the last figures and says so", () => {
    render(<IncomeView bot="btc" income={fixture()} error="502" />);
    expect(screen.getByRole("status")).toHaveTextContent("Last refresh failed (502)");
    expect(screen.getByRole("table")).toBeInTheDocument();
  });
});

describe("wiring", () => {
  test("the summary card links to the income page of its bot", () => {
    const summary = {
      as_of: "2026-10-22T12:00:00Z",
      underlying: "BTC",
      holding: null,
      income: { coin: "BTC", months: [] },
      worst_case: { available: false },
      // A close in the journal: flat now, not a first day (#321).
      last_action: { at: "2026-10-21T15:00:00Z", kind: "close", instrument: "BTC-X-P", qty: 0.1, rule: "roi_target", reason: "Took profit" },
    };
    render(<BotSummary bot={{ name: "btc", summary }} />);
    expect(screen.getByRole("link", { name: /Income, month by month/ })).toHaveAttribute("href", "/income?bot=btc");
  });

  test("the proxy forwards /api/income and the client calls it", async () => {
    expect(ALLOWED_ENDPOINTS).toContain("income");
    const realFetch = global.fetch;
    global.fetch = jest.fn(async (url) => ({ ok: true, status: 200, headers: new Headers(), json: async () => ({ url }) }));
    try {
      expect(await fetchBotIncome("btc")).toEqual({ url: "/api/bots/btc/income" });
    } finally {
      global.fetch = realFetch;
    }
  });
});

describe("the page with its data hook", () => {
  test("loads /api/income and remembers the unit per browser", async () => {
    const realFetch = global.fetch;
    global.fetch = jest.fn(async () => ({ ok: true, status: 200, headers: new Headers(), json: async () => fixture() }));
    window.localStorage.setItem("income.unit", "usd");
    try {
      render(<IncomePage bot="btc" />);
      expect(screen.getByText("Loading the income…")).toBeInTheDocument();
      expect(await screen.findByRole("table")).toBeInTheDocument();
      expect(global.fetch).toHaveBeenCalledWith("/api/bots/btc/income", expect.anything());
      expect(screen.getByRole("button", { name: "USD" })).toHaveAttribute("aria-pressed", "true");
      expect(row("Aug 2026")).toHaveTextContent("+$1,140");
      fireEvent.click(screen.getByRole("button", { name: "BTC" }));
      expect(window.localStorage.getItem("income.unit")).toBe("coin");
      expect(row("Aug 2026")).toHaveTextContent("+0.01900 BTC");
    } finally {
      global.fetch = realFetch;
      window.localStorage.clear();
    }
  });
});

describe("income helpers", () => {
  test("amounts, lines and moves", () => {
    expect(formatAmount(-0.0123, "coin", "BTC")).toBe("−0.01230 BTC");
    expect(formatAmount(1200, "usd", "BTC")).toBe("+$1,200");
    expect(upDownLine({ up_months: 1, down_months: 0 })).toBe("1 month up, 0 down");
    // A negative return reads "less", never "−… more" (review of #349).
    expect(returnLine({ return_pct: -1.25 }, "ETH")).toBe("1.3 % less ETH than holding");
    expect(returnLine({ return_pct: 1.2 }, "BTC")).toBe("+1.2 % more BTC than holding");
    expect(returnLine({ return_pct: -0.01 }, "BTC")).toBe("0.0 % more BTC than holding");
    expect(worstMonthMove({ month: "2026-03", spot_move_pct: 4.2, spot_days: 1 }, "ETH")).toBe("March: ETH rose 4.2 % in 1 day");
    expect(worstMonthMove({ month: "2026-03", spot_move_pct: null }, "ETH")).toBeNull();
    expect(worstWeekPct({ known: true, pct: null })).toBe("% of the account unknown");
    expect(worstWeekPct({ known: false })).toBeNull();
  });
});
