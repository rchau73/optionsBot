// Reads what the glossary must cover (#188) straight from the sources, so a
// new monitor label or config key without an entry fails a test:
//   - monitorLabels(): every literal column header (<th>), definition term
//     (<dt>) and label="…" prop in components/, plus the label maps in
//     lib/held.js and lib/monitor.js (hold rules, feed events, exit reasons);
//   - configKeys(): every key the shipped config files set, nested keys as
//     "parent.child" (backtest.fill_model, iv_margin_bands.max_im_pct).
import fs from "node:fs";
import path from "node:path";

const FRONTEND = path.join(__dirname, "..");
const REPO = path.join(FRONTEND, "..");

export const CONFIG_FILES = ["config_btc.yaml", "config_eth.yaml", "config_btc.mvp.yaml"];

function clean(text) {
  return text
    .replace(/&amp;/g, "&")
    .replace(/\s+/g, " ")
    .trim();
}

/** Literal labels in one JSX source; text with {expressions} is skipped. */
export function jsxLabels(src) {
  const out = [];
  for (const m of src.matchAll(/<(th|dt)\b[^>]*>([^<{]*)<\/\1>/g)) out.push(clean(m[2]));
  // label="…" props (StatTile, MarginBar, Select), not aria-label="…".
  for (const m of src.matchAll(/(?<![\w-])label="([^"]+)"/g)) out.push(clean(m[1]));
  return out.filter(Boolean);
}

/** Labels of the `key: { label: "…" }` maps and of CLOSE_LABELS in a lib file. */
export function mapLabels(src) {
  const out = [...src.matchAll(/\blabel: "([^"]+)"/g)].map((m) => m[1]);
  const close = src.match(/const CLOSE_LABELS = \{([^}]*)\}/);
  if (close) out.push(...[...close[1].matchAll(/:\s*"([^"]+)"/g)].map((m) => m[1]));
  return out;
}

/** Every label the monitor shows, with the file it comes from. */
export function monitorLabels() {
  const found = new Map();
  const add = (label, file) => {
    if (!found.has(label)) found.set(label, file);
  };
  const dir = path.join(FRONTEND, "components");
  for (const f of fs.readdirSync(dir).filter((n) => n.endsWith(".jsx")).sort()) {
    for (const l of jsxLabels(fs.readFileSync(path.join(dir, f), "utf8"))) add(l, `components/${f}`);
  }
  for (const f of ["lib/held.js", "lib/monitor.js"]) {
    for (const l of mapLabels(fs.readFileSync(path.join(FRONTEND, f), "utf8"))) add(l, f);
  }
  return [...found].map(([label, file]) => ({ label, file }));
}

/**
 * Keys of a config YAML, nested ones as "parent.child". A small reader for
 * the shape our config files have (maps, lists of maps, inline { a: 1 }
 * maps, # comments); it is not a general YAML parser.
 */
export function yamlKeys(text) {
  const keys = new Set();
  const stack = []; // [{ indent, key }]
  for (const raw of text.split("\n")) {
    const line = raw.replace(/\s+#.*$/, "").replace(/^\s*#.*$/, "");
    if (!line.trim()) continue;
    const m = line.match(/^(\s*)(- )?\s*([A-Za-z_][A-Za-z0-9_]*):/);
    if (!m) {
      // "- { a: 1, b: 2 }": an inline map in a list.
      const inline = line.match(/^(\s*)- \{(.*)\}/);
      if (inline) {
        const parent = stack.filter((s) => s.indent < inline[1].length).at(-1);
        for (const k of inline[2].matchAll(/([A-Za-z_][A-Za-z0-9_]*):/g)) keys.add(parent ? `${parent.key}.${k[1]}` : k[1]);
      }
      continue;
    }
    const indent = m[1].length + (m[2] ? 2 : 0);
    while (stack.length && stack.at(-1).indent >= indent) stack.pop();
    const parent = stack.at(-1);
    const key = parent ? `${parent.key}.${m[3]}` : m[3];
    keys.add(key);
    stack.push({ indent, key });
  }
  return [...keys];
}

/** Every key set by a shipped config file, with the files that set it. */
export function configKeys() {
  const found = new Map();
  for (const f of CONFIG_FILES) {
    for (const k of yamlKeys(fs.readFileSync(path.join(REPO, f), "utf8"))) {
      found.set(k, [...(found.get(k) ?? []), f]);
    }
  }
  return [...found].map(([key, files]) => ({ key, files }));
}

/** GitHub-style anchors of every heading in a markdown file. */
export function markdownAnchors(file) {
  const text = fs.readFileSync(path.join(REPO, file), "utf8");
  const anchors = new Set();
  for (const m of text.matchAll(/^#{1,6} (.+)$/gm)) {
    anchors.add(
      m[1]
        .trim()
        .toLowerCase()
        .replace(/[^\p{L}\p{N} _-]/gu, "")
        .replace(/ /g, "-"),
    );
  }
  return anchors;
}

export function readGlossary() {
  return JSON.parse(fs.readFileSync(path.join(FRONTEND, "content", "glossary.json"), "utf8"));
}
