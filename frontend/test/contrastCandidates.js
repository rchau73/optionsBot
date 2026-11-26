// The decision sheet of contrast candidates (#878, epic E12): for each pinned
// finding of the token test, the nearest colours that pass WCAG AA and what each
// does to the other pairs of the same token. Pure: it reads lib/a11y.js and the
// shared pairs, writes nothing (the test writes docs/product/responsive-a11y-contrast-candidates.md
// when UPDATE_CONTRAST_SHEET=1). It changes no colour of the monitor.
import { AA_BODY, AA_LARGE, hueGap, hueOf, measurePairs, nearestPassing, pairKey, pairRatio, toHex } from "@/lib/a11y";
import { KNOWN_FINDINGS, PAIRS, THEMES } from "@/test/contrastPairs";
import { TAILWIND_PALETTE } from "@/test/tailwindPalette";

export const SHEET_PATH = "docs/product/responsive-a11y-contrast-candidates.md";

/** The number of each pinned finding in docs/product/responsive-a11y-baseline.md. */
export const FINDING_NUMBERS = {
  "text-white on bg-live": 2,
  "text-loss on bg-loss/15": 3,
  "text-white on bg-loss": 6,
  "text-action on bg-action/20": 7,
  "text-white on bg-action": 9,
  "text-loss on bg-loss/10": 10,
  "text-loss on bg-loss/20": 11,
};

/** Findings WCAG exempts (a disabled control): noted in the sheet, no candidates. */
export const EXEMPT = { "text-slate-500 on bg-slate-800": 8 };

const MAX_CANDIDATES = 3;
const KIND_ORDER = { text: 0, background: 1, tint: 2 };
const MARGIN = 0.5; // the "with room to spare" candidate when fewer than two just pass
const base = (name) => String(name).split("/")[0];

/** The pinned findings of the dark theme that the sheet neither numbers nor exempts: each needs a decision. */
export function unnumbered() {
  return KNOWN_FINDINGS.dark.filter((k) => !(k in FINDING_NUMBERS) && !(k in EXEMPT));
}

function words(c) {
  if (c.kind === "tint") return `a ${c.direction} tint: \`${c.token}\` at ${c.value} % opacity instead of ${c.now} %`;
  const who = c.kind === "text" ? "text" : "background";
  return `${c.direction} \`${c.token}\` ${who}, hue kept`;
}

/** The up-to-three candidates of one pair: the nearest hit per kind and direction, nearest first by kind, with room to spare when fewer than two just pass. */
export function candidatesFor(pair, tokens = THEMES.dark, palette = TAILWIND_PALETTE) {
  const now = Number(String(pair.bg).split("/")[1]);
  const pick = (hits) => [...hits].sort((a, b) => KIND_ORDER[a.kind] - KIND_ORDER[b.kind] || a.distance - b.distance).map((h) => ({ ...h, now }));
  let list = pick(nearestPassing(pair, tokens, palette, 0));
  if (list.length < 2) list = pick([...list, ...nearestPassing(pair, tokens, palette, MARGIN)].filter((h, i, all) => all.findIndex((o) => o.kind === h.kind && o.direction === h.direction && o.ratio === h.ratio) === i));
  return list.slice(0, MAX_CANDIDATES);
}

// The tokens a candidate sets: a colour candidate replaces the token everywhere, a tint only this pair's bg.
function applied(pair, c, tokens) {
  if (c.kind === "tint") return { pair: { ...pair, bg: `${c.token}/${c.value}` }, tokens };
  return { pair, tokens: { ...tokens, [c.token]: c.value } };
}

/** The ratio of `pair` with candidate `c` applied (exact), recomputed from the colour written in the sheet. */
export function candidateRatio(pair, c, tokens = THEMES.dark, palette = TAILWIND_PALETTE) {
  const a = applied(pair, c, tokens);
  return pairRatio(a.pair, a.tokens, palette);
}

// The other pairs a candidate reaches: those using the token as text, background or under-layer; for a tint, the pairs with the same tinted background.
function touchedBy(c, pair) {
  const touches = (p) => (c.kind === "tint" ? p.bg === pair.bg : [base(p.text), base(p.bg), base(p.under ?? "panel")].includes(c.token));
  return PAIRS.filter(touches);
}

const need = (p) => (p.large ? AA_LARGE : AA_BODY);
const fmt = (r) => r.toFixed(2);
const mark = (r, p) => `${fmt(r)}${r >= need(p) ? " ✓" : " ✗"}`;

