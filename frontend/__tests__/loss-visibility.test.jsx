import { render, screen, within } from "@testing-library/react";
import DetailsPage from "@/components/DetailsPage";
import { IncomeView } from "@/components/IncomePage";
import PhoneOverview from "@/components/PhoneOverview";
import { PositionsScreen } from "@/components/PositionsPage";
import { WorstCaseScreen } from "@/components/WorstCaseView";
import { useMonitor } from "@/hooks/useMonitor";
import { botFor, incomeFor, payoffFor, summaryFor, tradesFor } from "@/test/resultFixtures";

// #582 (epic E3): losses as visible as gains on every page that shows a
// result, beyond the Overview (overview-four-answers.test.jsx, #489). On the
// loss fixture each page's result carries a minus sign and its unit (colour is
// never the only cue), is no smaller or lighter than the same figure on the
// profit fixture, and is not folded away; nothing held shows no invented 0.

jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/lib/track", () => ({ track: jest.fn(() => true) }));
jest.mock("@/components/PnlChart", () => () => null);

const SIZE = /^(text-(xs|sm|base|lg|xl|[2-9]xl|\[[^\]]+\])|font-(thin|extralight|light|normal|medium|semibold|bold|extrabold|black))$/;

/** The size and weight classes an element shows, its own and inherited, nearest first (colour ignored). */
function sizeAndWeight(el) {
  const out = [];
  for (let n = el; n && n !== document.body; n = n.parentElement) {
    const own = [...n.classList].filter((c) => SIZE.test(c));
    for (const kind of ["text-", "font-"]) {
      if (!out.some((c) => c.startsWith(kind))) {
        const c = own.find((x) => x.startsWith(kind));
        if (c) out.push(c);
      }
    }
  }
  return out.sort();
}

/** Throws, naming the page, when the result is missing or folded away (closed details, hidden, aria-hidden, sr-only). */
function shownResult(page, container, matcher) {
  const found = within(container).queryAllByText(matcher);
  if (!found.length) throw new Error(`${page}: the result ${matcher} is not shown (folded away or dropped)`);
  const el = found[0];
  for (let n = el; n && n !== container; n = n.parentElement) {
    const folded = (n.tagName === "DETAILS" && !n.open) || n.hidden || n.getAttribute("aria-hidden") === "true" || n.classList.contains("hidden") || n.classList.contains("sr-only");
    if (folded) throw new Error(`${page}: the result ${matcher} is folded away by default (${n.tagName.toLowerCase()})`);
  }
  return el;
}

const incomeRow = (c, month) => within(within(c).getByRole("region", { name: "Each month, where the money came from" })).getByText(month).closest("tr");
const exact = (s) => (_, el) => el?.textContent === s && [...el.children].every((c) => c.textContent !== s);
const containing = (re) => (_, el) => re.test(el?.textContent ?? "") && [...el.children].every((c) => !re.test(c.textContent));

function renderDetails(kind) {
  const bot = botFor(kind);
  useMonitor.mockReturnValue({ names: [bot.name], bots: [bot], feed: [], history: [], trades: { btc: tradesFor(kind) }, error: null, pollMs: 1000, polledAt: Date.now() });
  return render(<DetailsPage />).container;
}

// Each page: how to render it on a fixture, and its result in coin and in USD
// (on the profit fixture the gain, on the loss fixture the loss). Worst case
// shows the same estimate on both: its gain is the calm day, its loss the
// 3σ fall, in one table.
const PAGES = [
  {
    page: "Income",
    render: (kind, unit = "coin") => render(<IncomeView bot="btc" income={incomeFor(kind)} unit={unit} />).container,
    results: (kind, s) => [
      ["coin", (c) => within(incomeRow(c, "Oct 2026")).getByText(`${s}0.00400 BTC`)],
      ["usd", (c) => within(incomeRow(c, "Oct 2026")).getByText(`${s}$410`)],
    ],
  },
  {
    page: "Details (pro)",
    render: (kind) => renderDetails(kind),
    results: (kind, s) => [
      ["coin", (c) => within(within(c).getByTestId("trades-summary")).getByText(`${s}0.00400 BTC`)],
      ["usd", (c) => within(within(c).getByTestId("trades-summary")).getByText(`≈ ${s}$410`)],
    ],
  },
  {
    page: "Phone overview",
    render: (kind) => render(<PhoneOverview bots={[botFor(kind)]} selected="btc" />).container,
    results: (kind, s) => [
      ["coin", (c) => within(within(c).getByRole("heading", { level: 1 })).getByText(`${s}0.00400 BTC`)],
      ["usd", (c) => within(c).getByText(containing(new RegExp(`^About \\${s}\\$410 ·`)))],
      ["this month", (c) => within(within(c).getByRole("region", { name: "Making or losing" })).getByText(`${s}0.00400 BTC`)],
    ],
  },
  {
    page: "Worst case",
    render: (kind) => render(<WorstCaseScreen bot="btc" names={["btc"]} summary={summaryFor(kind)} />).container,
    results: (kind) => [["coin and USD", (c) => within(c).getByText(kind === "loss" ? "−0.05000 BTC · −$4,935" : "+0.00030 BTC · +$29")]],
  },
  {
    page: "Positions",
    render: (kind) => render(<PositionsScreen names={["btc"]} bot="btc" payoff={payoffFor(kind)} />).container,
    results: (kind, s) => [["coin", (c) => within(c).getByText(containing(new RegExp(`^${s === "−" ? "−" : ""}62 % of the premium earned · \\${s}0\\.0062 of \\+0\\.0100 BTC$`)))]],
  },
];

