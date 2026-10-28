// The answer guard (#191): the JavaScript twin of internal/assistant/guard
// (#260), so the monitor checks every model answer before it is shown. An
// answer must cite a passage it was given, every number in it must come from
// what it cites, and it must not advise or forecast. Pure: no model, no
// network. __tests__/assistant-guard.test.js runs the Go tests' cases on it.
import { ADVICE_NEGATION_PATTERNS, ADVICE_PATTERNS, DONT_KNOW, FALLBACK_REPLY, FORECAST_PATTERNS, NEGATION_PATTERNS, REFUSAL } from "./phrases";

export const REASON_NO_CITATION = "no_citation";
export const REASON_UNKNOWN_CITATION = "unknown_citation";
export const REASON_UNGROUNDED_NUMBER = "ungrounded_number";
export const REASON_ADVICE = "advice";
export const REASON_FORECAST = "forecast";

const advice = ADVICE_PATTERNS.map((p) => new RegExp(p, "i"));
const adviceNegations = ADVICE_NEGATION_PATTERNS.map((p) => new RegExp(p, "gi"));
const forecasts = FORECAST_PATTERNS.map((p) => new RegExp(p, "i"));
const negations = NEGATION_PATTERNS.map((p) => new RegExp(p, "gi"));

/** True when the text is exactly one of the canonical replies (spacing aside): the only answers that need no citation. */
export function isCanonical(text) {
  const t = String(text ?? "").trim().split(/\s+/).join(" ");
  // "I don't know" for the facts and the refusal for the advice (#427).
  return t === DONT_KNOW || t === REFUSAL || t === FALLBACK_REPLY || t === `${DONT_KNOW} ${REFUSAL}`;
}

// Dates and numbers not glued to a word before them ("SMA9" is not a number).
const NUMBER_RE = /(?:^|[^\p{L}\p{N}_.,])(\d{4}-\d{2}-\d{2}|\d+(?:[.,]\d+)*)/gu;
const THOUSANDS_RE = /^\d{1,3}(,\d{3})+(\.\d+)?$/;

/** The numbers of a text: { text, key, value?, isValue }; "1,454" = 1454, "0,5" = 0.5, a date is kept whole. */
export function numbers(text) {
  const out = [];
  for (const m of String(text ?? "").matchAll(NUMBER_RE)) {
    const s = m[1];
    if (s.length === 10 && s[4] === "-" && s[7] === "-") {
      out.push({ text: s, key: `date:${s}`, isValue: false });
      continue;
    }
    const plain = THOUSANDS_RE.test(s) ? s.replaceAll(",", "") : s.replaceAll(",", ".");
    if (!/^\d+(\.\d+)?$/.test(plain)) {
      // "1.2.3": a version or a list number, kept as text
      out.push({ text: s, key: `text:${s}`, isValue: false });
      continue;
    }
    const value = Number(plain);
    out.push({ text: s, key: `n:${value}`, value, isValue: true });
  }
  return out;
}

const same = (a, b) => Math.abs(a - b) <= 1e-9 * Math.max(1, Math.abs(a), Math.abs(b));
// The same value, or the same as a fraction and a percentage (0.30 and 30 %).
const matchesValue = (x, values) => values.some((v) => same(x, v) || same(x, v * 100) || same(x * 100, v));

/**
 * Checks an answer { text, citations } against the passages it may cite
 * (help passages and the user's journal facts). → { ok, reasons, details }:
 * each reason once, one detail per finding ("unknown_citation: glossary:nope").
 */
export function check(answer, passages) {
  const reasons = [];
  const details = [];
  const add = (reason, detail) => {
    details.push(`${reason}: ${detail}`);
    if (!reasons.includes(reason)) reasons.push(reason);
  };
  const text = String(answer?.text ?? "").trim();
  const citations = Array.isArray(answer?.citations) ? answer.citations : [];

  const cited = [];
  for (const c of citations) {
    const found = passages.filter((p) => p.id === c || p.source === c);
    if (found.length === 0) add(REASON_UNKNOWN_CITATION, c);
    cited.push(...found);
  }
  if (citations.length === 0 && !isCanonical(text)) add(REASON_NO_CITATION, "the answer cites no passage");

  const grounded = new Set();
  const values = [];
  for (const p of cited) {
    for (const n of numbers(`${p.title}\n${p.text}`)) {
      grounded.add(n.key);
      if (n.isValue) values.push(n.value);
    }
  }
  for (const n of numbers(text)) {
    if (!grounded.has(n.key) && !(n.isValue && matchesValue(n.value, values))) add(REASON_UNGROUNDED_NUMBER, n.text);
  }

  let unadvised = text;
  for (const re of adviceNegations) unadvised = unadvised.replace(re, " ");
  for (const re of advice) {
    const m = unadvised.match(re);
    if (m) add(REASON_ADVICE, m[0].trim());
  }
  let unnegated = text;
  for (const re of negations) unnegated = unnegated.replace(re, " ");
  for (const re of forecasts) {
    const m = unnegated.match(re);
    if (m) add(REASON_FORECAST, m[0].trim());
  }

  return { ok: reasons.length === 0, reasons, details };
}
