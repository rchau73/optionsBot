// The accessibility bar of the monitor (#763): colour contrast (WCAG 2.x),
// the smallest text and the smallest tap target. Pure: no DOM, no files, so
// the token test (__tests__/a11y-tokens.test.js) and the phone report
// (e2e/phone.js) share one definition of each rule.

/** WCAG 2.x AA: 4.5:1 for body text, 3:1 for large text (≥ 24 px, or ≥ 18.66 px bold). */
export const AA_BODY = 4.5;
export const AA_LARGE = 3;

/** The smallest text the monitor should show, in CSS px (the story's assumption, docs/product/responsive-a11y-baseline.md). */
export const MIN_TEXT_PX = 12;

/** The smallest tap target, in CSS px each way (WCAG 2.5.5, Apple's and Google's guidance). */
export const MIN_TAP_PX = 44;

const clamp01 = (x) => Math.min(1, Math.max(0, x));
const encode = (c) => (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055);
const decode = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);

// oklch(L% C H) → sRGB 0–255 (Björn Ottosson's OKLab matrices), clamped to the gamut.
function oklchToRgb(l, c, hDeg) {
  const h = (hDeg * Math.PI) / 180;
  const a = c * Math.cos(h);
  const b = c * Math.sin(h);
  const l3 = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m3 = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s3 = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin = [
    4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3,
    -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3,
    -0.0041960863 * l3 - 0.7034186147 * m3 + 1.707614701 * s3,
  ];
  const [r, g, bl] = lin.map((x) => encode(clamp01(x)) * 255);
  return { r, g, b: bl };
}

/**
 * A CSS colour → { r, g, b } (0–255). Reads what the monitor's tokens use:
 * #rgb, #rrggbb and oklch(L% C H) (Tailwind v4's palette). Anything else throws,
 * so a token in a new format is noticed rather than checked as black.
 */
export function parseColor(css) {
  if (typeof css === "object" && css) return css;
  const s = String(css).trim().toLowerCase();
  let m = s.match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/);
  if (m) {
    const hex = m[1].length === 3 ? [...m[1]].map((ch) => ch + ch).join("") : m[1];
    return { r: parseInt(hex.slice(0, 2), 16), g: parseInt(hex.slice(2, 4), 16), b: parseInt(hex.slice(4, 6), 16) };
  }
  m = s.match(/^oklch\(\s*([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)\s*\)$/);
  if (m) return oklchToRgb(m[2] ? Number(m[1]) / 100 : Number(m[1]), Number(m[3]), Number(m[4]));
  throw new Error(`a11y: unreadable colour "${css}"`);
}

/** `fg` at `alpha` painted over `bg` (what bg-loss/15 over a panel shows). */
export function mix(fg, bg, alpha) {
  const f = parseColor(fg);
  const b = parseColor(bg);
  return { r: f.r * alpha + b.r * (1 - alpha), g: f.g * alpha + b.g * (1 - alpha), b: f.b * alpha + b.b * (1 - alpha) };
}

/** WCAG 2.x relative luminance, 0 (black) to 1 (white). */
export function relativeLuminance(color) {
  const { r, g, b } = parseColor(color);
  return 0.2126 * decode(r / 255) + 0.7152 * decode(g / 255) + 0.0722 * decode(b / 255);
}

