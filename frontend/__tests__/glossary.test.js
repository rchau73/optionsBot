import glossary, { MAX_LONG, MAX_SHORT, buildIndex, findTerm, normalise } from "@/lib/glossary";
import { configKeys, jsxLabels, mapLabels, markdownAnchors, monitorLabels, yamlKeys } from "@/test/glossarySources";

import { longSentences, undefinedJargon } from "@/test/plainWords";

const entries = Object.entries(glossary);

// Jargon an entry may use without a glossary entry of its own: "<entry>:<word>" → why.
const JARGON_ALLOWED = {
  "implied volatility:iv": "the entry spells out its own abbreviation: 'Implied volatility (IV)'",
};

describe("glossary (#188)", () => {
  // Scenario: complete — every config key and every monitor column label has an entry.
  test("complete: every config key in the shipped config files has an entry", () => {
    const keys = configKeys();
    expect(keys.map((k) => k.key)).toEqual(expect.arrayContaining(["rollout_dte", "iv_margin_bands.max_im_pct", "backtest.fill_model"]));
    const missing = keys.filter((k) => !glossary[k.key]).map((k) => `${k.key} (${k.files.join(", ")})`);
    expect(missing).toEqual([]);
  });

  test("complete: every monitor column and label has an entry", () => {
    const labels = monitorLabels();
    // #405: Details (pro)'s positions table has no DTE column (days left are in each strangle's group row).
    expect(labels.map((l) => l.label)).toEqual(expect.arrayContaining(["OTM %", "IM %", "Stop-loss", "GEX shed"]));
    const missing = labels.filter((l) => !findTerm(l.label)).map((l) => `"${l.label}" (${l.file})`);
    expect(missing).toEqual([]);
  });

  test("the terms the story names have their own entry", () => {
    for (const term of ["im", "mm", "delta", "dte", "strangle", "slot", "repair", "shed", "gex", "gamma flip", "gamma regime", "dvol", "take-profit", "stop-loss"]) {
      expect(glossary[term]).toBeDefined();
    }
  });

  test("every entry has short, long, risk_income and a source within the limits", () => {
    for (const [term, e] of entries) {
      expect([term, typeof e.short, typeof e.long, typeof e.risk_income]).toEqual([term, "string", "string", "string"]);
      expect([term, e.short.length > 0 && e.short.length <= MAX_SHORT]).toEqual([term, true]);
      expect([term, e.long.length > 0 && e.long.length <= MAX_LONG]).toEqual([term, true]);
      expect([term, e.risk_income.length > 0]).toEqual([term, true]);
      expect([term, Object.keys(e).filter((k) => !["short", "long", "risk_income", "source", "labels"].includes(k))]).toEqual([term, []]);
    }
  });

  test("every source points at a heading that exists", () => {
    const anchors = {};
    for (const [term, e] of entries) {
      const [file, anchor] = e.source.split("#");
      expect([term, ["OptionStrategy.md", "frontend/README.md"].includes(file)]).toEqual([term, true]);
      anchors[file] ??= markdownAnchors(file);
      expect([term, anchors[file].has(anchor)]).toEqual([term, true]);
    }
  });

  test("a label names one term only", () => {
    const owner = new Map();
    for (const [term, e] of entries) {
      for (const label of e.labels ?? []) {
        const key = normalise(label);
        if (owner.has(key)) throw new Error(`label "${label}" is on "${owner.get(key)}" and "${term}"`);
        // A label may repeat its own term (the monitor's exact text), never another term.
        if (entries.some(([t]) => normalise(t) === key && t !== term)) throw new Error(`label "${label}" of "${term}" is also a term`);
        owner.set(key, term);
      }
    }
  });

  test("no return promises", () => {
    const promise = /guarantee|risk[- ]free|no risk|can(not|'t) lose|sure (profit|win|income)|always (profit|win|earn)|will earn|safe income/i;
    for (const [term, e] of entries) {
      for (const text of [e.short, e.long, e.risk_income]) expect([term, promise.test(text)]).toEqual([term, false]);
    }
  });

  test("plain sentences: none longer than 35 words", () => {
    for (const [term, e] of entries) {
      for (const sentence of `${e.short} ${e.long} ${e.risk_income}`.split(/(?<=[.!?:;])\s+/)) {
        expect([term, sentence.split(/\s+/).length <= 35 ? "ok" : sentence]).toEqual([term, "ok"]);
      }
    }
  });
});

describe("plain words (#339)", () => {
  // Scenario: A very long sentence
  test("no short tip has a sentence over 25 words", () => {
    const bad = entries.flatMap(([term, e]) => longSentences(e.short).map((s) => `${term}: ${s}`));
    expect(bad).toEqual([]);
  });

  test("a sentence over 25 words is reported, naming its text", () => {
    const long = Array(26).fill("word").join(" ");
    expect(longSentences(`Short one. ${long}.`)).toEqual([`${long}.`]);
    expect(longSentences(Array(25).fill("word").join(" "))).toEqual([]);
  });

  // Scenario: A tip uses an undefined jargon word
  test("every jargon word in a text has an entry of its own", () => {
    const bad = entries.flatMap(([term, e]) =>
      [e.short, e.long, e.risk_income].flatMap((t) => undefinedJargon(t, term, findTerm, JARGON_ALLOWED).map((w) => `${term}: ${w}`)),
    );
    expect(bad).toEqual([]);
  });

  test("an undefined jargon word is reported, a defined one or the entry's own term is not", () => {
    const g = { delta: { short: "d" }, "iv rank": { short: "r" } };
    const find = (w) => (g[w] ? { term: w } : null);
    expect(undefinedJargon("Vega and delta move with gamma.", "premium", find)).toEqual(["vega", "gamma"]);
    expect(undefinedJargon("The IV rank of vega.", "iv rank", find)).toEqual(["vega"]);
    expect(undefinedJargon("Vega here.", "premium", find, { "premium:vega": "reason" })).toEqual([]);
    expect(undefinedJargon(undefined, "x", find)).toEqual([]);
  });
});

describe("findTerm", () => {
  test("finds a term by its key, a label or a config key, ignoring case and spaces", () => {
    expect(findTerm("DTE").term).toBe("dte");
    expect(findTerm("  im   used / LIMIT ").term).toBe("im");
    expect(findTerm("P&amp;L %").term).toBe("p&l %");
    expect(findTerm("rollout_dte").short).toMatch(/15/);
  });

  test("unknown or empty labels → null, never a guess", () => {
    expect(findTerm("no such label")).toBeNull();
    expect(findTerm(undefined)).toBeNull();
    expect(findTerm("")).toBeNull();
  });

  test("a term's own key wins over another entry's label", () => {
    const g = { a: { short: "A", labels: ["b"] }, b: { short: "B" } };
    expect(findTerm("b", g, buildIndex(g)).short).toBe("B");
  });
});

describe("glossary sources", () => {
  test("jsxLabels reads literal headers, terms and label props, not expressions or aria-labels", () => {
    const src = `<th className="x" title="t">P&amp;L</th><th>{title}</th><dt className="m">Gamma flip</dt>
      <th>
        Shed release
      </th><StatTile label="Open legs" /><div aria-label="P&L range" /><Select label={x} />`;
    expect(jsxLabels(src)).toEqual(["P&L", "Gamma flip", "Shed release", "Open legs"]);
  });

  test("mapLabels reads label maps and CLOSE_LABELS", () => {
    const src = `const A = { x: { label: "stop hold", tone: "warn" } };\nconst CLOSE_LABELS = {\n  stop_loss: "Stop-loss",\n  kill_switch: "Kill switch",\n};`;
    expect(mapLabels(src)).toEqual(["stop hold", "Stop-loss", "Kill switch"]);
  });

  test("yamlKeys reads nested maps, lists of maps and inline maps, and skips comments", () => {
    const text = [
      "# a comment: with a colon",
      "underlying: BTC",
      "dte_delta_matrix:",
      "  - dte: 25",
      "    deltas: [0.16]   # trailing: comment",
      "iv_margin_bands:",
      "  - { min_iv_pct: 70, max_im_pct: 50 }",
      "backtest:",
      "  fill_model:  mid",
      "max_mm_pct: 35",
    ].join("\n");
    expect(yamlKeys(text)).toEqual([
      "underlying",
      "dte_delta_matrix",
      "dte_delta_matrix.dte",
      "dte_delta_matrix.deltas",
      "iv_margin_bands",
      "iv_margin_bands.min_iv_pct",
      "iv_margin_bands.max_im_pct",
      "backtest",
      "backtest.fill_model",
      "max_mm_pct",
    ]);
  });

  test("a new config key without an entry is reported", () => {
    const keys = yamlKeys("rollout_dte: 15\nbrand_new_setting: 3\n");
    expect(keys.filter((k) => !glossary[k])).toEqual(["brand_new_setting"]);
  });

  test("a new monitor label without an entry is reported", () => {
    const labels = jsxLabels("<th>DTE</th><th>Brand new column</th>");
    expect(labels.filter((l) => !findTerm(l))).toEqual(["Brand new column"]);
  });
});
