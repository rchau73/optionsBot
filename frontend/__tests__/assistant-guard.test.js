/**
 * @jest-environment node
 */
import fs from "node:fs";
import path from "node:path";
import helpIndex from "@/content/help-index.json";
import {
  REASON_ADVICE,
  REASON_FORECAST,
  REASON_NO_CITATION,
  REASON_UNGROUNDED_NUMBER,
  REASON_UNKNOWN_CITATION,
  check,
  numbers,
} from "@/lib/assistant/guard";
import { DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "@/lib/assistant/phrases";

// The JavaScript guard (#191) on the Go guard's own cases
// (tests/assistant_guard_test.go, #260): both must judge an answer the same.

const passages = helpIndex.chunks;
const srcIdea = "OptionStrategy.md#1-the-idea-in-one-paragraph";
const srcParams = "OptionStrategy.md#10-the-parameters-in-plain-words";
const srcRegimes = "OptionStrategy.md#9-market-regimes-and-what-the-bot-does";
const srcLossB = "OptionStrategy.md#loss-b-gap-through-the-stop";

const verdict = (text, ...citations) => check({ text, citations }, passages);

test("a grounded, cited answer passes", () => {
  const v = verdict(
    "A leg is closed early when its delta reaches 0.30, bought back at 2× its premium loss, and a strangle is rolled with 15 days left.",
    srcParams,
  );
  expect(v).toEqual({ ok: true, reasons: [], details: [] });
});

test("an invented number is rejected and named", () => {
  const v = verdict("The take-profit buys a leg back once 65 % of its premium is earned.", srcParams);
  expect(v.reasons).toContain(REASON_UNGROUNDED_NUMBER);
  expect(v.details).toContain("ungrounded_number: 65");
});

test.each([
  ["no citation", "The bot sells options.", [], REASON_NO_CITATION],
  ["I don't know needs no citation", DONT_KNOW, [], ""],
  ["the refusal needs no citation", REFUSAL, [], ""],
  ["the fallback needs no citation", FALLBACK_REPLY, [], ""],
  ["a refusal with more text needs a citation", `${REFUSAL} The stop-loss is a rule.`, [], REASON_NO_CITATION],
  ["unknown citation", "The bot sells options.", ["glossary:no-such-term"], REASON_UNKNOWN_CITATION],
  ["a passage id is a citation", "The bot sells options.", [`${srcIdea}/1`], ""],
  ["ungrounded number", "The stop-loss fires at 3× the premium.", ["glossary:stop-loss"], REASON_UNGROUNDED_NUMBER],
  ["0.30 = 30 %", "A leg is closed early when its delta reaches 30 %.", ["glossary:delta_exit_threshold"], ""],
  ["50 % = 0.50", "The take-profit setting is 0.50.", ["glossary:take-profit"], ""],
  ["thousands separator", "The illustration has BTC at $100,000.", ["OptionStrategy.md#the-payoff-of-a-short-strangle-at-expiry"], ""],
  ["grounded date", "On 2026-10-15 ETH sat frozen.", [srcRegimes], ""],
  ["ungrounded date", "On 2026-01-01 ETH sat frozen.", [srcRegimes], REASON_UNGROUNDED_NUMBER],
  ["advice: recommend", "I recommend the 25-day slot.", [srcParams], REASON_ADVICE],
  ["advice: imperative", "Sell the put now.", [srcIdea], REASON_ADVICE],
  ["advice: your setting", "Increase your margin limit.", [srcParams], REASON_ADVICE],
  ["advice: good time", "It is a good time to sell options.", [srcIdea], REASON_ADVICE],
  ["advice: you should", "You should sell more puts.", [srcParams], REASON_ADVICE],
  ["not advice: the bot does not sell", "In a down-trend the bot does not sell puts.", [srcRegimes], ""],
  ["not advice: puts gain", "Puts gain value when the price falls below the strike.", ["glossary:put"], ""],
  ["forecast: price", "Bitcoin will go up after the expiry.", [srcIdea], REASON_FORECAST],
  ["forecast: return", "The expected return of the strategy is high.", [srcIdea], REASON_FORECAST],
  ["forecast: per month", "With these settings you can expect 2 % a month.", [srcParams], REASON_FORECAST],
  ["forecast: promise", "Selling strangles is risk-free.", [srcIdea], REASON_FORECAST],
  ["not a forecast: a denied guarantee", "Stop-loss levels are triggers, not guaranteed prices.", [srcLossB], ""],
  ["not a forecast: the bot will buy back", "The bot will buy back the leg at market.", [srcIdea], ""],
])("%s", (_name, text, citations, reason) => {
  const v = verdict(text, ...citations);
  if (reason === "") expect(v.details).toEqual([]);
  else expect(v.reasons).toContain(reason);
});

test("an unknown citation is named", () => {
  const v = verdict("The bot sells options.", srcIdea, "OptionStrategy.md#99-nowhere/1");
  expect(v.details).toContain("unknown_citation: OptionStrategy.md#99-nowhere/1");
});

test("numbers are read as the Go guard reads them", () => {
  const keys = numbers("SMA9 rollout_19dte 1,454 lots, 0,30 and 0.30, 72,000 on 2026-10-12, 88k").map((n) => n.key);
  expect(keys).toEqual(["n:1454", "n:0.3", "n:0.3", "n:72000", "date:2026-10-12", "n:88"]);
});

// Every golden question's correct answer passes, and each must_not_say phrase
// added to a valid answer is rejected (as TestAssistantGuard_GoldenSet).
test("the golden set: correct answers pass, must_not_say phrases are rejected", () => {
  const file = path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl");
  const golden = fs.readFileSync(file, "utf8").split("\n").filter((l) => l.trim()).map((l) => JSON.parse(l));
  expect(golden.length).toBeGreaterThan(0);
  const valid = { text: "The bot sells a call and a put.", citations: [srcIdea] };
  const failures = [];
  for (const q of golden) {
    let a = { text: `From the help: ${q.must_say?.join(", ")}.`, citations: q.expected_sources };
    if (q.expected_behaviour === "refuse") a = { text: REFUSAL, citations: [] };
    if (q.expected_behaviour === "dont_know") a = { text: DONT_KNOW, citations: [] };
    const v = check(a, passages);
    if (!v.ok) failures.push(`${q.id}: correct answer rejected: ${v.details}`);
    const base = q.expected_behaviour === "answer" ? a : valid;
    for (const phrase of q.must_not_say ?? []) {
      if (check({ text: `${base.text} ${phrase}.`, citations: base.citations }, passages).ok) {
        failures.push(`${q.id}: must_not_say "${phrase}" passes`);
      }
    }
  }
  expect(failures).toEqual([]);
});
