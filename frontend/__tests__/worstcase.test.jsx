import { render, screen, within } from "@testing-library/react";
import WorstCaseReport from "@/components/WorstCaseReport";
import { WorstCaseScreen } from "@/components/WorstCaseView";
import { BotSummary } from "@/components/InvestorSummary";
import { formatMove, headlineLoss, scenarioRows, stopWord, swingWords, whyNoScenarios } from "@/lib/worstcase";

// The shape GET /api/summary serves (internal/api/summary.go), worst_case with
// its five scenarios (#318). Signs: loss_* positive = money lost; result_* + = gain.
const fixture = (overrides = {}) => ({
  as_of: "2026-10-22T12:00:00Z",
  strategy_id: "btc-strangle",
  underlying: "BTC",
  environment: "testnet",
  spot: 100000,
  stop_loss_multiple: 2,
  holding: { strangles: 1, legs: 2, calls: 1, puts: 1, notional_coin: 2, notional_usd: 200000, coin: "BTC" },
  income: { coin: "BTC", months: [] },
  worst_case: {
    model: "model: …",
    available: true,
    sigmas: 3,
    dvol: 50,
    move_pct: 7.85,
    coin: "BTC",
    down: { spot: 92150, loss_coin: 0.05, loss_usd: 4607, pct_of_equity: 2.5 },
    up: { spot: 107850, loss_coin: 0.02, loss_usd: 2157, pct_of_equity: 1.0 },
    scenarios: [
      { sigmas: -3, move_pct: -7.85, spot: 92150, result_coin: -0.05, result_usd: -4607, pct_of_equity: -2.5 },
      { sigmas: -2, move_pct: -5.23, spot: 94770, result_coin: -0.02, result_usd: -1895, pct_of_equity: -1.0 },
      { sigmas: 0, move_pct: 0, spot: 100000, result_coin: 0.0012, result_usd: 120, pct_of_equity: 0.06 },
      { sigmas: 2, move_pct: 5.23, spot: 105230, result_coin: -0.008, result_usd: -842, pct_of_equity: -0.4 },
      { sigmas: 3, move_pct: 7.85, spot: 107850, result_coin: -0.02, result_usd: -2157, pct_of_equity: -1.0 },
    ],
  },
  last_action: null,
  ...overrides,
});

const page = (summary) => {
  render(<WorstCaseReport summary={summary} />);
  return screen.getByRole("region", { name: "Worst case right now" });
};

