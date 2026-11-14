import { render, within } from "@testing-library/react";
import InvestorSummary from "@/components/InvestorSummary";
import PhoneOverview from "@/components/PhoneOverview";
import { formatCoin, formatDate, formatNumber, formatUSD } from "@/lib/format";
import { formatSignedUSD, monthLabel } from "@/lib/summary";
import DEMO from "@/test/demoScenarios.json";
import { assertNotFolded, sizeAndWeight } from "@/test/lossVisibility";

// #743 (epic E3): the usability round's four questions (docs/usability/
// e3-investor-monitor-test.md, sections 6 and 6b) answered by the first
// screen, on the demo's own data for each scenario: the desktop investor
// summary and the phone overview. test/demoScenarios.json is what
// cmd/monitor-demo serves; tests/usability_keys_test.go checks it against
// the kit's answer keys (and rewrites it with -update-scenarios). A failure
// names the scenario, the layout and the question.

jest.mock("@/components/RegimeChart", () => () => null);

// The kit's scenario names → the fixture's (the no-flag demo is "default").
const SCENARIOS = { baseline: "default", mixed: "mixed", drawdown: "drawdown" };

const botOf = (scenario) => {
  const served = DEMO[SCENARIOS[scenario]];
  return { name: "btc", stale: false, status: served["/api/status"], summary: served["/api/summary"], positions: served["/api/positions"] };
};

const LAYOUTS = {
  desktop: (bot) => render(<InvestorSummary bots={[bot]} />).container,
  phone: (bot) => render(<PhoneOverview bots={[bot]} selected="btc" />).container,
};

// Where each question is answered: a block of the desktop card, a card of the phone overview.
const BLOCKS = {
  desktop: { 1: "What it holds", 2: "Income per month (realised, net of fees)", 3: "Worst case now (one day)", 4: "Last action" },
  phone: { 1: "What it holds", 2: "Making or losing", 3: "Worst case right now", 4: /^Last action/ },
};

const FOLDED = "[hidden], [aria-hidden='true'], .hidden, .sr-only, details:not([open]) > :not(summary)";

/** The text a reader sees: without what is folded away (hidden, sr-only, a closed details). */
function text(el) {
  const copy = el.cloneNode(true);
  for (const n of copy.querySelectorAll(FOLDED)) n.remove();
  return copy.textContent.replace(/\s+/g, " ");
}

function need(ok, message) {
  if (!ok) throw new Error(message);
}

/** The block that answers question q, shown (not folded away); throws when it is not on the first screen. */
function block(container, layout, q) {
  const name = BLOCKS[layout][q];
  let el = null;
  if (layout === "desktop") {
    const card = within(container).queryByRole("region", { name: "BTC · investor summary" });
    need(card, "the BTC · investor summary card is not shown");
    el = within(card).queryByRole("heading", { level: 3, name })?.parentElement ?? null;
  } else {
    el = within(container).queryByRole("region", { name });
  }
  need(el, `the block "${name}" is not on the first screen`);
  return assertNotFolded(String(name), el, container, `"${name}"`);
}

function includes(el, wanted, what) {
  need(text(el).includes(wanted), `${what}: "${wanted}" is not shown (the block reads "${text(el).slice(0, 300)}")`);
}

const monthName = (month) => new Date(`${month}-15T00:00:00Z`).toLocaleString("en-US", { month: "long", timeZone: "UTC" });

/** The desktop income table's row of a month. */
function monthRow(el, month) {
  const row = within(el)
    .queryAllByRole("row")
    .find((r) => r.querySelector("th")?.textContent.startsWith(monthLabel(month)));
  need(row, `the month ${monthLabel(month)} has no row`);
  return row;
}

/** The phone's figure under a dt label ("This month", "Open now"). */
function figureUnder(el, label) {
  const dt = within(el)
    .queryAllByText(label)
    .find((n) => n.tagName === "DT");
  need(dt, `no "${label}" figure`);
  return dt.nextElementSibling;
}

