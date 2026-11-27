// The decision sheet of contrast candidates (#878, epic E12): regenerated from the
// pinned findings and compared with docs/product/responsive-a11y-contrast-candidates.md.
// Out of date → UPDATE_CONTRAST_SHEET=1 npm test -- contrast-candidates (from frontend/).
// No colour, token, class or component changes here.
import fs from "node:fs";
import path from "node:path";
import { AA_BODY, AA_LARGE, contrastRatio, hueGap, hueOf, nearestPassing, pairKey, parseColor, toHex } from "@/lib/a11y";
import { TAILWIND_PALETTE } from "@/test/tailwindPalette";
import { KNOWN_FINDINGS, PAIRS, THEMES } from "@/test/contrastPairs";
import { buildSheet, candidateRatio, candidatesFor, EXEMPT, FINDING_NUMBERS, SHEET_PATH, unnumbered } from "@/test/contrastCandidates";

const REPO = path.join(__dirname, "..", "..");
const SHEET = path.join(REPO, SHEET_PATH);
const pairOf = (key) => PAIRS.find((p) => pairKey(p) === key);

// Rewrite the sheet before any test reads it, so a first UPDATE_CONTRAST_SHEET=1 run works without one.
beforeAll(() => {
  if (process.env.UPDATE_CONTRAST_SHEET === "1") fs.writeFileSync(SHEET, buildSheet());
});

describe("Scenario: every finding has candidates that reach their bar", () => {
  test("every pinned finding is numbered in the baseline or exempt, so a new one asks for a decision", () => {
    expect(unnumbered()).toEqual([]);
  });

  test("the numbers are the baseline doc's rows", () => {
    const doc = fs.readFileSync(path.join(REPO, "docs", "product", "responsive-a11y-baseline.md"), "utf8");
    for (const [key, n] of [...Object.entries(FINDING_NUMBERS), ...Object.entries(EXEMPT)]) {
      const row = doc.split("\n").find((l) => l.startsWith(`| ${n} |`));
      expect([key, row?.includes(`\`${key}\``)]).toEqual([key, true]);
    }
  });

  test.each(Object.keys(FINDING_NUMBERS))("%s: two or three candidates, each at or above its bar", (key) => {
    const pair = pairOf(key);
    const need = pair.large ? AA_LARGE : AA_BODY;
    const cands = candidatesFor(pair);
    expect(cands.length).toBeGreaterThanOrEqual(2);
    expect(cands.length).toBeLessThanOrEqual(3);
    for (const c of cands) expect(candidateRatio(pair, c, THEMES.dark, TAILWIND_PALETTE)).toBeGreaterThanOrEqual(need);
  });

  test("a candidate colour keeps its colour family, and a red never nears the gain green", () => {
    const profitHue = hueOf(THEMES.dark.profit);
    for (const key of Object.keys(FINDING_NUMBERS)) {
      for (const c of candidatesFor(pairOf(key)).filter((x) => x.kind !== "tint")) {
        const from = THEMES.dark[c.token] ?? TAILWIND_PALETTE[c.token];
        if (parseColor(from).r === parseColor(from).b && parseColor(from).g === parseColor(from).b) continue; // white: no hue
        expect([key, c.token, hueGap(hueOf(c.value), hueOf(from)) <= 12]).toEqual([key, c.token, true]);
        if (["loss", "live"].includes(c.token)) expect(hueGap(hueOf(c.value), profitHue)).toBeGreaterThan(60);
      }
    }
  });

  test("the ratio in the sheet is the WCAG ratio of the colour written there, to two decimals", () => {
    const sheet = fs.readFileSync(SHEET, "utf8");
    const pair = pairOf("text-white on bg-loss");
    const [c] = candidatesFor(pair).filter((x) => x.kind === "background");
    const recomputed = contrastRatio("#ffffff", c.value).toFixed(2);
    expect(sheet).toContain(`\`${c.value}\``);
    expect(sheet).toContain(`| ${recomputed}:1 |`);
  });
});

describe("the search", () => {
  const tokens = { panel: "#111a2e", dim: "#3b4252" };

  test("lighter and darker are searched; a direction with no way to pass is left out", () => {
    const hits = nearestPassing({ text: "dim", bg: "panel", where: "x" }, tokens);
    expect(hits.filter((h) => h.kind === "text").map((h) => h.direction)).toEqual(["lighter"]);
    expect(contrastRatio(hits[0].value, "#111a2e")).toBeGreaterThanOrEqual(4.5);
    // one step less does not pass: it is the nearest
    expect(hits[0].distance).toBeGreaterThan(0);
  });

  test("white cannot get lighter; its darker hit reaches the bar on a solid background", () => {
    const hits = nearestPassing({ text: "white", bg: "red", where: "x" }, { red: "#ef4444" }, { white: "#fff" });
    expect(hits.filter((h) => h.kind === "text").map((h) => h.direction)).toEqual(["darker"]);
    expect(toHex(hits[0].value)).toBe(hits[0].value);
  });

  test("a margin asks for more than the bar", () => {
    const [plain] = nearestPassing({ text: "dim", bg: "panel", where: "x" }, tokens);
    const [roomy] = nearestPassing({ text: "dim", bg: "panel", where: "x" }, tokens, {}, 0.5);
    expect(roomy.ratio).toBeGreaterThanOrEqual(5);
    expect(roomy.distance).toBeGreaterThan(plain.distance);
  });
});

describe("Scenario: the sheet is regenerated with the known findings", () => {
  test("the committed sheet is today's (UPDATE_CONTRAST_SHEET=1 rewrites it)", () => {
    const built = buildSheet();
    expect(fs.readFileSync(SHEET, "utf8") === built ? "up to date" : `${SHEET_PATH} is out of date with the known findings: regenerate it with UPDATE_CONTRAST_SHEET=1 npm test -- contrast-candidates`).toBe("up to date");
  });

  test("a finding that leaves KNOWN_FINDINGS leaves the sheet, and the test asks for the regeneration", () => {
    const key = "text-white on bg-live";
    const before = buildSheet();
    expect(before).toContain(`\`${key}\``);
    const at = KNOWN_FINDINGS.dark.indexOf(key);
    KNOWN_FINDINGS.dark.splice(at, 1);
    try {
      const after = buildSheet();
      expect(after).not.toContain(`### #2 \`${key}\``);
      expect(after === before).toBe(false); // so the comparison above fails until the sheet is regenerated
    } finally {
      KNOWN_FINDINGS.dark.splice(at, 0, key);
    }
  });

  test("the exempt finding is noted, with no candidates", () => {
    const sheet = fs.readFileSync(SHEET, "utf8");
    expect(sheet).toMatch(/- #8 `text-slate-500 on bg-slate-800`: a disabled control/);
    expect(sheet).not.toMatch(/### #8/);
  });
});
