// Plain-words checks (#339): short sentences, and no jargon word in a text
// unless that word has its own glossary entry (or the entry defines it).
// Pure functions that return the problems found, like emptyStateChecks.js.
import { findTerm } from "@/lib/glossary";

export const MAX_SENTENCE_WORDS = 25;

/** Words a non-technical reader may not know. Each must be a glossary key or label to be used. */
export const JARGON = ["delta", "gamma", "theta", "vega", "strangle", "iv", "dvol", "gex", "margin", "strike"];

const WORD = /[a-z][a-z&'-]*/gi;

/** Sentences of a text longer than `max` words (the sentence is returned, to be named). */
export function longSentences(text, max = MAX_SENTENCE_WORDS) {
  return String(text ?? "")
    .split(/(?<=[.!?;])\s+/)
    .filter((s) => s.trim().split(/\s+/).length > max);
}

/**
 * Jargon words used in `text` that are neither a glossary key/label nor the term being defined.
 * `own` is the entry's own key; `allow` lists words an entry may use anyway ("entry:word" → reason).
 */
export function undefinedJargon(text, own, find = findTerm, allow = {}) {
  const found = new Set();
  for (const m of String(text ?? "").matchAll(WORD)) {
    const word = m[0].toLowerCase();
    if (!JARGON.includes(word)) continue;
    if (find(word)) continue;
    if (String(own).toLowerCase().split(/[\s._]+/).includes(word)) continue;
    if (allow[`${own}:${word}`]) continue;
    found.add(word);
  }
  return [...found];
}
