/**
 * @jest-environment node
 */
// The phone-width audit (#207) is a document, not code: these tests keep it
// complete (a row per panel at each width, the Close and Rebalance actions)
// and keep its contrast ratios true to the design tokens in app/globals.css.
import fs from "node:fs";
import path from "node:path";
import { outOfOrder } from "@/test/order";

const FRONTEND = path.join(__dirname, "..");
const read = (...p) => fs.readFileSync(path.join(FRONTEND, ...p), "utf8");
const AUDIT = read("docs", "responsive-audit.md");
const README = read("README.md");
const PAGES = read("docs", "menu-and-pages.md");

/** The rows of the first markdown table under the heading `## <title>`, as arrays of trimmed cells. */
function tableUnder(markdown, title) {
  const start = markdown.indexOf(`\n## ${title}\n`);
  if (start < 0) throw new Error(`no "## ${title}" section`);
  const section = markdown.slice(start + 1).split(/\n## /)[0];
  const lines = section.split("\n").filter((l) => l.startsWith("|"));
  return lines
    .slice(2) // header and separator
    .map((l) =>
      l
        .slice(1, -1)
        .split("|")
        .map((c) => c.trim()),
    );
}

const plain = (cell) => cell.replace(/[`*]/g, "").trim();

// --- colours -----------------------------------------------------------------

/** "#rrggbb" → [r, g, b] in 0–1 (sRGB, gamma-encoded). */
const fromHex = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);

/** CSS oklch(L% C H) → sRGB 0–1, clipped to the gamut (as a browser shows it). */
function fromOklch(l, c, h) {
  const L = l / 100;
  const a = c * Math.cos((h * Math.PI) / 180);
  const b = c * Math.sin((h * Math.PI) / 180);
  const lms = [L + 0.3963377774 * a + 0.2158037573 * b, L - 0.1055613458 * a - 0.0638541728 * b, L - 0.0894841775 * a - 1.291485548 * b].map(
    (v) => v ** 3,
  );
  const linear = [
    4.0767416621 * lms[0] - 3.3077115913 * lms[1] + 0.2309699292 * lms[2],
    -1.2684380046 * lms[0] + 2.6097574011 * lms[1] - 0.3413193965 * lms[2],
    -0.0041960863 * lms[0] - 0.7034186147 * lms[1] + 1.707614701 * lms[2],
  ];
  return linear.map((v) => {
    const x = Math.min(Math.max(v, 0), 1);
    return x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055;
  });
}

/** Design tokens of app/globals.css, Tailwind's slate scale and white, by name. */
function palette() {
  const colours = { white: [1, 1, 1] };
  for (const [, name, hex] of read("app", "globals.css").matchAll(/--color-([a-z]+):\s*(#[0-9a-f]{6})/g)) colours[name] = fromHex(hex);
  const theme = read("node_modules", "tailwindcss", "theme.css");
  for (const [, name, l, c, h] of theme.matchAll(/--color-(slate-\d+):\s*oklch\(([\d.]+)% ([\d.]+) ([\d.]+)\)/g)) {
    colours[name] = fromOklch(Number(l), Number(c), Number(h));
  }
  return colours;
}

const COLOURS = palette();
const TOKENS = [...read("app", "globals.css").matchAll(/--color-([a-z]+):/g)].map((m) => m[1]);

/**
 * A colour expression of the audit: a name (`profit`, `slate-800`, `white`),
 * a hex (`#e2e8f0`), or `<colour>/<alpha %> over <expression>` — Tailwind's
 * opacity modifier, painted over what is behind it.
 */
function resolve(expr) {
  const text = plain(expr);
  const over = text.match(/^(\S+)\/(\d+) over (.+)$/);
  if (over) {
    const top = resolve(over[1]);
    const below = resolve(over[3]);
    const alpha = Number(over[2]) / 100;
    return top.map((v, i) => alpha * v + (1 - alpha) * below[i]);
  }
  if (/^#[0-9a-f]{6}$/i.test(text)) return fromHex(text.toLowerCase());
  if (!COLOURS[text]) throw new Error(`unknown colour "${text}"`);
  return COLOURS[text];
}

/** WCAG 2.1 relative luminance and contrast ratio. */
const luminance = (rgb) => {
  const [r, g, b] = rgb.map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (fg, bg) => {
  const [a, b] = [luminance(fg), luminance(bg)].sort((x, y) => y - x);
  return (a + 0.05) / (b + 0.05);
};

const ratioOf = (cell) => Number(plain(cell).replace(/:1$/, ""));
const verdict = (ratio, min) => (ratio >= min ? "pass" : "fail");

/** Every row whose ratio or pass/fail differs from the computed one, as readable lines. */
function wrongRows(rows, columns) {
  const wrong = [];
  for (const row of rows) {
    const [fg, bg] = row;
    const ratio = Math.round(contrast(resolve(fg), resolve(bg)) * 100) / 100;
    const got = [ratioOf(row[3]), ...columns.map(([col]) => plain(row[col]))];
    const want = [ratio, ...columns.map(([, min]) => verdict(ratio, min))];
    if (Math.abs(got[0] - want[0]) > 0.01 || got.slice(1).join() !== want.slice(1).join()) {
      wrong.push(`${plain(fg)} on ${plain(bg)}: audit says ${got.join(" ")}, computed ${want.join(" ")}`);
    }
  }
  return wrong;
}

// --- the scenarios -----------------------------------------------------------

/** The areas of docs/menu-and-pages.md "What you see" (one `###` heading each), by their name before any "(…)". */
function pageAreas() {
  const start = PAGES.indexOf("\n## What you see\n");
  if (start < 0) throw new Error('no "## What you see" section');
  const section = PAGES.slice(start + 1).split(/\n## /)[0];
  const areas = [...section.matchAll(/^### (.+)$/gm)].map((m) => m[1].replace(/\s*\(.*$/, ""));
  // "Income page" is a page section of the same level, not an area row.
  return areas.filter((a) => a !== "Income page" && a !== "Every page");
}

describe("responsive audit (#207)", () => {
  const panels = tableUnder(AUDIT, "Panels at 360, 390 and 768 px");

  // Scenario: every panel is covered
  test("every panel is covered: a row for each README area and both dialogs at 360, 390 and 768 px", () => {
    const areas = [...pageAreas(), "Close dialog", "Rebalance dialog"];
    expect(areas).toEqual(expect.arrayContaining(["Header", "Open positions", "Working orders", "P&L chart"]));
    const missing = areas.filter((area) => {
      const row = panels.find((r) => plain(r[0]).toLowerCase().startsWith(area.toLowerCase()));
      return !row || [2, 3, 4].some((i) => !row[i] || row[i] === "—");
    });
    expect(missing).toEqual([]);
  });

  // #465: rows in alphabetical order of the panel's name (case aside), so two
  // PRs that each add a panel do not collide at the end of the table.
  test("the panels are in alphabetical order", () => {
    const name = (row) => plain(row[0]).toLowerCase();
    expect(panels.length).toBeGreaterThan(20);
    expect(outOfOrder(panels.map(name))).toBeNull();
    expect(outOfOrder([...panels.map(name), "account page (again)"])).toMatch(/^"account page \(again\)" comes after/);
  });

  test("every panel is covered: each component file is named in the audit", () => {
    const files = fs.readdirSync(path.join(FRONTEND, "components")).filter((f) => f.endsWith(".jsx"));
    expect(files.filter((f) => !AUDIT.includes(`components/${f}`))).toEqual([]);
  });

  // Scenario: contrast is measured
  test("contrast is measured: every text pair has its true ratio and AA verdicts", () => {
    const rows = tableUnder(AUDIT, "Contrast of the design tokens");
    expect(rows.length).toBeGreaterThan(20);
    expect(wrongRows(rows, [[4, 4.5], [5, 3]])).toEqual([]);
  });

  test("contrast is measured: every non-text pair has its true ratio and 3:1 verdict", () => {
    const rows = tableUnder(AUDIT, "Contrast of non-text marks");
    expect(rows.length).toBeGreaterThan(5);
    expect(wrongRows(rows, [[4, 3]])).toEqual([]);
  });

  test("contrast is measured: every design token appears in a measured pair", () => {
    const cells = [...tableUnder(AUDIT, "Contrast of the design tokens"), ...tableUnder(AUDIT, "Contrast of non-text marks")]
      .flatMap((r) => [plain(r[0]), plain(r[1])])
      .join(" ");
    expect(TOKENS.length).toBeGreaterThanOrEqual(12);
    expect(TOKENS.filter((t) => !new RegExp(`(^|[\\s])${t}(/|\\s|$)`).test(cells))).toEqual([]);
  });

  test("the contrast formula matches WCAG's reference values", () => {
    expect(contrast([0, 0, 0], [1, 1, 1])).toBeCloseTo(21, 5);
    expect(contrast(fromHex("#767676"), [1, 1, 1])).toBeCloseTo(4.54, 2);
    // Tailwind's slate-800 is oklch(27.9% 0.041 260.031) ≈ #1d293d.
    expect(COLOURS["slate-800"].map((v) => Math.round(v * 255))).toEqual([29, 41, 61]);
  });

  // Scenario: actions are checked
  test("actions are checked: Close and Rebalance are each judged reachable and usable at 360 px", () => {
    const rows = tableUnder(AUDIT, "Close and Rebalance at 360 px");
    for (const action of ["Close button", "Close dialog", "Rebalance button", "Rebalance dialog"]) {
      const row = rows.find((r) => plain(r[0]) === action);
      expect([action, Boolean(row)]).toEqual([action, true]);
      expect([action, /^(yes|no|partly)\b/.test(plain(row[1])), /^(yes|no|partly)\b/.test(plain(row[2]))]).toEqual([action, true, true]);
    }
  });

  test("the audit is linked from frontend/README.md", () => {
    expect(README).toContain("(docs/responsive-audit.md)");
  });
});