// One check per question and layout, from the rendered text only.
const CHECKS = {
  desktop: {
    1: (c, s) => {
      const el = block(c, "desktop", 1);
      const h = s.holding;
      includes(el, `${h.strangles} strangles`, "the count of strangles");
      includes(el, `(${h.calls} calls, ${h.puts} puts)`, "the calls and puts");
      includes(el, `${formatNumber(h.notional_coin, 2)} ${h.coin} sold`, "what is sold, in coin");
      includes(el, `Nearest expiry ${formatDate(h.nearest_expiry)}`, "the nearest expiry");
    },
    2: (c, s) => {
      const el = block(c, "desktop", 2);
      for (const m of s.income.months.filter((x) => x.closes > 0)) {
        const row = monthRow(el, m.month);
        includes(row, formatCoin(m.realised_coin, s.income.coin), `${monthLabel(m.month)} in coin with its sign`);
        includes(row, formatSignedUSD(m.realised_usd), `${monthLabel(m.month)} in USD with its sign`);
        need(m.realised_coin < 0 === / · loss/.test(text(row)), `${monthLabel(m.month)} ${m.realised_coin < 0 ? "is a loss not named in words" : "is a gain named a loss"}`);
      }
      // The open line is apart from the month's result and says it is not final.
      includes(el, `Open positions now (not realised): ${formatCoin(s.income.unrealised_coin, s.income.coin)}`, "the open positions, labelled not realised");
      const tm = s.income.this_month;
      includes(monthRow(el, tm.month), formatCoin(tm.realised_coin, s.income.coin), "this month's result without the open positions");
    },
    3: (c, s) => {
      const el = block(c, "desktop", 3);
      const wc = s.worst_case;
      for (const side of [wc.down, wc.up].filter((x) => x.loss_coin > 0)) {
        includes(el, `${formatCoin(-side.loss_coin, wc.coin)} · ${formatSignedUSD(-side.loss_usd)}`, "the worst case as a loss in BTC and USD");
      }
      includes(el, "This is a model, not a quote", "the worst case marked as an estimate");
    },
    4: (c, s) => {
      const el = block(c, "desktop", 4);
      const a = s.last_action;
      includes(el, a.reason, "the reason of the last action");
      includes(el, `${a.kind === "close" ? "bought back" : "sold"} ${formatNumber(a.qty, 2)} × ${a.instrument}`, "what the last action did");
    },
  },
  phone: {
    1: (c, s) => {
      const el = block(c, "phone", 1);
      const h = s.holding;
      need(within(c).queryByRole("button", { name: "BTC", pressed: true }), "the coin shown (BTC) is not selected in the header");
      includes(el, `${h.legs} positions`, "the count of options held");
      includes(el, `BTC stays between ${formatUSD(h.nearest.put_strike)} and ${formatUSD(h.nearest.call_strike)} until ${formatDate(h.nearest.expiry)}`, "the nearest strikes and expiry");
    },
    2: (c, s) => {
      const el = block(c, "phone", 2);
      const tm = s.income.this_month;
      const headline = within(c).queryByRole("heading", { level: 1 });
      need(headline, "no headline");
      includes(headline, `This month: ${formatCoin(tm.realised_coin, "BTC")} after fees`, "the month's result with its sign and unit");
      need(text(figureUnder(el, "This month")) === formatCoin(tm.realised_coin, "BTC"), "this month's figure is not the realised result alone");
      need(text(figureUnder(el, "Open now")) === formatCoin(s.income.unrealised_coin, "BTC"), "the open positions are not shown apart");
      const bars = within(el).queryByTestId("month-bars");
      need(bars, "the six month bars are not shown");
      for (const m of s.income.months.filter((x) => x.closes > 0 && x.realised_coin < 0)) {
        need(new RegExp(`\\b${monthName(m.month)}\\b.* down`).test(bars.getAttribute("aria-label")), `the losing month ${monthName(m.month)} is not named down (${bars.getAttribute("aria-label")})`);
      }
    },
    3: (c, s) => {
      const el = block(c, "phone", 3);
      const wc = s.worst_case;
      const worse = wc.up.loss_coin > wc.down.loss_coin ? wc.up : wc.down;
      includes(el, `would cost about ${formatCoin(worse.loss_coin, "BTC").replace(/^[+−]/, "")} (${formatUSD(worse.loss_usd)})`, "the worst case as a cost in BTC and USD");
    },
    4: (c, s) => {
      const el = block(c, "phone", 4);
      includes(el, s.last_action.reason, "the reason of the last action");
    },
  },
};

/** Runs one question's check, naming the scenario, the layout and the question when it fails. */
function check(scenario, layout, q, container) {
  try {
    CHECKS[layout][q](container, botOf(scenario).summary);
  } catch (e) {
    throw new Error(`scenario ${scenario} · layout ${layout} · question ${q}: ${e.message}`);
  }
}

afterEach(() => {
  document.body.innerHTML = "";
});

const CASES = Object.keys(SCENARIOS).flatMap((scenario) => Object.keys(LAYOUTS).flatMap((layout) => [1, 2, 3, 4].map((q) => [scenario, layout, q])));

describe("Scenario: each demo scenario answers the four questions on the first screen", () => {
  test.each(CASES)("%s · %s · question %d", (scenario, layout, q) => {
    check(scenario, layout, q, LAYOUTS[layout](botOf(scenario)));
  });
});

