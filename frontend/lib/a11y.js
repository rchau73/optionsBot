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

/**
 * Each pair { text, bg, under?, large?, where } measured: the background is
 * painted over `under` (or the panel) when it has an opacity. Returns
 * { key, where, ratio, needed, ok } per pair, ratio rounded to 2 decimals.
 */
export function measurePairs(pairs, tokens, palette = {}) {
  return pairs.map((pair) => {
    const fg = resolveColor(pair.text, tokens, palette);
    const bg = resolveColor(pair.bg, tokens, palette);
    const under = resolveColor(pair.under ?? "panel", tokens, palette);
    const background = bg.alpha < 1 ? mix(bg.color, under.color, bg.alpha) : bg.color;
    const text = fg.alpha < 1 ? mix(fg.color, background, fg.alpha) : fg.color;
    const ratio = Math.round(contrastRatio(text, background) * 100) / 100;
    const needed = pair.large ? AA_LARGE : AA_BODY;
    return { key: pairKey(pair), where: pair.where, ratio, needed, ok: meetsAA(ratio, pair.large) };
  });
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
