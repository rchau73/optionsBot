import { fireEvent, render, screen, within } from "@testing-library/react";
import { useState } from "react";
import IncomePage, { IncomeView } from "@/components/IncomePage";
import { fetchBotIncome } from "@/lib/api/bots";
import { formatStartPct, startTableRows, worstWeekPct } from "@/lib/income";

// The income page's "% of start" view (#629): /api/income's `start`
// (internal/api/income_start.go) — results as a % of the start balance.

const result = (r, h, c, extra = {}) => ({ result_pct: r, hold_pct: h, combined_pct: c, spot_start: 60000, spot_end: 66000, result_coin: 0, closes: 1, ...extra });

const start = (overrides = {}) => ({
  known: true,
  requested: null,
  at: "2026-10-28T11:45:00Z",
  source: "margin balance in BTC at 2026-10-28T11:45:00Z",
  balance_coin: 0.5,
  balance_usd: 30000,
  to: "2026-11-04T12:00:00Z",
  ...result(1, 10, 11.1, { result_coin: 0.005, closes: 2 }),
  worst_week: { known: true, start: "2026-10-28T12:00:00Z", end: "2026-11-03T12:00:00Z", coin: -0.00325, pct: -0.65, known_at: null },
  months: [
    { month: "2026-06", reason: "no margin balance recorded in the P&L history this month", result_pct: null, hold_pct: null, combined_pct: null },
    { month: "2026-07", reason: "no margin balance recorded in the P&L history this month", result_pct: null },
    { month: "2026-08", reason: "no margin balance recorded in the P&L history this month", result_pct: null },
    { month: "2026-09", reason: "no margin balance recorded in the P&L history this month", result_pct: null },
    { month: "2026-10", partial: true, from: "2026-10-28T11:45:00Z", balance_coin: 0.5, balance_usd: 30000, ...result(2, 3.3, 5.4) },
    { month: "2026-11", partial: false, from: "2026-11-01T00:00:00Z", balance_coin: 0.505, balance_usd: 31310, ...result(-1, 6.5, 5.4) },
  ],
  ...overrides,
});

const income = (st = start()) => ({
  coin: "BTC",
  months: [{ month: "2026-11", closes: 1, realised_coin: 0.001, realised_usd: 60, premium_coin: 0.002, buyback_coin: 0.0005, fees_coin: 0.0005 }],
  summary: { label: "testnet", total_6m: { coin: 0.001, usd: 60 }, return_pct: 0.2, worst_week: { known: false }, missing_days: [] },
  start: st,
});

function Page({ data = income(), unit: first = "pct" }) {
  const [unit, setUnit] = useState(first);
  return <IncomeView bot="btc" income={data} unit={unit} onUnit={setUnit} />;
}

const table = () => screen.getByRole("region", { name: "Each month, as a % of its start balance" });
const row = (label) => within(table()).getByText(label).closest("tr");

describe("Scenario: % of start view", () => {
  test("the start balance with its time, and the three results as signed % of it", () => {
    render(<Page />);
    expect(screen.getByRole("button", { name: "% of start" })).toHaveAttribute("aria-pressed", "true");
    const strip = screen.getByRole("region", { name: "Start balance" });
    expect(strip).toHaveTextContent("0.50000 BTC · $30,000");
    expect(strip).toHaveTextContent("Recorded 2026-10-28");
    expect(strip).toHaveTextContent("margin balance in BTC");
    expect(screen.getByRole("region", { name: "Bot result, % of start" })).toHaveTextContent("+1.00 %");
    expect(screen.getByRole("region", { name: "Holding alone, % of start" })).toHaveTextContent("+10.00 %");
    expect(screen.getByRole("region", { name: "Holding alone, % of start" })).toHaveTextContent("0 % in BTC");
    expect(screen.getByRole("region", { name: "Bot + holding, % of start" })).toHaveTextContent("+11.10 %");
    expect(screen.getByRole("region", { name: "Worst week, % of start" })).toHaveTextContent("−0.65 %");
    // The coin and USD views' table is not shown.
    expect(screen.queryByRole("region", { name: "Each month, where the money came from" })).toBeNull();
  });

  test("a loss shows its minus sign, the loss colour and '· loss' in words", () => {
    render(<Page />);
    const nov = row("Nov 2026");
    expect(nov).toHaveTextContent("· loss");
    expect(within(nov).getByText("−1.00 %")).toHaveClass("text-loss");
    expect(within(row("Oct 2026")).getByText("+2.00 %")).toHaveClass("text-profit");
  });

  test("the worst week before 7 days says not yet and when it will be known", () => {
    render(<Page data={income(start({ worst_week: { known: false, coin: 0, pct: null, known_at: "2026-11-04T11:45:00Z" } }))} />);
    const tile = screen.getByRole("region", { name: "Worst week, % of start" });
    expect(tile).toHaveTextContent("not yet");
    expect(tile).toHaveTextContent("known on 2026-11-04");
  });

  test("the 6-month tile links to the % view, and the toggle goes back", () => {
    render(<Page unit="coin" />);
    fireEvent.click(screen.getByRole("button", { name: "See it as a % of the start balance" }));
    expect(screen.getByRole("region", { name: "Start balance" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "BTC" }));
    expect(screen.getByRole("region", { name: "Each month, where the money came from" })).toBeInTheDocument();
  });
});

