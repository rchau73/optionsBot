/**
 * @jest-environment node
 */
import fs from "node:fs";
import path from "node:path";
import helpIndex from "@/content/help-index.json";
import { HelpIndex, noAnswer, tokens } from "@/lib/assistant/search";

// The JavaScript search (#191) is internal/helpkb's BM25 (#259): scored on
// the Go tests' question set, it must reach the same baseline.

const testdata = path.join(process.cwd(), "..", "tests", "testdata");

/** tests/testdata/helpkb_questions.yaml: "- question:", "expect: [...]", "dont_know: true" lines. */
function questions() {
  const out = [];
  for (const line of fs.readFileSync(path.join(testdata, "helpkb_questions.yaml"), "utf8").split("\n")) {
    const q = line.match(/^\s*- question:\s*(.+)$/);
    if (q) out.push({ question: q[1].trim(), expect: [], dontKnow: false });
    const e = line.match(/^\s*expect:\s*(\[.*\])\s*$/);
    if (e) out[out.length - 1].expect = JSON.parse(e[1]);
    if (/^\s*dont_know:\s*true/.test(line)) out[out.length - 1].dontKnow = true;
  }
  return out;
}

test("tokens drop stop words and stem as the Go index does", () => {
  expect(tokens("What happens when the leg's delta reaches 0.30?")).toEqual(["leg", "delta", "reache", "0.30"]);
  expect(tokens("stopped stopping stops strangles")).toEqual(["stop", "stop", "stop", "strangle"]);
});

test("the question set scores at least the Go baseline", () => {
  const baseline = JSON.parse(fs.readFileSync(path.join(testdata, "helpkb_baseline.json"), "utf8"));
  const ix = new HelpIndex(helpIndex.chunks);
  const qs = questions();
  expect(qs.length).toBeGreaterThan(10);
  let answerable = 0;
  let hit1 = 0;
  let hit3 = 0;
  let none = 0;
  let noneRight = 0;
  for (const q of qs) {
    const hits = ix.search(q.question, 3);
    if (noAnswer(hits)) {
      none++;
      if (q.dontKnow) noneRight++;
    }
    if (q.dontKnow) continue;
    answerable++;
    const rank = hits.findIndex((h) => q.expect.some((e) => h.passage.source.startsWith(e))) + 1;
    if (rank === 1) hit1++;
    if (rank > 0) hit3++;
  }
  expect(hit1 / answerable).toBeGreaterThanOrEqual(baseline.hit_at_1);
  expect(hit3 / answerable).toBeGreaterThanOrEqual(baseline.hit_at_3);
  expect(none > 0 ? noneRight / none : 1).toBeGreaterThanOrEqual(baseline.dont_know_precision);
});

test("an empty question or an empty index finds nothing", () => {
  expect(new HelpIndex(helpIndex.chunks).search("the of and", 3)).toEqual([]);
  expect(new HelpIndex([]).search("stop-loss", 3)).toEqual([]);
  expect(noAnswer([])).toBe(true);
});
