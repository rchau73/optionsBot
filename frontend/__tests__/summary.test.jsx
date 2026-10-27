import { render, screen, within } from "@testing-library/react";
import InvestorSummary, { BotSummary } from "@/components/InvestorSummary";
import { fetchBotState } from "@/lib/api";
import { ALLOWED_ENDPOINTS } from "@/lib/bots";
import { formatSignedPct, formatSignedUSD, hasIncome, holdingLine, incomeRows, monthLabel, worstCaseRows } from "@/lib/summary";

const MODEL = "model: Black–Scholes on each leg's current IV (DVOL when missing), spot moved ±3 daily σ (σ = DVOL ÷ √365) over one day";

// The shape GET /api/summary serves (internal/api/summary.go).
const fixture = (overrides = {}) => ({
  as_of: "2026-10-22T12:00:00Z",
  strategy_id: "btc-strangle",
  underlying: "BTC",
  spot: 100000,
  holding: {
    strangles: 2,
    legs: 4,
    calls: 2,
    puts: 2,
    notional_coin: 1.5,
    notional_usd: 150000,
    nearest_expiry: "2026-10-30T08:00:00Z",
    nearest_dte: 7.8,
    coin: "BTC",
  },
  income: {
    coin: "BTC",
    months: [
      { month: "2026-05", realised_coin: 0, realised_usd: 0, closes: 0 },
      { month: "2026-06", realised_coin: 0, realised_usd: 0, closes: 0 },
      { month: "2026-07", realised_coin: 0, realised_usd: 0, closes: 0 },
      { month: "2026-08", realised_coin: 0.012, realised_usd: 1200, closes: 3 },
      { month: "2026-09", realised_coin: -0.008, realised_usd: -800, closes: 2 },
      { month: "2026-10", realised_coin: 0.004, realised_usd: 410, closes: 1 },
    ],
    this_month: { month: "2026-10", realised_coin: 0.004, realised_usd: 410, closes: 1 },
    unrealised_coin: -0.001,
    unrealised_usd: -100,
    basis: "realised: journal closes, net of fees; unrealised: open book at its marks",
  },
  worst_case: {
    model: MODEL,
    available: true,
    sigmas: 3,
    dvol: 50,
    move_pct: 7.9,
    coin: "BTC",
    down: { spot: 92100, loss_coin: 0.05, loss_usd: 4605, pct_of_equity: 4.2 },
    up: { spot: 107900, loss_coin: 0.03, loss_usd: 3237, pct_of_equity: null },
  },
  last_action: {
    at: "2026-10-21T15:00:00Z",
    kind: "close",
    instrument: "BTC-30OCT26-90000-P",
    option_type: "put",
    qty: 0.5,
    rule: "stop_loss",
    reason: "Bought back the put: it lost twice its premium (stop-loss)",
    detail: "mark 0.02 ≥ 2 × premium 0.01",
    pnl_coin: -0.008,
    pnl_usd: -800,
  },
  ...overrides,
});

const card = (bot) => {
  render(<BotSummary bot={{ name: "btc", ...bot }} />);
  return screen.getByRole("region", { name: "BTC · investor summary" });
};