describe("Scenario: the baseline scenario answers all four questions on desktop", () => {
  test("it names what is held, says the bot is making money, shows the worst case in money and the last action with its reason", () => {
    const s = botOf("baseline").summary;
    const c = LAYOUTS.desktop(botOf("baseline"));
    for (const q of [1, 2, 3, 4]) check("baseline", "desktop", q, c);
    // Making money: every full past month is a gain, none named a loss.
    const past = s.income.months.slice(0, -1).filter((m) => m.closes > 0);
    expect(past.length).toBeGreaterThan(0);
    expect(past.every((m) => m.realised_coin > 0)).toBe(true);
    expect(text(block(c, "desktop", 2))).not.toMatch(/ · loss/);
  });
});

// The loss figures of a layout, located the same way in any scenario.
const FIGURES = {
  desktop: (s, months) => [
    ...months.map((m) => ({ what: `${monthLabel(m)} in coin`, value: s.income.months.find((x) => x.month === m).realised_coin, find: (cc, ss) => within(monthRow(block(cc, "desktop", 2), m)).queryByText(formatCoin(ss.income.months.find((x) => x.month === m).realised_coin, "BTC")) })),
    { what: "open positions", value: s.income.unrealised_coin, find: (cc, ss) => within(block(cc, "desktop", 2)).queryByText(formatCoin(ss.income.unrealised_coin, "BTC")) },
  ],
  phone: (s) => [
    { what: "the headline month", value: s.income.this_month.realised_coin, find: (cc, ss) => within(within(cc).getByRole("heading", { level: 1 })).queryByText(formatCoin(ss.income.this_month.realised_coin, "BTC")) },
    { what: "this month", value: s.income.this_month.realised_coin, find: (cc) => figureUnder(block(cc, "phone", 2), "This month").firstElementChild },
    { what: "open now", value: s.income.unrealised_coin, find: (cc) => figureUnder(block(cc, "phone", 2), "Open now").firstElementChild },
  ],
};

/** An element's place: its tag path from the container, and its size and weight. */
function prominence(el, container) {
  const path = [];
  for (let n = el; n && n !== container; n = n.parentElement) path.unshift(n.tagName.toLowerCase());
  return { path: path.join(">"), size: sizeAndWeight(el) };
}

describe("Scenario: the drawdown scenario shows the loss as clearly as a gain", () => {
  const drawdown = botOf("drawdown").summary;
  const losingMonths = drawdown.income.months.filter((m) => m.closes > 0 && m.realised_coin < 0).map((m) => m.month);

  test("the drawdown demo has a losing month to show", () => {
    expect(losingMonths.length).toBeGreaterThan(0);
  });

  test.each(Object.keys(LAYOUTS))("%s: each loss carries its sign and unit, in the element, size and place of the baseline's gain", (layout) => {
    const losses = FIGURES[layout](drawdown, losingMonths).filter((f) => f.value < 0);
    expect(losses.length).toBeGreaterThan(0);
    const baseline = botOf("baseline").summary;
    for (const f of losses) {
      const where = `scenario drawdown · layout ${layout} · question 2 (${f.what})`;
      const lossPage = LAYOUTS[layout](botOf("drawdown"));
      const loss = f.find(lossPage, drawdown);
      if (!loss) throw new Error(`${where}: the loss is not shown`);
      assertNotFolded(where, loss, lossPage, loss.textContent);
      expect([where, loss.textContent]).toEqual([where, expect.stringMatching(/^−[\d.]+ BTC$/)]);
      const lossPlace = prominence(loss, lossPage);
      document.body.innerHTML = "";

      const gainPage = LAYOUTS[layout](botOf("baseline"));
      const gain = f.find(gainPage, baseline);
      if (!gain) throw new Error(`scenario baseline · layout ${layout} · question 2 (${f.what}): the figure is not shown`);
      expect([where, lossPlace]).toEqual([where, prominence(gain, gainPage)]);
      document.body.innerHTML = "";
    }
  });

  test("phone: a losing month's bar is drawn as a loss, at the place of the baseline's month", () => {
    const lossBars = within(LAYOUTS.phone(botOf("drawdown"))).getByTestId("month-bars");
    const months = drawdown.income.months.map((m) => m.month);
    const lossOrder = [...lossBars.children].map((b) => b.textContent);
    for (const m of losingMonths) {
      const bar = lossBars.children[months.indexOf(m)].firstElementChild;
      expect([m, bar.dataset.loss]).toEqual([m, "true"]);
      expect(parseFloat(bar.style.height)).toBeGreaterThanOrEqual(6);
    }
    document.body.innerHTML = "";
    const gainBars = within(LAYOUTS.phone(botOf("baseline"))).getByTestId("month-bars");
    expect([...gainBars.children].map((b) => b.textContent)).toEqual(lossOrder);
  });

  test("desktop: the losing month is named a loss in words, not by colour alone", () => {
    const el = block(LAYOUTS.desktop(botOf("drawdown")), "desktop", 2);
    for (const m of losingMonths) expect(text(monthRow(el, m))).toMatch(/ · loss/);
  });
});

