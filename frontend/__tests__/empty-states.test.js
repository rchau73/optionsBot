import emptyStates, { MAX_TEXT, PANELS, STATES, emptyStateFor } from "@/lib/emptyStates";
import { brokenSources, missingStates, monitorPanels, promiseWords, tooLong } from "@/test/emptyStateChecks";
import { longSentences, undefinedJargon } from "@/test/plainWords";

const fullEntry = {
  empty: "Nothing yet.",
  loading: "Loading.",
  stale: "Old data.",
  error: "Could not read.",
  source: "frontend/docs/menu-and-pages.md#what-you-see",
};

describe("empty states (#298)", () => {
  // Scenario: Every panel is covered
  test("every panel is covered: each panel key the monitor renders has empty, loading, stale and error", () => {
    const rendered = monitorPanels();
    // #394: Account and Activity are their own pages. #404: the overview's P&L chart is no panel. #405: Details
    // (pro) is its own page, its sections not collapsible; the USD P&L over every range is its last, folded panel.
    expect(rendered.storageKeys).toEqual(["monitor.panel.pnl"]);
    expect(rendered.titles).toEqual(
      expect.arrayContaining([
        "Open positions",
        "Missing legs: why repair holds them",
        "Trades and results",
        "Working orders (placed, not filled yet)",
        "P&L in USD (every range)",
      ]),
    );
    expect(rendered.blocks).toEqual(["<Header", "<BotDashboards", "<CountsLine"]);
    expect(missingStates(emptyStates, PANELS, rendered)).toEqual([]);
  });

  test("every panel is covered: a missing state, entry or panel key is named", () => {
    const { stale: _stale, ...noStale } = fullEntry;
    const panels = { "monitor.panel.a": "A", "monitor.b": "B" };
    const rendered = { storageKeys: ["monitor.panel.a", "monitor.panel.new"], titles: ["A", "New panel"], blocks: [] };
    expect(missingStates({ "monitor.panel.a": noStale }, panels, rendered)).toEqual([
      "monitor.panel.new: not in PANELS",
      "New panel: no panel key in PANELS",
      "monitor.panel.a: missing stale",
      "monitor.b: no entry",
    ]);
  });

  test("the examples the story names are covered", () => {
    expect(emptyStateFor("monitor.panel.positions", "empty")).toMatch(/no open strangle/i);
    expect(emptyStateFor("monitor.panel.trades", "empty")).toMatch(/first close/i);
    expect(emptyStateFor("monitor.panel.pnl", "empty")).toMatch(/report interval/i);
    expect(emptyStateFor("monitor.panel.account", "error")).toMatch(/account not loaded/i);
    expect(emptyStateFor("monitor.header", "error")).toMatch(/not answering/i);
    expect(emptyStateFor("monitor.gex", "empty")).toMatch(/GEX not computed/i);
  });

  test(`each text is at most ${MAX_TEXT} characters and entries have no unknown fields`, () => {
    expect(tooLong(emptyStates)).toEqual([]);
    expect(tooLong({ "monitor.x": { ...fullEntry, error: "x".repeat(MAX_TEXT + 1), note: "" } })).toEqual([
      `monitor.x.error: ${MAX_TEXT + 1} chars`,
      "monitor.x: unknown fields note",
    ]);
  });

  // Scenario: No promise in an empty state
  test("no promise in an empty state: the catalogue has no forecast or advice word", () => {
    expect(promiseWords(emptyStates)).toEqual([]);
  });

  test("no promise in an empty state: a forecast or advice word fails naming the panel and the word", () => {
    const bad = { "monitor.pnl": { ...fullEntry, empty: "The bot will earn premium here.", stale: "You should buy back now." } };
    expect(promiseWords(bad)).toEqual(["monitor.pnl.empty: will earn", "monitor.pnl.stale: should buy", "monitor.pnl.stale: you should"]);
  });

  // Scenario: Broken source
  test("broken source: every source anchor exists", () => {
    expect(brokenSources(emptyStates)).toEqual([]);
  });

  test("broken source: an anchor not in the document, or another document, fails naming the entry", () => {
    const bad = {
      "monitor.ok": fullEntry,
      "monitor.gone": { ...fullEntry, source: "frontend/docs/menu-and-pages.md#no-such-heading" },
      "monitor.elsewhere": { ...fullEntry, source: "README.md#what-you-see" },
      "monitor.readme": { ...fullEntry, source: "frontend/README.md#what-you-see" },
      "monitor.none": { ...fullEntry, source: undefined },
    };
    expect(brokenSources(bad)).toEqual([
      "monitor.gone: frontend/docs/menu-and-pages.md#no-such-heading",
      "monitor.elsewhere: README.md#what-you-see",
      "monitor.readme: frontend/README.md#what-you-see",
      "monitor.none: undefined",
    ]);
  });
});

describe("plain words (#339)", () => {
  // Scenario: A very long sentence
  test("no empty-state sentence is over 25 words", () => {
    const bad = Object.entries(emptyStates).flatMap(([panel, e]) =>
      STATES.flatMap((s) => longSentences(e[s]).map((t) => `${panel}.${s}: ${t}`)),
    );
    expect(bad).toEqual([]);
  });

  // Scenario: A tip uses an undefined jargon word
  test("every jargon word in an empty state has a glossary entry", () => {
    const bad = Object.entries(emptyStates).flatMap(([panel, e]) =>
      STATES.flatMap((s) => undefinedJargon(e[s], panel).map((w) => `${panel}.${s}: ${w}`)),
    );
    expect(bad).toEqual([]);
  });

  test("a long sentence or an undefined jargon word in an empty state is named", () => {
    const text = `${Array(26).fill("word").join(" ")}. Vega moves.`;
    expect(longSentences(text)).toHaveLength(1);
    expect(undefinedJargon(text, "monitor.x", () => null)).toEqual(["vega"]);
  });
});

describe("emptyStateFor", () => {
  test("returns the sentence for a known panel and state", () => {
    for (const key of Object.keys(PANELS)) {
      for (const state of STATES) expect(emptyStateFor(key, state)).toBe(emptyStates[key][state]);
    }
  });

  test("unknown panel or state → null, never a guess", () => {
    expect(emptyStateFor("monitor.nope", "empty")).toBeNull();
    expect(emptyStateFor("monitor.panel.positions", "source")).toBeNull();
    expect(emptyStateFor("monitor.panel.positions", "broken")).toBeNull();
    expect(emptyStateFor(undefined, undefined)).toBeNull();
  });

  test("reads a catalogue passed in", () => {
    expect(emptyStateFor("x", "empty", { x: fullEntry })).toBe("Nothing yet.");
  });
});
