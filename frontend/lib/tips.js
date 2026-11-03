// Plain-words tips for the settings (#588, epic E11). The source of truth is
// docs/product/education/tips.json; content/tips.json is its copy, because the
// monitor image is built from frontend/ alone (a test fails when it is stale).
// A term's tip is its glossary entry (lib/glossary.js).
import tips from "@/content/tips.json";
import { findTerm } from "@/lib/glossary";

/** A setting's tip as the screens use it, or null. A key with no tip is null, never an empty tip. */
export function findSettingTip(key, entries = tips) {
  if (!Object.hasOwn(entries, key)) return null;
  const { meaning, unit, raise_risk, raise_income, video, policy_note } = entries[key];
  return {
    kind: "setting",
    key,
    meaning,
    unit,
    raise_risk,
    raise_income,
    video: video ?? null,
    needs_product_owner: Boolean(policy_note),
    ...(policy_note ? { policy_note } : {}),
  };
}

/** A glossary term's tip: its short text and its risk and income line, or null. */
export function findTermTip(label) {
  const entry = findTerm(label);
  if (!entry) return null;
  return { kind: "term", term: entry.term, short: entry.short, risk_income: entry.risk_income };
}

/** The tip for a setting key, else for a glossary term or label, else null: a screen asks once. */
export function findTip(key, entries = tips) {
  return findSettingTip(key, entries) ?? findTermTip(key);
}

/** Every setting's tip, by key. */
export function allSettingTips(entries = tips) {
  return Object.keys(entries).sort().map((key) => findSettingTip(key, entries));
}
