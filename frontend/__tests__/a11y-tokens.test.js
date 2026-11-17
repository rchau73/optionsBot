// Colour contrast of the monitor's text on its backgrounds (#763, epic E12):
// every text-on-background pair the pages use (PAIRS) is measured from the
// tokens of app/globals.css and Tailwind's palette against WCAG AA (4.5:1
// body text, 3:1 large text). Today's failures are pinned in KNOWN_FINDINGS:
// they pass, a new failing pair fails naming it, and a pinned pair that now
// meets AA fails until it is removed. The ranked list and what each fix needs
// are in docs/product/responsive-a11y-baseline.md. No colour changes here.
import fs from "node:fs";
import path from "node:path";
import { AA_BODY, AA_LARGE, compareContrast, contrastRatio, measurePairs, meetsAA, MIN_TAP_PX, mix, parseColor, tapTargetOk, themeTokens } from "@/lib/a11y";
import { TAILWIND_PALETTE } from "@/test/tailwindPalette";

const ROOT = path.join(__dirname, "..");
const CSS = fs.readFileSync(path.join(ROOT, "app", "globals.css"), "utf8");

// The monitor has one theme, dark (#0b1120 surface, no light variant): its
// tokens, plus "body", the page's default text colour (html, body { color }).
function darkTokens(css) {
  const body = css.match(/html,\s*body\s*\{[^}]*\bcolor:\s*([^;]+);/);
  return { ...themeTokens(css), body: body ? body[1].trim() : undefined };
}
const THEMES = { dark: darkTokens(CSS) };

// The pairs the pages use: text colour (the class after text-), background
// (after bg-; one with an opacity is painted over `under`, else the panel),
// and one place it is seen. A new text colour in a component needs its pair
// here (the last test checks every text-* colour class is covered).
const on = (bg, texts, where, extra = {}) => texts.map((text) => ({ text, bg, where, ...extra }));
const ACCENTS = ["profit", "loss", "warn", "testnet", "call", "put", "action"];
const PAIRS = [
  ...on("surface", ["body", "muted", "slate-100", ...ACCENTS], "page background (Filters, menu bar)"),
  ...on("panel", ["body", "muted", "slate-200", ...ACCENTS], "panel body (every page)"),
  ...on("slate-800", ["body", "muted", "slate-300"], "chips, tabs and the consent bar"),
  ...on("slate-800", ["slate-500"], "a disabled Rebalance / hedge button"),
  ...on("slate-900", ["body", "muted"], "the assistant's and survey's text boxes"),
  ...on("slate-700/60", ["slate-200"], "Badge neutral"),
  ...on("slate-800/40", ["slate-200"], "Close dialog, not rebuilt"),
  ...on("profit/10", ["profit"], "success notes"),
  ...on("profit/15", ["profit"], "Badge good"),
  ...on("loss/10", ["loss"], "error banners (Monitor, Details)"),
  ...on("loss/15", ["loss"], "Badge bad and form errors"),
  ...on("loss/20", ["loss"], "request draft chip"),
  ...on("warn/10", ["warn"], "hedge notes"),
  ...on("warn/15", ["warn"], "Badge warn"),
  ...on("warn/20", ["warn"], "Details slot needing attention"),
  ...on("testnet/20", ["testnet"], "Badge testnet, the Overview's P&L tab"),
  ...on("call/15", ["call"], "Badge call"),
  ...on("put/15", ["put"], "Badge put"),
  ...on("action/20", ["action"], "the Rebalance button"),
  ...on("live", ["white"], "Badge live"),
  ...on("action", ["white"], "primary buttons (Save, Send)"),
  ...on("loss", ["white"], "Close / Delete confirm buttons"),
  ...on("slate-700", ["white"], "the menu's current page, selected tabs"),
  ...on("slate-600", ["white"], "selected chart range"),
  ...on("sky-900", ["white", "sky-50"], "the Ask button, survey choices"),
  ...on("testnet", ["slate-950"], "the notification count"),
];

// Today's pairs under AA (#763 baseline). Remove an entry when its fix lands;
// never add one to silence a new pair without the designer's say.
// The ratios and fixes: docs/product/responsive-a11y-baseline.md.
const KNOWN_FINDINGS = {
  dark: [
    "text-action on bg-action/20",
    "text-loss on bg-loss/10",
    "text-loss on bg-loss/15",
    "text-loss on bg-loss/20",
    "text-slate-500 on bg-slate-800",
    "text-white on bg-action",
    "text-white on bg-live",
    "text-white on bg-loss",
  ],
};