describe("Scenario: month that began before the first balance", () => {
  test("October is measured from 28 Oct and says partial: from 28 Oct", () => {
    render(<Page />);
    expect(row("Oct 2026")).toHaveTextContent("partial: from 2026-10-28");
    expect(row("Nov 2026")).not.toHaveTextContent("partial");
  });
});

describe("Scenario: no balance", () => {
  test("a month with no balance says why and shows no %", () => {
    render(<Page />);
    const sep = row("Sep 2026");
    expect(sep).toHaveTextContent("no %: no margin balance recorded in the P&L history this month");
    expect(within(sep).queryByText(/[+−]\d/)).toBeNull();
  });

  test("no balance at all: the reason, never a 0 %", () => {
    render(<Page data={income({ known: false, reason: "no P&L history line in the window carries Deribit's margin balance", months: [] })} />);
    expect(screen.getByRole("status")).toHaveTextContent("No start balance yet: no P&L history line");
    expect(screen.queryByText(/0\.00 %/)).toBeNull();
  });

  test("an older bot without start says so", () => {
    render(<Page data={{ ...income(), start: undefined }} />);
    expect(screen.getByRole("status")).toHaveTextContent("No start balance yet");
  });
});

describe("Scenario: same start on both environments", () => {
  test("?from= reaches /api/income and the strip names it", async () => {
    const realFetch = global.fetch;
    global.fetch = jest.fn(async () => ({
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => income(start({ requested: "2026-10-28T11:45:00Z" })),
    }));
    window.localStorage.setItem("income.unit", "pct");
    try {
      render(<IncomePage bot="btc" from="2026-10-28T11:45Z" />);
      expect(await screen.findByRole("region", { name: "Start balance" })).toHaveTextContent("the first balance at or after 2026-10-28");
      expect(global.fetch).toHaveBeenCalledWith("/api/bots/btc/income?from=2026-10-28T11%3A45Z", expect.anything());
      expect(screen.getByRole("button", { name: "% of start" })).toHaveAttribute("aria-pressed", "true");
    } finally {
      global.fetch = realFetch;
      window.localStorage.clear();
    }
  });

  test("without from the client calls the plain route", async () => {
    const realFetch = global.fetch;
    global.fetch = jest.fn(async (url) => ({ ok: true, status: 200, headers: new Headers(), json: async () => ({ url }) }));
    try {
      expect(await fetchBotIncome("btc", undefined, null)).toEqual({ url: "/api/bots/btc/income" });
    } finally {
      global.fetch = realFetch;
    }
  });
});

describe("helpers", () => {
  test("signed % and rows", () => {
    expect(formatStartPct(1.234)).toBe("+1.23 %");
    expect(formatStartPct(-0.65)).toBe("−0.65 %");
    expect(formatStartPct(null)).toBe("—");
    const rows = startTableRows(income());
    expect(rows[0].month).toBe("2026-11");
    expect(rows[1].partialFrom).toBe("2026-10-28T11:45:00Z");
    expect(rows[2].reason).toMatch(/no margin balance/);
    expect(startTableRows({})).toEqual([]);
  });

  test("the coin view's worst week names why it has no %", () => {
    expect(worstWeekPct({ known: true, pct: null, pct_note: "the balance was first recorded on 28 Oct 2026, after this week began" })).toBe(
      "% of the account unknown: the balance was first recorded on 28 Oct 2026, after this week began",
    );
  });
});
