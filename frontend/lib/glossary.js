// Plain-words tips for the monitor's terms and the config settings (#188).
// Content lives in content/glossary.json: { term: { short, long,
// risk_income, source, labels? } } — `labels` are the exact texts the
// monitor shows for that term (column headers, tiles, badges).
import glossary from "@/content/glossary.json";

export const MAX_SHORT = 140;
export const MAX_LONG = 600;

/** Case- and space-insensitive form of a label or term. */
export function normalise(text) {
  return String(text ?? "")
    .replace(/&amp;/g, "&")
    .replace(/\s+/g, " ")
    .trim()
    .toLowerCase();
}

/** Index of every term and label → term key, built once per glossary. */
export function buildIndex(entries = glossary) {
  const index = new Map();
  for (const [term, entry] of Object.entries(entries)) {
    index.set(normalise(term), term);
    for (const label of entry.labels ?? []) {
      if (!index.has(normalise(label))) index.set(normalise(label), term);
    }
  }
  return index;
}

const INDEX = buildIndex();

/** The entry for a term, config key or monitor label: { term, ...entry }, or null. */
export function findTerm(label, entries = glossary, index = entries === glossary ? INDEX : buildIndex(entries)) {
  const term = index.get(normalise(label));
  return term ? { term, ...entries[term] } : null;
}

export default glossary;