describe("Scenario: the mixed scenario keeps the open line apart from the final result", () => {
  const s = botOf("mixed").summary;
  const tm = s.income.this_month;
  const added = formatCoin(tm.realised_coin + s.income.unrealised_coin, "BTC");

  test("the demo's open line is not 0, so adding it would show", () => {
    expect(added).not.toBe(formatCoin(tm.realised_coin, "BTC"));
  });

  test.each(Object.keys(LAYOUTS))("%s: the open figure is labelled not yet final and is not added to the month", (layout) => {
    const c = LAYOUTS[layout](botOf("mixed"));
    check("mixed", layout, 2, c);
    const el = block(c, layout, 2);
    if (layout === "desktop") {
      expect(text(el)).toMatch(/Open positions now \(not realised\)/);
      expect(text(monthRow(el, tm.month))).not.toContain(added);
    } else {
      expect(text(figureUnder(el, "This month"))).not.toBe(added);
      expect(text(within(c).getByRole("heading", { level: 1 }))).not.toContain(added);
    }
  });
});

describe("Scenario: a regression names the question", () => {
  test("a phone overview without its worst case fails naming the scenario, the layout phone and question 3", () => {
    const c = LAYOUTS.phone(botOf("baseline"));
    within(c).getByRole("region", { name: "Worst case right now" }).remove();
    expect(() => check("baseline", "phone", 3, c)).toThrow(/^scenario baseline · layout phone · question 3: the block "Worst case right now" is not on the first screen/);
  });

  test.each([
    ["the card", (card) => card],
    ["its amount", (card) => card.querySelector("p")],
  ])("a worst case with %s folded away fails the same way", (_, part) => {
    const c = LAYOUTS.phone(botOf("drawdown"));
    part(within(c).getByRole("region", { name: "Worst case right now" })).hidden = true;
    expect(() => check("drawdown", "phone", 3, c)).toThrow(/^scenario drawdown · layout phone · question 3: /);
  });

  test("a losing month without its word fails naming question 2 on desktop", () => {
    const c = LAYOUTS.desktop(botOf("drawdown"));
    for (const word of within(c).getAllByText("· loss")) word.remove();
    expect(() => check("drawdown", "desktop", 2, c)).toThrow(/^scenario drawdown · layout desktop · question 2: .* is a loss not named in words/);
  });

  test("a last action without its reason fails naming question 4", () => {
    const c = LAYOUTS.desktop(botOf("mixed"));
    within(c).getByText(botOf("mixed").summary.last_action.reason).remove();
    expect(() => check("mixed", "desktop", 4, c)).toThrow(/^scenario mixed · layout desktop · question 4: the reason of the last action/);
  });
});

// #781: the baseline demo with nothing held, idle for a freeze or waiting for
// a free expiry — "why is it not trading" answered under question 1.
const IDLE = {
  frozen: { reason: "risk_frozen", blocked: true, held: false, seq: 41, at: "2026-11-14T09:00:00Z", plain: "No new strangle: new risk is paused.", detail: "regime NEGATIVE (1/2 closes)" },
  waiting: { reason: "no_free_expiry", blocked: false, held: false, seq: 42, at: "2026-11-14T09:05:00Z", plain: "No new strangle in this slot.", detail: "every expiry up to 40 DTE is held by another slot" },
};
const idleBot = (idle) => {
  const bot = botOf("baseline");
  return { ...bot, summary: { ...bot.summary, holding: { ...bot.summary.holding, strangles: 0, legs: 0, calls: 0, puts: 0, nearest: null }, idle } };
};

describe("Scenario: nothing held — the first screen says why the bot is not trading", () => {
  test.each(Object.keys(LAYOUTS).flatMap((layout) => Object.keys(IDLE).map((kind) => [layout, kind])))("%s · idle %s", (layout, kind) => {
    const c = LAYOUTS[layout](idleBot(IDLE[kind]));
    const el = block(c, layout, 1);
    const note = within(el).getByTestId("idle-note-btc");
    assertNotFolded(`layout ${layout} · idle ${kind}`, note, c, "the idle note");
    expect(text(note)).toMatch(kind === "frozen" ? /Not opening new positions\. New risk is paused/ : /Waiting\. Every expiry date in this slot's range is already used/);
    expect(text(note)).toContain("You need to do nothing.");
    expect(within(note).getByRole("button", { name: "Why?" })).toHaveClass("min-h-11", "min-w-11");
  });

  test("the baseline, holding strangles, shows no idle note", () => {
    expect(within(LAYOUTS.phone(botOf("baseline"))).queryByTestId("idle-note-btc")).toBeNull();
  });
});