describe("Scenario: the page shows five days, worst first in the headline", () => {
  test("the headline names the larger of the two 3σ losses in coin, USD and % of the account", () => {
    page(fixture());
    const h1 = screen.getByRole("heading", { level: 1 });
    expect(h1).toHaveTextContent("If BTC had a very bad day today, you'd lose about 0.0500 BTC ($4,607), 2.5 % of the account");
    expect(screen.getByText(/Estimated at \d\d:\d\d from today's option prices/)).toBeInTheDocument();
  });

  test("the headline takes the up side when it loses more", () => {
    const s = fixture();
    s.worst_case.up = { spot: 107850, loss_coin: 0.07, loss_usd: 7550, pct_of_equity: 3.5 };
    expect(headlineLoss(s)).toEqual({ coin: 0.07, usd: 7550, pct: 3.5 });
    page(s);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("0.0700 BTC ($7,550), 3.5 % of the account");
  });

  test("the table has five rows from −3σ to +3σ, each result signed in coin and USD", () => {
    const el = page(fixture());
    const table = within(el).getByRole("table");
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(5);
    expect(rows.map((r) => within(r).getByRole("rowheader").textContent)).toEqual([
      "−8 % (very bad)",
      "−5 % (bad)",
      "0 % (quiet)",
      "+5 % (strong rise)",
      "+8 % (very strong rise)",
    ]);
    expect(rows[0]).toHaveTextContent("3× a typical day's swing");
    expect(rows[0]).toHaveTextContent("−0.05000 BTC · −$4,607");
    expect(rows[0]).toHaveTextContent("−2.5%");
    expect(rows[2]).toHaveTextContent("no move");
    expect(rows[2]).toHaveTextContent("+0.00120 BTC · +$120");
    expect(rows[2]).toHaveTextContent("+0.1%");
  });

  test("a loss carries a labelled mark and the same type as a gain", () => {
    const el = page(fixture());
    const rows = within(within(el).getByRole("table")).getAllByRole("row").slice(1);
    expect(within(rows[0]).getAllByLabelText("loss").length).toBeGreaterThan(0);
    expect(within(rows[2]).queryByLabelText("loss")).toBeNull();
    const loss = within(rows[0]).getByText("−0.05000 BTC · −$4,607");
    const gain = within(rows[2]).getByText("+0.00120 BTC · +$120");
    expect(loss).toHaveClass("text-loss");
    expect(gain).toHaveClass("text-profit");
    const strip = (n) => [...n.classList].filter((c) => !/^text-(loss|profit)$/.test(c)).sort();
    expect(strip(loss)).toEqual(strip(gain));
  });

  test("the bad-day and can't-promise cards, with the stop multiple from the config", () => {
    page(fixture({ stop_loss_multiple: 3 }));
    const bad = screen.getByRole("region", { name: "What the bot does on a bad day" });
    expect(bad).toHaveTextContent("Buys back any side that has lost 3 times what it was paid, at once.");
    expect(bad).toHaveTextContent("its exit line");
    expect(bad).toHaveTextContent("Cuts positions at once if the account's safety margin gets thin.");
    const promise = screen.getByRole("region", { name: "What this estimate can't promise" });
    expect(promise).toHaveTextContent("A crash can be bigger than −8 %");
    expect(screen.getByText("How it's worked out")).toBeInTheDocument();
    expect(screen.getByText(/DVOL 50\.0\)/)).toHaveTextContent("50.0 % ÷ √365 × 3 ≈ 7.9 %");
  });

  test("twice for the shipped multiple of 2", () => {
    page(fixture());
    expect(screen.getByRole("region", { name: "What the bot does on a bad day" })).toHaveTextContent("has lost twice what it was paid");
  });

  test("an unknown equity says so instead of a %", () => {
    const s = fixture();
    s.worst_case.down.pct_of_equity = null;
    s.worst_case.scenarios = s.worst_case.scenarios.map((r) => ({ ...r, pct_of_equity: null }));
    const el = page(s);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("% of the account unknown");
    expect(within(el).getAllByText("unknown: no equity reading")).toHaveLength(5);
  });
});

describe("Scenario: no frequency claim", () => {
  test("no row says how often such a day happens", () => {
    const el = page(fixture());
    expect(el).not.toHaveTextContent(/1 day in|once (a|every)|per (year|month)|times a year|\bdays? in \d+|how often|probability|chance/i);
  });
});

describe("Scenario: nothing held or DVOL unknown", () => {
  const noZeros = (el) => expect(el).not.toHaveTextContent(/\$0\b|0\.0+ BTC|[+−]?0\.0%/);

  test("nothing held: says so in words, no table", () => {
    const s = fixture({ holding: { strangles: 0, legs: 0, calls: 0, puts: 0, coin: "BTC" } });
    s.worst_case.scenarios = s.worst_case.scenarios.map((r) => ({ ...r, result_coin: 0, result_usd: 0, pct_of_equity: 0 }));
    const el = page(s);
    expect(el).toHaveTextContent("The bot holds nothing right now");
    expect(within(el).queryByRole("table")).toBeNull();
    noZeros(el);
  });

  test("DVOL unknown: says so in words, no table", () => {
    const s = fixture({
      worst_case: { model: "m", available: false, reason: "no DVOL reading", sigmas: 3, dvol: 0, coin: "BTC", scenarios: [] },
    });
    const el = page(s);
    expect(el).toHaveTextContent("Unknown: no DVOL reading.");
    expect(within(el).queryByRole("table")).toBeNull();
    noZeros(el);
  });

  test("an older bot without scenarios", () => {
    const s = fixture();
    delete s.worst_case.scenarios;
    expect(whyNoScenarios(s)).toMatch(/does not report the five scenarios/);
    expect(scenarioRows(s)).toEqual([]);
  });
});

describe("the page shell", () => {
  test("Overview back link, testnet badge and the footer", () => {
    render(<WorstCaseScreen bot="btc" summary={fixture()} />);
    expect(screen.getByRole("link", { name: "← Overview" })).toHaveAttribute("href", "/?bot=btc");
    expect(screen.getByText("TESTNET")).toBeInTheDocument();
    expect(screen.getByText(/not investment advice/)).toBeInTheDocument();
    expect(screen.queryByText(/video/i)).toBeNull();
  });

  test("loading and failure say so, never zeros", () => {
    const { rerender } = render(<WorstCaseScreen bot="btc" summary={null} />);
    expect(screen.getByText(/Not yet: loading/)).toBeInTheDocument();
    rerender(<WorstCaseScreen bot="btc" summary={null} error="bot btc is unreachable" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Unknown: bot btc is unreachable");
    expect(screen.queryByRole("table")).toBeNull();
  });

  test("a failed poll keeps the last figures and says they are not updating", () => {
    render(<WorstCaseScreen bot="btc" summary={fixture()} error="timeout" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Not updating: timeout");
    expect(screen.getByRole("table")).toBeInTheDocument();
  });

  test("no bot chosen: links to each bot's page", () => {
    render(<WorstCaseScreen bot={null} names={["btc", "eth"]} summary={null} />);
    expect(screen.getByRole("link", { name: "ETH" })).toHaveAttribute("href", "/worst-case?bot=eth");
  });

  test("the summary block's card links to this page", () => {
    render(<BotSummary bot={{ name: "btc", summary: fixture({ income: { coin: "BTC", months: [] } }) }} />);
    expect(screen.getByRole("link", { name: /worst case right now/i })).toHaveAttribute("href", "/worst-case?bot=btc");
  });
});

describe("lib/worstcase", () => {
  test("formatMove, swingWords, stopWord", () => {
    expect(formatMove(-7.85)).toBe("−8 %");
    expect(formatMove(5.2)).toBe("+5 %");
    expect(formatMove(0.2)).toBe("0 %");
    expect(formatMove(null)).toBe("—");
    expect(swingWords(-3)).toBe("3× a typical day's swing");
    expect(swingWords(0)).toBe("no move");
    expect(stopWord(2)).toBe("twice");
    expect(stopWord(2.5)).toBe("2.5 times");
    expect(stopWord(undefined)).toBeNull();
  });

  test("rows are sorted worst fall first whatever the API order", () => {
    const s = fixture();
    s.worst_case.scenarios = [...s.worst_case.scenarios].reverse();
    expect(scenarioRows(s).map((r) => r.sigmas)).toEqual([-3, -2, 0, 2, 3]);
  });

  test("without the stop multiple the bad-day card still reads", () => {
    page(fixture({ stop_loss_multiple: undefined }));
    expect(screen.getByRole("region", { name: "What the bot does on a bad day" })).toHaveTextContent("reaches its stop-loss limit");
  });
});