/** WCAG 2.x contrast ratio of two colours, 1 to 21; the order does not matter. */
export function contrastRatio(fg, bg) {
  const [hi, lo] = [relativeLuminance(fg), relativeLuminance(bg)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/** Whether a contrast ratio meets AA: 4.5:1 for body text, 3:1 for large text. */
export function meetsAA(ratio, largeText = false) {
  return ratio >= (largeText ? AA_LARGE : AA_BODY);
}

/** Whether a box is a big enough tap target: at least MIN_TAP_PX wide and tall. */
export function tapTargetOk(width, height) {
  return width >= MIN_TAP_PX && height >= MIN_TAP_PX;
}

/** The `--color-<name>: <value>;` tokens of a stylesheet's @theme block, by name. */
export function themeTokens(css) {
  const block = String(css).match(/@theme\s*\{([^}]*)\}/);
  if (!block) return {};
  return Object.fromEntries([...block[1].matchAll(/--color-([\w-]+)\s*:\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]));
}

/**
 * "loss/15" → { color, alpha }: a token of the theme, a colour of `palette`
 * (Tailwind's slate-700, white…) and an optional /opacity. Throws on a name
 * neither knows, so a pair cannot be checked against a colour that is not there.
 */
export function resolveColor(name, tokens, palette = {}) {
  const [base, pct] = String(name).split("/");
  const color = tokens[base] ?? palette[base];
  if (!color) throw new Error(`a11y: unknown colour "${name}"`);
  return { color, alpha: pct === undefined ? 1 : Number(pct) / 100 };
}

/** One pair's key, as the known-findings list names it: "text-muted on bg-slate-800". */
export function pairKey(pair) {
  return `text-${pair.text} on bg-${pair.bg}${pair.under ? ` over bg-${pair.under}` : ""}`;
}

/** One pair's exact contrast ratio (not rounded); the background is painted over `under` (or the panel) when it has an opacity. */
export function pairRatio(pair, tokens, palette = {}) {
  const fg = resolveColor(pair.text, tokens, palette);
  const bg = resolveColor(pair.bg, tokens, palette);
  // only a tinted background needs the colour under it (a solid one hides it)
  const background = bg.alpha < 1 ? mix(bg.color, resolveColor(pair.under ?? "panel", tokens, palette).color, bg.alpha) : bg.color;
  const text = fg.alpha < 1 ? mix(fg.color, background, fg.alpha) : fg.color;
  return contrastRatio(text, background);
}

/**
 * Each pair { text, bg, under?, large?, where } measured: the background is
 * painted over `under` (or the panel) when it has an opacity. Returns
 * { key, where, ratio, needed, ok } per pair, ratio rounded to 2 decimals.
 */
export function measurePairs(pairs, tokens, palette = {}) {
  return pairs.map((pair) => {
    const ratio = Math.round(pairRatio(pair, tokens, palette) * 100) / 100;
    const needed = pair.large ? AA_LARGE : AA_BODY;
    return { key: pairKey(pair), where: pair.where, ratio, needed, ok: meetsAA(ratio, pair.large) };
  });
}

/** A colour as #rrggbb (channels rounded), the form a candidate is written in. */
export function toHex(color) {
  const { r, g, b } = parseColor(color);
  return `#${[r, g, b].map((x) => Math.round(x).toString(16).padStart(2, "0")).join("")}`;
}

/** The straight-line distance of two colours in sRGB (0–441), the sheet's "how far from today". */
export function colorDistance(a, b) {
  const x = parseColor(a);
  const y = parseColor(b);
  return Math.round(Math.hypot(x.r - y.r, x.g - y.g, x.b - y.b));
}

/** The hue of a colour in degrees (0–360), for checking that a candidate keeps its colour family. */
export function hueOf(color) {
  const { r, g, b } = parseColor(color);
  const [mx, mn] = [Math.max(r, g, b), Math.min(r, g, b)];
  if (mx === mn) return 0;
  const d = mx - mn;
  const h = mx === r ? ((g - b) / d) % 6 : mx === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return (h * 60 + 360) % 360;
}

/** The smaller angle between two hues, 0–180. */
export const hueGap = (a, b) => Math.min(Math.abs(a - b) % 360, 360 - (Math.abs(a - b) % 360));

/**
 * The closest ways to make `pair` pass, each found by changing ONE thing and
 * keeping the rest (#878; the designer's decision sheet, nothing here changes a token):
 *   - "text": the text colour's token, lightened (toward white) or darkened (toward black), 1 % steps;
 *   - "background": the background colour's token, the same, when the pair has a solid background token;
 *   - "tint": the opacity of a tinted background (bg-loss/15 → /8), weaker or stronger, 1-point steps.
 * Each hit is the first step that reaches `margin` extra ratio over the bar (0 = just passes), as
 * { kind, token, direction, value, ratio, distance, hue } with `value` a #rrggbb (or an opacity in
 * percent for "tint") and `ratio` the exact ratio of the pair with it. A colour changed here is
 * the token's colour everywhere, so a token that is both the text and the tint moves both.
 * `direction`s with no hit (white cannot get lighter) are left out. Pure.
 */
export function nearestPassing(pair, tokens, palette = {}, margin = 0) {
  const need = (pair.large ? AA_LARGE : AA_BODY) + margin;
  const out = [];
  const base = (name) => String(name).split("/")[0];
  const colorOf = (name) => tokens[base(name)] ?? palette[base(name)];
  const colourSteps = (kind, name) => {
    const from = parseColor(colorOf(name));
    for (const [direction, to] of [["lighter", 255], ["darker", 0]]) {
      for (let pct = 1; pct <= 100; pct++) {
        const value = toHex({ r: from.r + (to - from.r) * (pct / 100), g: from.g + (to - from.g) * (pct / 100), b: from.b + (to - from.b) * (pct / 100) });
        const ratio = pairRatio(pair, { ...tokens, [base(name)]: value }, palette);
        if (ratio >= need) {
          out.push({ kind, token: base(name), direction, value, ratio, distance: colorDistance(value, toHex(from)), hue: hueOf(value) });
          break;
        }
      }
    }
  };
  colourSteps("text", pair.text);
  if (base(pair.bg) !== base(pair.text) && colorOf(pair.bg)) colourSteps("background", pair.bg);
  const [bgName, pctText] = String(pair.bg).split("/");
  if (pctText !== undefined) {
    const now = Number(pctText);
    for (const [direction, sign] of [["weaker", -1], ["stronger", 1]]) {
      for (let step = 1; step <= 100; step++) {
        const pct = now + sign * step;
        if (pct < 0 || pct > 100) break;
        const ratio = pairRatio({ ...pair, bg: `${bgName}/${pct}` }, tokens, palette);
        if (ratio >= need) {
          out.push({ kind: "tint", token: bgName, direction, value: pct, ratio, distance: step, hue: hueOf(colorOf(bgName)) });
          break;
        }
      }
    }
  }
  return out;
}

/**
 * What the token test fails on, one sentence each: a pair under AA that is
 * not in `known`, and a known finding that now passes or is no longer checked
 * (so the list shrinks when a fix lands). [] when the pinned list is exact.
 */
export function compareContrast(measured, known) {
  const problems = [];
  const knownSet = new Set(known);
  for (const m of measured) {
    if (!m.ok && !knownSet.has(m.key)) problems.push(`${m.key} (${m.where}): contrast ${m.ratio}:1, AA needs ${m.needed}:1 — new finding`);
    if (m.ok && knownSet.has(m.key)) problems.push(`${m.key}: now ${m.ratio}:1, meets AA — remove it from the known findings`);
  }
  const checked = new Set(measured.map((m) => m.key));
  for (const k of known) if (!checked.has(k)) problems.push(`${k}: a known finding that is no longer checked — remove it`);
  return problems;
}