describe("lib/a11y: the rules", () => {
  test("contrast follows WCAG 2.x: black on white is 21:1, a colour on itself 1:1, order does not matter", () => {
    expect(contrastRatio("#000", "#fff")).toBeCloseTo(21, 5);
    expect(contrastRatio("#777777", "#777777")).toBeCloseTo(1, 5);
    expect(contrastRatio("#fff", "#767676")).toBeCloseTo(contrastRatio("#767676", "#fff"), 10);
    // #767676 on white is the classic 4.54:1, just over AA.
    expect(contrastRatio("#767676", "#fff")).toBeCloseTo(4.54, 2);
  });

  test("AA is 4.5:1 for body text and 3:1 for large text, at the boundary and one step past it", () => {
    expect([AA_BODY, AA_LARGE]).toEqual([4.5, 3]);
    expect(meetsAA(4.5)).toBe(true);
    expect(meetsAA(4.49)).toBe(false);
    expect(meetsAA(3, true)).toBe(true);
    expect(meetsAA(2.99, true)).toBe(false);
  });

  test("a tap target is at least 44 × 44 px", () => {
    expect(MIN_TAP_PX).toBe(44);
    expect(tapTargetOk(44, 44)).toBe(true);
    expect(tapTargetOk(43.9, 60)).toBe(false);
    expect(tapTargetOk(120, 20)).toBe(false);
  });

  test("colours are read as hex and oklch; an opacity is painted over the background; anything else throws", () => {
    expect(parseColor("#0b1120")).toEqual({ r: 11, g: 17, b: 32 });
    expect(parseColor("#fff")).toEqual({ r: 255, g: 255, b: 255 });
    const slate200 = parseColor("oklch(92.9% 0.013 255.508)"); // ≈ #e2e8f0
    expect([slate200.r, slate200.g, slate200.b].map(Math.round)).toEqual([226, 232, 240]);
    expect(mix("#ffffff", "#000000", 0.5)).toEqual({ r: 127.5, g: 127.5, b: 127.5 });
    expect(() => parseColor("rebeccapurple")).toThrow(/unreadable colour/);
    expect(() => measurePairs([{ text: "nope", bg: "panel", where: "x" }], THEMES.dark, TAILWIND_PALETTE)).toThrow(/unknown colour "nope"/);
  });
});

describe("Scenario: a token pair below AA is listed, not hidden", () => {
  const tokens = { panel: "#111a2e", dim: "#3b4252", bright: "#e2e8f0" };
  const measured = measurePairs(
    [
      { text: "dim", bg: "panel", where: "a dim label" },
      { text: "bright", bg: "panel", where: "body text" },
    ],
    tokens,
  );

  test("a pair under 4.5:1 is measured as failing, with its ratio", () => {
    expect(measured[0]).toMatchObject({ key: "text-dim on bg-panel", ok: false, needed: 4.5 });
    expect(measured[0].ratio).toBeLessThan(4.5);
    expect(measured[1].ok).toBe(true);
  });

  test("in the known-findings list it passes", () => {
    expect(compareContrast(measured, ["text-dim on bg-panel"])).toEqual([]);
  });

  test("large text is held to 3:1", () => {
    const [m] = measurePairs([{ text: "dim", bg: "panel", where: "x", large: true }], { panel: "#000000", dim: "#5a5a5a" });
    expect(m).toMatchObject({ needed: 3, ok: true });
  });
});

describe("Scenario: a new failing pair fails the test", () => {
  const measured = measurePairs([{ text: "dim", bg: "panel", where: "a dim label" }], { panel: "#111a2e", dim: "#3b4252" });

  test("a failing pair not in the known list fails, naming the pair, where it is and its ratio", () => {
    const [problem] = compareContrast(measured, []);
    expect(problem).toMatch(/^text-dim on bg-panel \(a dim label\): contrast [\d.]+:1, AA needs 4.5:1 — new finding$/);
  });

  test("a known finding that now meets AA, or is no longer checked, fails until it is removed", () => {
    const fixed = measurePairs([{ text: "bright", bg: "panel", where: "x" }], { panel: "#111a2e", bright: "#ffffff" });
    expect(compareContrast(fixed, ["text-bright on bg-panel", "text-gone on bg-panel"])).toEqual([
      expect.stringMatching(/^text-bright on bg-panel: now [\d.]+:1, meets AA — remove it/),
      "text-gone on bg-panel: a known finding that is no longer checked — remove it",
    ]);
  });
});

