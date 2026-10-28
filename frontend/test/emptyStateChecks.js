// Checks of the empty-state catalogue (#298), written as functions that
// return the problems found, so the tests can run them on the real catalogue
// and on broken ones (and show that a problem is named).
import fs from "node:fs";
import path from "node:path";
import { MAX_TEXT, STATES } from "@/lib/emptyStates";
import { markdownAnchors } from "@/test/glossarySources";

const FRONTEND = path.join(__dirname, "..");

export const SOURCE_FILES = ["OptionStrategy.md", "frontend/README.md"];

/** Forecast and advice words an empty state must never use. */
export const DENY = [
  "will earn",
  "will profit",
  "will make money",
  "will rise",
  "will fall",
  "will go up",
  "will go down",
  "will recover",
  "guarantee",
  "risk-free",
  "risk free",
  "no risk",
  "can't lose",
  "cannot lose",
  "safe income",
  "sure profit",
  "expect profit",
  "should buy",
  "should sell",
  "should hold",
  "should close",
  "you should",
  "we recommend",
  "recommend",
  "predict",
  "forecast",
];

/** The monitor's pages: the overview and Details (pro) (#405). */
export const MONITOR_SOURCES = ["Monitor.jsx", "DetailsPage.jsx"];
const readSources = () => MONITOR_SOURCES.map((f) => fs.readFileSync(path.join(FRONTEND, "components", f), "utf8")).join("\n");

/** What the monitor renders, read from components/Monitor.jsx and components/DetailsPage.jsx. */
export function monitorPanels(src = readSources()) {
  const storageKeys = [...src.matchAll(/storageKey="([^"]+)"/g)].map((m) => m[1]);
  const titles = [...src.matchAll(/<(?:Panel|OneLine)\b[^>]*?\stitle="([^"]+)"/g)].map((m) => m[1].replace(/&amp;/g, "&"));
  // Blocks rendered outside a Panel, straight in <main> or the header.
  const blocks = ["<Header", "<BotDashboards", "<CountsLine"].filter((tag) => src.includes(`${tag} `));
  return { storageKeys, titles, blocks };
}

/** "panel: <what is missing>" for every panel the monitor renders that lacks a key or a state. */
export function missingStates(catalogue, panels, rendered = monitorPanels()) {
  const out = [];
  const shown = new Set(Object.values(panels));
  for (const key of rendered.storageKeys) if (!(key in panels)) out.push(`${key}: not in PANELS`);
  for (const t of [...rendered.titles, ...rendered.blocks]) if (!shown.has(t)) out.push(`${t}: no panel key in PANELS`);
  for (const key of Object.keys(panels)) {
    const entry = catalogue[key];
    if (!entry) {
      out.push(`${key}: no entry`);
      continue;
    }
    for (const state of STATES) {
      if (typeof entry[state] !== "string" || !entry[state].trim()) out.push(`${key}: missing ${state}`);
    }
  }
  for (const key of Object.keys(catalogue)) if (!(key in panels)) out.push(`${key}: not a panel the monitor uses`);
  return out;
}

/** "panel.state: <n> chars" for every text over MAX_TEXT, and unknown fields. */
export function tooLong(catalogue) {
  const out = [];
  for (const [key, entry] of Object.entries(catalogue)) {
    for (const state of STATES) {
      const n = entry[state]?.length ?? 0;
      if (n > MAX_TEXT) out.push(`${key}.${state}: ${n} chars`);
    }
    const extra = Object.keys(entry).filter((f) => ![...STATES, "source"].includes(f));
    if (extra.length) out.push(`${key}: unknown fields ${extra.join(", ")}`);
  }
  return out;
}

/** "panel.state: <word>" for every forecast or advice word. */
export function promiseWords(catalogue, deny = DENY) {
  const out = [];
  for (const [key, entry] of Object.entries(catalogue)) {
    for (const state of STATES) {
      const text = String(entry[state] ?? "").toLowerCase();
      for (const word of deny) if (text.includes(word)) out.push(`${key}.${state}: ${word}`);
    }
  }
  return out;
}

/** "panel: <source>" for every source that is not an existing heading of an allowed document. */
export function brokenSources(catalogue, anchorsOf = markdownAnchors) {
  const out = [];
  const cache = {};
  for (const [key, entry] of Object.entries(catalogue)) {
    const [file, anchor] = String(entry.source ?? "").split("#");
    if (!SOURCE_FILES.includes(file) || !anchor) {
      out.push(`${key}: ${entry.source}`);
      continue;
    }
    cache[file] ??= anchorsOf(file);
    if (!cache[file].has(anchor)) out.push(`${key}: ${entry.source}`);
  }
  return out;
}