describe("Scenario: the investor summary is the first block for that bot", () => {
  test("shows holding, income per month, worst case and the last action", () => {
    const el = card({ summary: fixture() });
    expect(within(el).getByText("2 strangles · 4 legs (2 calls, 2 puts)")).toBeInTheDocument();
    expect(within(el).getByText(/1\.50 BTC sold ≈ \$150,000/)).toBeInTheDocument();
    expect(within(el).getByText(/in 7\.8 days/)).toBeInTheDocument();

    // Income: coin and USD per month, net of fees, newest first.
    expect(within(el).getByText("Income per month (realised, net of fees)")).toBeInTheDocument();
    const oct = within(el).getByText("Oct 2026").closest("tr");
    expect(oct).toHaveTextContent("(this month)");
    expect(within(oct).getByText("+0.00400 BTC")).toBeInTheDocument();
    expect(within(oct).getByText("+$410")).toBeInTheDocument();
    const aug = within(el).getByText("Aug 2026").closest("tr");
    expect(within(aug).getByText("+0.01200 BTC")).toBeInTheDocument();
    expect(within(aug).getByText("+$1,200")).toBeInTheDocument();
    // A month without closes says so, never zeros.
    expect(within(el).getByText("May 2026").closest("tr")).toHaveTextContent("no closes");
    expect(within(el).getByText("May 2026").closest("tr")).not.toHaveTextContent("0.00000");
    const rows = within(el).getAllByRole("row");
    expect(rows[1]).toHaveTextContent("Oct 2026"); // first body row: the current month

    // Worst case in coin, USD and % of equity.
    const down = within(el).getByText(/If BTC falls 7\.9% to \$92,100/);
    expect(down).toHaveTextContent("−0.05000 BTC");
    expect(down).toHaveTextContent("−$4,605");
    expect(down).toHaveTextContent("−4.2% of equity");
    expect(within(el).getByText(/If BTC rises/)).toHaveTextContent("% of equity unknown");

    // Last action in plain words, with its result.
    expect(within(el).getByText("Bought back the put: it lost twice its premium (stop-loss)")).toBeInTheDocument();
    expect(within(el).getByText(/bought back 0\.50 × BTC-30OCT26-90000-P/)).toBeInTheDocument();
    expect(within(el).getByText(/Result of that trade/)).toHaveTextContent("−0.00800 BTC · −$800");
  });

  test("the block comes first, one per bot", () => {
    render(
      <InvestorSummary
        bots={[
          { name: "btc", summary: fixture() },
          { name: "eth", summary: fixture({ underlying: "ETH" }) },
        ]}
      />,
    );
    const regions = screen.getAllByRole("region");
    expect(regions.map((r) => r.getAttribute("aria-label"))).toEqual(["BTC · investor summary", "ETH · investor summary"]);
  });

  test("Monitor renders the investor summary before the KPI strip", () => {
    const src = require("fs").readFileSync(require("path").join(__dirname, "../components/Monitor.jsx"), "utf8");
    const main = src.slice(src.indexOf("<main"));
    expect(main.indexOf("<InvestorSummary")).toBeGreaterThan(-1);
    expect(main.indexOf("<InvestorSummary")).toBeLessThan(main.indexOf("<KpiStrip"));
    expect(main.indexOf("<InvestorSummary")).toBeLessThan(main.indexOf("<Panel"));
  });
});

describe("Scenario: a month with a net loss", () => {
  test("the loss has a minus sign and the same type size as a gain", () => {
    const el = card({ summary: fixture() });
    const loss = within(within(el).getByText("Sep 2026").closest("tr")).getByText("−$800");
    const gain = within(within(el).getByText("Aug 2026").closest("tr")).getByText("+$1,200");
    expect(loss).toHaveClass("text-loss");
    expect(gain).toHaveClass("text-profit");
    // Same weight and size: the classes differ only in colour.
    const strip = (node) => [...node.classList].filter((c) => !/^text-(loss|profit)$/.test(c)).sort();
    expect(strip(loss)).toEqual(strip(gain));
    expect(loss.closest("table")).toHaveClass("text-base");
    const lossCoin = within(within(el).getByText("Sep 2026").closest("tr")).getByText("−0.00800 BTC");
    expect(lossCoin).toBeInTheDocument();
  });
});

describe("Scenario: /api/summary fails", () => {
  test("the block says the summary is unavailable and shows no zeros", () => {
    const el = card({ summary: null, summaryError: "bot btc is unreachable" });
    expect(within(el).getByRole("alert")).toHaveTextContent("Summary unavailable: bot btc is unreachable");
    expect(el).not.toHaveTextContent(/\$0|0\.0+ BTC|0 legs|0 strangles/);
    expect(el).not.toHaveTextContent("Income per month");
  });

  test("a bot that stopped answering shows its error, not its last summary", () => {
    const el = card({ summary: fixture(), error: "500" });
    expect(within(el).getByRole("alert")).toHaveTextContent("Summary unavailable: 500");
    expect(el).not.toHaveTextContent("2 strangles");
  });

  test("before the first answer it says it is loading, with no numbers", () => {
    const el = card({});
    expect(el).toHaveTextContent("Loading the summary");
    expect(el).not.toHaveTextContent(/\d/);
  });

  test("fetchBotState keeps the bot when only the summary fails", async () => {
    const realFetch = global.fetch;
    global.fetch = jest.fn(async (url) =>
      url.endsWith("/summary")
        ? { ok: false, status: 404, headers: new Headers(), json: async () => ({ error: "unknown bot or endpoint" }) }
        : { ok: true, status: 200, headers: new Headers(), json: async () => ({ url }) },
    );
    try {
      const state = await fetchBotState("btc");
      expect(state.status).toEqual({ url: "/api/bots/btc/status" });
      expect(state.summary).toBeNull();
      expect(state.summaryError).toBe("unknown bot or endpoint");
      global.fetch = jest.fn(async (url) => ({ ok: true, status: 200, headers: new Headers(), json: async () => ({ url }) }));
      const fine = await fetchBotState("btc");
      expect(fine.summary).toEqual({ url: "/api/bots/btc/summary" });
      expect(fine.summaryError).toBeNull();
    } finally {
      global.fetch = realFetch;
    }
  });

  test("the proxy forwards /api/summary (read-only allow-list)", () => {
    expect(ALLOWED_ENDPOINTS).toContain("summary");
  });
});