describe("Scenario: the baseline doc ranks the findings", () => {
  const doc = fs.readFileSync(path.join(ROOT, "..", "docs", "product", "responsive-a11y-baseline.md"), "utf8");
  const rows = doc
    .split("\n")
    .filter((l) => /^\| \d+ \|/.test(l))
    .map((l) => l.split("|").map((c) => c.trim()));
  const header = doc.split("\n").find((l) => l.startsWith("| # |"));

  test("the table has the pages affected and the fix owner, and every row names an owner", () => {
    expect(header).toMatch(/\| Pages affected \|.*\| Owner \|$/);
    for (const r of rows) expect([r[1], r.at(-2)]).toEqual([r[1], expect.stringMatching(/designer|developer/)]);
  });

  test("findings are listed by number of pages affected, most first", () => {
    const counts = rows.map((r) => Number(r[4].match(/^(\d+):/)?.[1]));
    expect(counts.every(Number.isFinite)).toBe(true);
    expect(counts).toEqual([...counts].sort((a, b) => b - a));
  });

  test("every pinned contrast finding is in the doc with its measured ratio", () => {
    const measured = measurePairs(PAIRS, THEMES.dark, TAILWIND_PALETTE);
    for (const key of KNOWN_FINDINGS.dark) {
      const row = rows.find((r) => r[2].includes(`\`${key}\``));
      expect([key, row?.[3]]).toEqual([key, `${measured.find((m) => m.key === key).ratio.toFixed(2)}:1`]);
    }
  });
});

describe("the monitor's own pairs", () => {
  test.each(Object.keys(THEMES))("%s theme: every pair meets AA or is a pinned known finding", (theme) => {
    const measured = measurePairs(PAIRS, THEMES[theme], TAILWIND_PALETTE);
    const problems = compareContrast(measured, KNOWN_FINDINGS[theme]);
    expect(problems).toEqual([]);
  });

  test("the themes checked are the ones the stylesheet has: dark only, no light variant yet", () => {
    // A light theme (prefers-color-scheme, a .light class or Tailwind's dark:
    // variant) needs its own tokens in THEMES and its row in KNOWN_FINDINGS.
    expect(CSS).not.toMatch(/prefers-color-scheme|\.light\b|@custom-variant\s+dark/);
    expect(Object.keys(THEMES)).toEqual(["dark"]);
    expect(THEMES.dark.body).toBe("#e2e8f0");
  });

  test("every text colour class in the pages is checked in at least one pair", () => {
    const known = new Set([...Object.keys(THEMES.dark), ...Object.keys(TAILWIND_PALETTE)]);
    const used = new Set();
    const walk = (dir) => {
      for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
        const p = path.join(dir, e.name);
        if (e.isDirectory()) walk(p);
        else if (/\.jsx?$/.test(e.name)) {
          for (const m of fs.readFileSync(p, "utf8").matchAll(/(?<![\w:-])text-([a-z]+(?:-\d{2,3})?)(?![\w-])/g)) if (known.has(m[1])) used.add(m[1]);
        }
      }
    };
    walk(path.join(ROOT, "app"));
    walk(path.join(ROOT, "components"));
    const covered = new Set(PAIRS.map((p) => p.text));
    expect([...used].filter((t) => !covered.has(t)).sort()).toEqual([]);
  });

  test("the copied Tailwind colours match tailwindcss/theme.css when it is installed", () => {
    // Read from disk: Jest maps a required .css file to a stub.
    const file = path.join(ROOT, "node_modules", "tailwindcss", "theme.css");
    if (!fs.existsSync(file)) return; // not installed here: CI's npm ci installs it
    const theme = fs.readFileSync(file, "utf8");
    for (const [name, value] of Object.entries(TAILWIND_PALETTE)) {
      const m = theme.match(new RegExp(`--color-${name}:\\s*([^;]+);`));
      expect([name, m?.[1].trim()]).toEqual([name, value]);
    }
  });
});
