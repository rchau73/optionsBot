/**
 * @jest-environment node
 */
import fs from "node:fs";
import path from "node:path";
import * as phrases from "@/lib/assistant/phrases";

// lib/assistant/phrases.js is a copy of internal/assistant/guard/phrases.go
// (#191): the replies and every pattern must stay equal, so counsel's answer
// to L11 changes both in one PR.

const goFile = fs.readFileSync(path.join(process.cwd(), "..", "internal", "assistant", "guard", "phrases.go"), "utf8");

/** The raw-string patterns of `var <name> = compile(…)`. */
function goPatterns(name) {
  const start = goFile.indexOf(`var ${name} = compile(`);
  expect(start).toBeGreaterThanOrEqual(0);
  const block = goFile.slice(start, goFile.indexOf("\n)\n", start));
  return [...block.matchAll(/`([^`]*)`/g)].map((m) => m[1]);
}

/** The value of a const: its quoted strings joined (a "…" + "…" concatenation included). */
function goConst(name) {
  const m = goFile.match(new RegExp(`\\n\\t${name} = ((?:"(?:[^"\\\\]|\\\\.)*"\\s*\\+?\\s*)+)`));
  expect(m).not.toBeNull();
  return [...m[1].matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((s) => JSON.parse(`"${s[1]}"`)).join("");
}

test.each([
  ["advicePhrases", "ADVICE_PATTERNS"],
  ["forecastPhrases", "FORECAST_PATTERNS"],
  ["negations", "NEGATION_PATTERNS"],
  ["adviceNegations", "ADVICE_NEGATION_PATTERNS"],
])("%s equals %s", (goName, jsName) => {
  expect(phrases[jsName]).toEqual(goPatterns(goName));
});

test.each([
  ["DontKnow", "DONT_KNOW"],
  ["Refusal", "REFUSAL"],
  ["FallbackReply", "FALLBACK_REPLY"],
])("the reply %s equals %s", (goName, jsName) => {
  expect(phrases[jsName]).toBe(goConst(goName));
});