describe("Scenario: the worst case is shown", () => {
  test("the model label and its assumptions sit next to the number, in the same type size", () => {
    const el = card({ summary: fixture() });
    const label = within(el).getByText(/This is a model, not a quote/).closest("p");
    expect(label).toHaveTextContent(MODEL);
    expect(label).toHaveTextContent("DVOL 50.0, 3σ");
    expect(label).toHaveClass("text-base");
    const numbers = within(el).getByText(/If BTC falls/).closest("ul");
    expect(numbers).toHaveClass("text-base");
    expect(label.parentElement).toBe(numbers.parentElement);
  });

  test("without spot or DVOL it says unknown, never 0, and keeps the label", () => {
    const wc = { model: MODEL, available: false, reason: "no DVOL reading", sigmas: 3, dvol: 0, move_pct: 0, coin: "BTC", down: {}, up: {} };
    const el = card({ summary: fixture({ worst_case: wc }) });
    const block = within(el).getByText("Worst case now (one day)").parentElement;
    expect(block).toHaveTextContent("Unknown: no DVOL reading.");
    expect(block).toHaveTextContent("This is a model, not a quote");
    expect(block).not.toHaveTextContent(/\$0|0\.00000/);
  });
});

describe("empty states", () => {
  test("no closes yet: says what will appear and when", () => {
    const months = fixture().income.months.map((m) => ({ ...m, realised_coin: 0, realised_usd: 0, closes: 0 }));
    const el = card({ summary: fixture({ income: { ...fixture().income, months } }) });
    expect(el).toHaveTextContent("No closed trades in the last six months yet");
    expect(el).toHaveTextContent("once the bot buys back or lets expire its first option");
    expect(within(el).queryByRole("table")).toBeNull();
  });

  // #321 "flat later is not a first day": closes in the journal, nothing open.
  test("nothing held after trading: the normal empty state", () => {
    const el = card({
      summary: fixture({ holding: { strangles: 0, legs: 0, calls: 0, puts: 0, notional_coin: 0, notional_usd: 0, nearest_expiry: null, nearest_dte: null, coin: "BTC" } }),
    });
    expect(el).toHaveTextContent("Nothing open right now");
    expect(screen.queryByRole("region", { name: "BTC · first day" })).toBeNull();
  });

  test("an opening trade has no result line", () => {
    const open = { ...fixture().last_action, kind: "open", rule: "entry", reason: "Sold a strangle", pnl_coin: undefined, pnl_usd: undefined };
    const el = card({ summary: fixture({ last_action: open }) });
    expect(el).toHaveTextContent(/sold 0\.50 × BTC-30OCT26-90000-P/);
    expect(el).not.toHaveTextContent("Result of that trade");
  });
});

describe("summary helpers", () => {
  test("signed amounts", () => {
    expect(formatSignedUSD(1234.5)).toBe("+$1,235");
    expect(formatSignedUSD(-40)).toBe("−$40");
    expect(formatSignedUSD(0.2)).toBe("$0");
    expect(formatSignedUSD(undefined)).toBe("—");
    expect(formatSignedPct(4.24)).toBe("+4.2%");
    expect(formatSignedPct(-4.24)).toBe("−4.2%");
    expect(formatSignedPct(-0.01)).toBe("0.0%");
    expect(formatSignedPct(null)).toBe("—");
  });

  test("month labels", () => {
    expect(monthLabel("2026-10")).toBe("Oct 2026");
    expect(monthLabel("2026-13")).toBe("2026-13");
    expect(monthLabel(undefined)).toBe("—");
  });

  test("income rows, newest first, the current month flagged", () => {
    const rows = incomeRows(fixture().income);
    expect(rows.map((r) => r.month)).toEqual(["2026-10", "2026-09", "2026-08", "2026-07", "2026-06", "2026-05"]);
    expect(rows[0].current).toBe(true);
    expect(rows[1].current).toBe(false);
    expect(incomeRows(undefined)).toEqual([]);
    expect(hasIncome(fixture().income)).toBe(true);
    expect(hasIncome({ months: [] })).toBe(false);
  });

  test("worst case losses become signed results", () => {
    const [down, up] = worstCaseRows(fixture().worst_case);
    expect(down).toEqual({ key: "down", label: "falls", spot: 92100, coin: -0.05, usd: -4605, pct: -4.2 });
    expect(up.pct).toBeNull();
    expect(worstCaseRows({ available: false })).toEqual([]);
  });

  test("holding line", () => {
    expect(holdingLine({ strangles: 1, legs: 1, calls: 1, puts: 0 })).toBe("1 strangle · 1 leg (1 call, 0 puts)");
    expect(holdingLine(null)).toBe("—");
  });
});
