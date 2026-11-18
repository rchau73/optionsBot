import { render, within } from "@testing-library/react";
import InvestorSummary from "@/components/InvestorSummary";
import PhoneOverview from "@/components/PhoneOverview";
import DEMO from "@/test/demoScenarios.json";
import { longSentences, undefinedJargon } from "@/test/plainWords";

// #844 (epic E3): the words a first-time visitor reads on the first screen —
// the four sections of the investor summary and of the phone overview — on
// every demo scenario, checked as the glossary and the idle sentences are
// (test/plainWords.js): no sentence over 25 words, no jargon word without its
// glossary entry. Numbers, coin and instrument names are not words to check.
// A failure names the scenario, the layout, the section and the sentence or term.

jest.mock("@/components/RegimeChart", () => () => null);

// The kit's scenario names → the fixture's (as in scenario-answers.test.jsx).
const SCENARIOS = { baseline: "default", mixed: "mixed", drawdown: "drawdown", hedged: "hedged" };

const botOf = (scenario) => {
  const served = DEMO[SCENARIOS[scenario]];
  return { name: "btc", stale: false, status: served["/api/status"], summary: served["/api/summary"], positions: served["/api/positions"] };
};

const LAYOUTS = {
  desktop: (bot) => render(<InvestorSummary bots={[bot]} />).container,
  phone: (bot) => render(<PhoneOverview bots={[bot]} selected="btc" />).container,
};

// The four first-screen sections of each layout.
const SECTIONS = {
  desktop: ["What it holds", "Income per month (realised, net of fees)", "Worst case now (one day)", "Last action"],
  phone: ["What it holds", "Making or losing", "Worst case right now", /^Last action/],
};

/**
 * Jargon words a section may show anyway ("section:word" → reason), as
 * undefinedJargon's `allow` argument reads them. Keep each reason to one line.
 */
const ALLOW = {};

/**
 * Known long sentences (as a11y-tokens' KNOWN_FINDINGS): written by the bot
 * itself, so not fixable in the components, and quoted word for word by the
 * usability kit's answer key. A new long sentence fails; a fixed one must
 * leave this list (the test below fails while it stays).
 */
const KNOWN_LONG = {
  "Traded the perpetual future to bring the book's direction (delta) back near zero: the futures hedge experiment keeps the options and offsets their direction instead of buying a side back.":
    "the hedge trade's reason from internal/api/explain.go (Go, out of scope of #844); shortening it changes the kit's answer key and the demo fixture",
};

const FOLDED = "[hidden], [aria-hidden='true'], .hidden, .sr-only, details:not([open]) > :not(summary)";
// Each of these is read as its own text: a heading, a line, a row, a link.
const LINES = "h1, h2, h3, p, li, tr, a, dt, dd, button";
// An instrument name (BTC-27NOV26-110000-C, BTC-PERPETUAL) is a name, not words.
const INSTRUMENT = /\b[A-Z]{2,}(?:-[A-Z0-9]+)+\b/g;

/** The section's element, or null when it is not shown. */
function section(container, layout, name) {
  if (layout === "phone") return within(container).queryByRole("region", { name });
  const card = within(container).queryByRole("region", { name: "BTC · investor summary" });
  return card ? (within(card).queryByRole("heading", { level: 3, name })?.parentElement ?? null) : null;
}

/** The lines a reader sees in an element: folded parts removed, one text per innermost line element. */
function visibleLines(el) {
  const copy = el.cloneNode(true);
  for (const n of copy.querySelectorAll(FOLDED)) n.remove();
  const lines = [...copy.querySelectorAll(LINES)].filter((n) => !n.querySelector(LINES));
  return lines.map((n) => n.textContent.replace(INSTRUMENT, "").replace(/\s+/g, " ").trim()).filter(Boolean);
}

/** The plain-words problems of one section's lines, each naming the section and the sentence or term. */
function wordProblems(name, lines, allow = ALLOW, known = KNOWN_LONG) {
  const problems = [];
  for (const line of lines) {
    for (const s of longSentences(line).filter((x) => !known[x])) problems.push(`section "${name}": a sentence over 25 words: "${s}"`);
    for (const term of undefinedJargon(line, name, undefined, allow)) problems.push(`section "${name}": the term "${term}" is not explained: "${line}"`);
  }
  return problems;
}

/** Every problem of a rendered layout, prefixed with its scenario and layout. */
function problemsOf(scenario, layout, container) {
  const problems = [];
  for (const name of SECTIONS[layout]) {
    const el = section(container, layout, name);
    if (!el) {
      problems.push(`section "${name}" is not on the first screen`);
      continue;
    }
    problems.push(...wordProblems(String(name), visibleLines(el)));
  }
  return problems.map((p) => `scenario ${scenario} · layout ${layout} · ${p}`);
}