function findingSection(key, tokens, palette) {
  const pair = PAIRS.find((p) => pairKey(p) === key);
  const [measured] = measurePairs([pair], tokens, palette);
  const cands = candidatesFor(pair, tokens, palette);
  const letters = cands.map((_, i) => String.fromCharCode(65 + i));
  const out = [`### #${FINDING_NUMBERS[key]} \`${key}\``, ""];
  out.push(`Seen in: ${pair.where}. Now ${fmt(measured.ratio)}:1, AA needs ${measured.needed}:1.`, "");
  out.push("| Option | Change | Colour | Ratio | Distance from now | Hue moved |", "|---|---|---|---|---|---|");
  const hue0 = (c) => (c.kind === "tint" ? 0 : hueGap(c.hue, hueOf(tokens[c.token] ?? palette[c.token])));
  cands.forEach((c, i) => {
    const value = c.kind === "tint" ? `${c.value} % opacity (now ${c.now} %)` : `\`${c.value}\` (now \`${toHex((tokens[c.token] ?? palette[c.token]))}\`)`;
    const distance = c.kind === "tint" ? `${c.distance} points` : String(c.distance);
    out.push(`| ${letters[i]} | ${words(c)} | ${value} | ${fmt(candidateRatio(pair, c, tokens, palette))}:1 | ${distance} | ${Math.round(hue0(c))}° |`);
  });
  const touched = [...new Map(cands.flatMap((c) => touchedBy(c, pair)).map((p) => [pairKey(p), p])).values()];
  out.push("", "Other pairs the same change touches (ratio now → with each option; ✗ is under the bar):", "");
  out.push(`| Pair | Seen in | Now | ${letters.join(" | ")} |`, `|---|---|---|${letters.map(() => "---|").join("")}`);
  for (const p of touched) {
    const now = pairRatio(p, tokens, palette);
    const cells = cands.map((c) => {
      const t = touchedBy(c, pair).some((q) => pairKey(q) === pairKey(p));
      if (!t) return "same";
      const a = applied(p, c, tokens);
      return mark(pairRatio(a.pair, a.tokens, palette), p);
    });
    out.push(`| \`${pairKey(p)}\` | ${p.where} | ${mark(now, p)} | ${cells.join(" | ")} |`);
  }
  out.push("");
  return out.join("\n");
}

/** The whole sheet as markdown, from today's tokens and the pinned findings. */
export function buildSheet(tokens = THEMES.dark, palette = TAILWIND_PALETTE) {
  const numbered = Object.entries(FINDING_NUMBERS).filter(([k]) => KNOWN_FINDINGS.dark.includes(k)).sort((a, b) => a[1] - b[1]);
  const exempt = Object.entries(EXEMPT).filter(([k]) => KNOWN_FINDINGS.dark.includes(k));
  const lines = [
    "# Contrast candidates: the nearest passing colour for each failing pair (E12, #878)",
    "",
    "A decision sheet for the designer. For each failing pair of the accessibility baseline (`responsive-a11y-baseline.md`), two or three colours that pass WCAG AA, found by changing one thing and keeping the rest, and what each does to the other pairs of the same token.",
    "It changes no colour, token, class or component: the designer picks once the prototype (#654) is approved, and the fix is a later UI story.",
    "",
    "**Generated** by `frontend/__tests__/contrast-candidates.test.js` from `KNOWN_FINDINGS` (`frontend/test/contrastPairs.js`) and `app/globals.css`; do not edit by hand.",
    "When a finding is fixed or added the test fails and asks for `UPDATE_CONTRAST_SHEET=1 npm test -- contrast-candidates` from `frontend/`.",
    "",
    "## How to read it",
    "",
    `- **Bar:** ${AA_BODY}:1 for body text, ${AA_LARGE}:1 for large text.`,
    "- **Options:** *text* changes the text colour's token; *background* the background's token; *tint* the opacity of a tinted background. Lighter and darker mix the colour toward white or black in 1 % steps, so the hue stays; the first step that passes is listed (the nearest).",
    "- **A token is changed everywhere it is used.** When the text and its tint are the same token (`loss` on `loss/15`) both move, which is why a tint option can pass where a colour option does not. The second table shows each option's effect on every other pair of that token — a change to `loss` shows its effect on every loss figure.",
    "- **Distance** is the straight-line distance in sRGB (0–441); **hue moved** is the angle from today's hue (0° = the same colour family).",
    "- **Keep apart:** a loss candidate stays red and a gain stays green (a lighter or darker red never approaches `profit`); a filled-button red (#2, #6) that darkens `loss` or `live` moves them in lightness only, so a loss figure and a button stay told apart by size and shape as today — the designer decides whether a filled button gets a token of its own.",
    "",
    "## Findings",
    "",
  ];
  for (const [key] of numbered) lines.push(findingSection(key, tokens, palette));
  if (exempt.length) {
    lines.push("## Exempt", "");
    for (const [key, n] of exempt) lines.push(`- #${n} \`${key}\`: a disabled control, exempt from WCAG contrast; no candidates (see the baseline).`);
    lines.push("");
  }
  return `${lines.join("\n").trimEnd()}\n`;
}