beforeEach(() => window.localStorage.clear());

describe("Scenario: a losing month on each page", () => {
  describe.each(PAGES)("$page", ({ page, render: show, results }) => {
    test("the result shows a minus sign and its unit, as large and as bold as the gain, not folded away", () => {
      const styles = {};
      for (const kind of ["profit", "loss"]) {
        const s = kind === "loss" ? "−" : "+";
        for (const [label, find] of results(kind, s)) {
          const container = label === "usd" && page === "Income" ? show(kind, "usd") : show(kind);
          const el = find(container);
          shownResult(page, container, exact(el.textContent));
          if (kind === "loss") {
            expect(el.textContent).toMatch(/−/); // the sign: colour is never the only cue
            expect(el.textContent).toMatch(/BTC|\$/); // the unit
          }
          (styles[label] ??= {})[kind] = sizeAndWeight(el);
          document.body.innerHTML = "";
        }
      }
      for (const [label, { profit, loss }] of Object.entries(styles)) {
        expect([page, label, loss]).toEqual([page, label, profit]);
      }
    });
  });

  test("Income names the losing month in words, not colour alone", () => {
    render(<IncomeView bot="btc" income={incomeFor("loss")} />);
    expect(incomeRow(document.body, "Oct 2026")).toHaveTextContent("· loss");
  });

  test("Worst case marks a losing day with the word loss", () => {
    render(<WorstCaseScreen bot="btc" names={["btc"]} summary={summaryFor("loss")} />);
    const fall = screen.getByText("−0.05000 BTC · −$4,935").parentElement;
    expect(within(fall).getByLabelText("loss")).toHaveTextContent("▼ loss");
  });
});

// Nothing held: words instead of figures. A real result (a close earlier in
// the month) may still be shown; a figure that only stands for "nothing" (a
// 0.00000 BTC or a $0) may not.
const ZERO = /(^|[^\d.])0\.0{4,}\b|\$0(?![\d,.])/;

describe("Scenario: nothing held", () => {
  test.each([
    ["Income", () => render(<IncomeView bot="btc" income={incomeFor("none")} unit="usd" />).container, /no closes/],
    ["Details (pro)", () => renderDetails("none"), /No open positions[\s\S]*No opens or closes in the journal yet/],
    ["Phone overview", () => render(<PhoneOverview bots={[botFor("none")]} selected="btc" />).container, /Nothing open right now/],
    ["Worst case", () => render(<WorstCaseScreen bot="btc" names={["btc"]} summary={summaryFor("none")} />).container, /holds nothing right now/],
    ["Positions", () => render(<PositionsScreen names={["btc"]} bot="btc" payoff={payoffFor("none")} />).container, /Nothing open right now/],
  ])("%s says nothing is held and shows no invented figure", (page, show, words) => {
    const container = show();
    expect([page, container.textContent]).toEqual([page, expect.stringMatching(words)]);
    expect([page, container.textContent.match(ZERO)?.[0] ?? null]).toEqual([page, null]);
  });

  test("the phone overview's cards say nothing open instead of a zero", () => {
    render(<PhoneOverview bots={[botFor("none")]} selected="btc" />);
    expect(screen.getByRole("region", { name: "Making or losing" })).toHaveTextContent("Open nownothing open");
    expect(screen.getByRole("region", { name: "Worst case right now" })).toHaveTextContent("Nothing held: a bad day costs nothing.");
  });
});

describe("Scenario: a redesign hides a loss", () => {
  test.each([
    ["a closed details", <details key="d"><summary>Result</summary><span>−0.00400 BTC</span></details>],
    ["a hidden block", <div key="h" hidden><span>−0.00400 BTC</span></div>],
    ["screen-reader text only", <span key="s" className="sr-only">−0.00400 BTC</span>],
  ])("the check fails and names the page when the result is in %s", (_, folded) => {
    const { container } = render(<main>{folded}</main>);
    expect(() => shownResult("Income", container, "−0.00400 BTC")).toThrow(/^Income: the result −0\.00400 BTC is folded away by default/);
  });

  test("a result dropped from the page fails naming the page", () => {
    const { container } = render(<main>Nothing here</main>);
    expect(() => shownResult("Positions", container, "−0.0062")).toThrow(/^Positions: the result −0\.0062 is not shown/);
  });

  test("an open details shows its result", () => {
    const { container } = render(
      <details open>
        <summary>Result</summary>
        <span>−0.00400 BTC</span>
      </details>,
    );
    expect(shownResult("Income", container, "−0.00400 BTC")).toHaveTextContent("−0.00400 BTC");
  });

  test("size and weight: a smaller or lighter loss is caught", () => {
    const { container } = render(
      <p className="text-base">
        <span className="font-semibold text-profit">+1</span>
        <span className="text-xs text-loss">−1</span>
      </p>,
    );
    const [gain, loss] = container.querySelectorAll("span");
    expect(sizeAndWeight(gain)).toEqual(["font-semibold", "text-base"]);
    expect(sizeAndWeight(loss)).toEqual(["text-xs"]);
  });
});
