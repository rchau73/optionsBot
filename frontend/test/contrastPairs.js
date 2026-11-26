// The pairs of the colour-contrast test (#763) and today's known findings, shared
// by the token test and the decision sheet of candidate colours (#878,
// test/contrastCandidates.js). The rules for changing them are the token test's.
import fs from "node:fs";
import path from "node:path";
import { themeTokens } from "@/lib/a11y";

const CSS = fs.readFileSync(path.join(__dirname, "..", "app", "globals.css"), "utf8");

// The monitor has one theme, dark (#0b1120 surface, no light variant): its
// tokens, plus "body", the page's default text colour (html, body { color }).
function darkTokens(css) {
  const body = css.match(/html,\s*body\s*\{[^}]*\bcolor:\s*([^;]+);/);
  return { ...themeTokens(css), body: body ? body[1].trim() : undefined };
}
export const THEMES = { dark: darkTokens(CSS) };

// The pairs the pages use: text colour (the class after text-), background
// (after bg-; one with an opacity is painted over `under`, else the panel),
// and one place it is seen. A new text colour in a component needs its pair
// here (the last test checks every text-* colour class is covered).
const on = (bg, texts, where, extra = {}) => texts.map((text) => ({ text, bg, where, ...extra }));
const ACCENTS = ["profit", "loss", "warn", "testnet", "call", "put", "action"];
export const PAIRS = [
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
export const KNOWN_FINDINGS = {
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