afterEach(() => {
  document.body.innerHTML = "";
});

const CASES = Object.keys(SCENARIOS).flatMap((scenario) => Object.keys(LAYOUTS).map((layout) => [scenario, layout]));

describe("Scenario: Every scenario passes on both layouts", () => {
  test.each(CASES)("%s · %s: short sentences, every jargon term explained", (scenario, layout) => {
    expect(problemsOf(scenario, layout, LAYOUTS[layout](botOf(scenario)))).toEqual([]);
  });

  test("each section is read and has words to check", () => {
    for (const [scenario, layout] of CASES) {
      const c = LAYOUTS[layout](botOf(scenario));
      for (const name of SECTIONS[layout]) expect([scenario, layout, String(name), visibleLines(section(c, layout, name)).length > 0]).toEqual([scenario, layout, String(name), true]);
      document.body.innerHTML = "";
    }
  });

  test("every allow-listed term names a section and carries a reason", () => {
    for (const [key, reason] of Object.entries(ALLOW)) {
      expect(key).toMatch(/^.+:[a-z]+$/);
      expect(String(reason).trim().length).toBeGreaterThan(0);
    }
  });

  test("every known long sentence is still on a first screen and still long (a fixed one leaves KNOWN_LONG)", () => {
    const seen = new Set();
    for (const [scenario, layout] of CASES) {
      const c = LAYOUTS[layout](botOf(scenario));
      for (const name of SECTIONS[layout]) for (const line of visibleLines(section(c, layout, name))) for (const s of longSentences(line)) seen.add(s);
      document.body.innerHTML = "";
    }
    for (const [sentence, reason] of Object.entries(KNOWN_LONG)) {
      expect(String(reason).trim().length).toBeGreaterThan(0);
      expect([sentence, seen.has(sentence)]).toEqual([sentence, true]);
    }
  });
});

describe("Scenario: A new long sentence is caught", () => {
  test("a 30-word sentence in the Last action fails naming the scenario, the layout, the section and the sentence", () => {
    const bot = botOf("mixed");
    const reason = Array.from({ length: 30 }, (_, i) => `word${"abcdefghijklmnopqrstuvwxyz"[i % 26]}`).join(" ") + ".";
    const c = LAYOUTS.desktop({ ...bot, summary: { ...bot.summary, last_action: { ...bot.summary.last_action, reason } } });
    expect(problemsOf("mixed", "desktop", c)).toEqual([`scenario mixed · layout desktop · section "Last action": a sentence over 25 words: "${reason}"`]);
  });

  test("a sentence of exactly 25 words passes", () => {
    const line = Array.from({ length: 25 }, () => "word").join(" ") + ".";
    expect(wordProblems("Last action", [line])).toEqual([]);
  });
});

describe("Scenario: A new unexplained term is caught", () => {
  const noGlossary = () => null;

  test("the word gamma in the Last action with no glossary entry fails naming the term", () => {
    const bot = botOf("baseline");
    const reason = "Bought back the call: gamma turned against it.";
    const c = LAYOUTS.phone({ ...bot, summary: { ...bot.summary, last_action: { ...bot.summary.last_action, reason } } });
    const lines = visibleLines(section(c, "phone", /^Last action/));
    const problems = [];
    for (const line of lines) problems.push(...undefinedJargon(line, "Last action", noGlossary));
    expect(problems).toEqual(["gamma"]);
  });

  test("a term on the allow-list passes; the same term elsewhere still fails", () => {
    const allow = { "Last action:gamma": "test only" };
    expect(undefinedJargon("gamma turned.", "Last action", noGlossary, allow)).toEqual([]);
    expect(undefinedJargon("gamma turned.", "What it holds", noGlossary, allow)).toEqual(["gamma"]);
  });

  test("numbers, coin and instrument names are not words to check", () => {
    const el = document.createElement("div");
    el.innerHTML = "<p>sold 1.00 × BTC-27NOV26-110000-C</p><p>sold 300 USD of the perpetual (BTC-PERPETUAL)</p>";
    expect(visibleLines(el)).toEqual(["sold 1.00 ×", "sold 300 USD of the perpetual ()"]);
  });

  test("a folded line is not read", () => {
    const el = document.createElement("div");
    el.innerHTML = '<p>shown</p><p class="sr-only">gamma</p><p hidden>delta</p>';
    expect(visibleLines(el)).toEqual(["shown"]);
  });
});
