/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import helpIndex from "@/content/help-index.json";
import { REASON_LANGUAGE, check, isEnglish } from "@/lib/assistant/guard";
import { readAnswers } from "@/lib/assistant/log";
import { SYSTEM_PROMPT } from "@/lib/assistant/model";
import { DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "@/lib/assistant/phrases";
import { assistantIndex } from "@/lib/assistant/server";
import { noAnswer } from "@/lib/assistant/search";
import { HELP_PASSAGES, answerQuestion, retryFeedback } from "@/lib/assistant/service";

// The guard holds in any language (#565): an answer not in English is
// rejected as `language` (the phrase lists read English only), asked once
// more, then the fallback. The cases are the Go test's own
// (tests/testdata/assistant_language_cases.json). A fake model: no test calls
// the model API.

const repo = path.join(process.cwd(), "..");
const { cases } = JSON.parse(fs.readFileSync(path.join(repo, "tests", "testdata", "assistant_language_cases.json"), "utf8"));
const golden = fs
  .readFileSync(path.join(repo, "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const srcIdea = "OptionStrategy.md#1-the-idea-in-one-paragraph";

describe("Scenario: A Portuguese answer is rejected by the guard", () => {
  test.each(cases.map((c) => [c.name, c]))("%s", (_name, c) => {
    const v = check({ text: c.text.replaceAll("{refusal}", REFUSAL), citations: c.citations }, helpIndex.chunks);
    if (c.reason === "") expect(v.details).toEqual([]);
    else {
      // Only the language, as the Go test asserts: both guards agree.
      expect(v.reasons).toEqual([c.reason]);
      expect(v.details[0]).toMatch(/^language: \d+ of \d+ words are English$/);
    }
  });
});

/** A fake model replying each of `replies` in turn (the last one again after that). */
function fakeModel(...replies) {
  const m = { model: "fake", calls: [] };
  m.ask = async (args) => {
    m.calls.push(args);
    const r = replies[Math.min(m.calls.length, replies.length) - 1];
    // A reply citing "first" cites the first passage the model was given.
    return { ...r, citations: r.citations.map((c) => (c === "first" ? args.passages[0].id : c)) };
  };
  return m;
}

let dir, file;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-language-"));
  file = path.join(dir, "assistant.jsonl");
});
afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

const ask = (model, question) => answerQuestion({ question, user: { username: "viewer", role: "user" }, model, index: assistantIndex(), file });
const logged = () => [...readAnswers(file).values()];

const jar08 = golden.find((q) => q.id === "jar-08");
const adv08 = golden.find((q) => q.id === "adv-08");
const portuguese = { text: "Um strangle é uma call vendida acima do preço de hoje e uma put vendida abaixo dele.", citations: ["first"] };
const english = { text: "A strangle is One call sold above today's price and one put sold below it, with the same expiry.", citations: ["glossary:strangle"] };

describe("a question not in English reaches the model", () => {
  test("the English help matches no passage closely, yet its best hits go to the model", async () => {
    expect(noAnswer(assistantIndex().search(adv08.question, HELP_PASSAGES))).toBe(true);
    const model = fakeModel({ text: REFUSAL, citations: [] });
    const out = await ask(model, adv08.question);
    expect(out.body).toMatchObject({ outcome: "refused", answer: REFUSAL });
    expect(model.calls).toHaveLength(1);
    expect(model.calls[0].passages.length).toBeGreaterThan(0);
  });

  test("an English question with no close passage still gets I don't know without a model call", async () => {
    const model = fakeModel(english);
    const out = await ask(model, "What is the capital of Mongolia?");
    expect(out.body).toMatchObject({ outcome: "dont_know", answer: DONT_KNOW });
    expect(model.calls).toHaveLength(0);
  });

  test("isEnglish reads the question as the guard reads an answer", () => {
    expect(isEnglish(adv08.question)).toBe(false);
    expect(isEnglish("Should I sell more puts?")).toBe(true);
  });
});

describe("Scenario: The model is asked once more, then the safe reply", () => {
  test("two answers in Portuguese: the fallback, naming no document, logged with the reason language", async () => {
    const model = fakeModel(portuguese);
    const out = await ask(model, jar08.question);
    expect(out.body).toMatchObject({ outcome: "rejected", answer: FALLBACK_REPLY, citations: [] });
    expect(out.body.answer).not.toMatch(/see |read |guide|\.md|section|document/i);
    expect(model.calls).toHaveLength(2);
    expect(model.calls[1].retry.feedback).toBe("Your answer was not in English: answer in English, whatever language the question is in.");
    const [line] = logged();
    expect(line.first_answer.verdict[0]).toMatch(/^language: /);
    expect(line.verdict.map((d) => d.split(":")[0])).toEqual([REASON_LANGUAGE]);
  });

  test("Portuguese, then English: the English answer is shown (golden jar-08)", async () => {
    const model = fakeModel(portuguese, english);
    const out = await ask(model, jar08.question);
    // The question finds its expected source, so the English answer can cite it.
    expect(model.calls[0].passages.map((p) => p.source)).toContain(jar08.expected_sources[0]);
    expect(out.body).toMatchObject({ outcome: "answered", answer: english.text });
    for (const phrase of jar08.must_say) expect(out.body.answer).toContain(phrase);
  });

  test("language is retried alone or with another retried reason, never with advice", () => {
    expect(retryFeedback({ ok: false, reasons: ["language"], details: ["language: 0 of 9 words are English"] })).not.toBeNull();
    expect(retryFeedback({ ok: false, reasons: ["language", "advice"], details: ["language: 1 of 12 words are English", "advice: you should"] })).toBeNull();
  });

  test("the service prompt says the assistant answers in English", () => {
    expect(SYSTEM_PROMPT).toContain("Always answer in English, whatever language the question is written in.");
  });
});

describe("Scenario: A Portuguese advice question never gets free-text advice", () => {
  const fct06 = golden.find((q) => q.id === "fct-06");
  const ptAdvice = { text: "Sim, você deve vender mais puts agora, porque a volatilidade está alta.", citations: ["first"] };
  const ptForecast = { text: "O bitcoin vai subir esta semana e o bot vai render mais do que no mês passado.", citations: ["first"] };

  test("the golden question is the scenario's", () => {
    expect(adv08.question).toBe("Devo vender mais puts agora?");
    expect(adv08.expected_behaviour).toBe("refuse");
  });

  test.each([
    ["adv-08", "advice in Portuguese twice", [ptAdvice], "rejected", FALLBACK_REPLY],
    ["adv-08", "advice in Portuguese, then in English", [ptAdvice, { text: "You should sell more puts now.", citations: ["first"] }], "rejected", FALLBACK_REPLY],
    ["adv-08", "the refusal", [{ text: REFUSAL, citations: [] }], "refused", REFUSAL],
    ["fct-06", "a forecast in Portuguese twice", [ptForecast], "rejected", FALLBACK_REPLY],
    ["fct-06", "a forecast in Portuguese, then in English", [ptForecast, { text: "Bitcoin will go up this week.", citations: ["first"] }], "rejected", FALLBACK_REPLY],
  ])("%s, %s → %s", async (id, _name, replies, outcome, answer) => {
    const q = id === "adv-08" ? adv08 : fct06;
    const out = await ask(fakeModel(...replies), q.question);
    expect(out.body).toMatchObject({ outcome, answer });
  });
});

describe("Scenario: A normal English answer is unaffected", () => {
  test("no canonical reply is rejected", () => {
    for (const text of [DONT_KNOW, REFUSAL, FALLBACK_REPLY, `${DONT_KNOW} ${REFUSAL}`]) expect(check({ text, citations: [] }, helpIndex.chunks).details).toEqual([]);
  });

  test("no English golden case's correct answer reads as not English", () => {
    const failures = [];
    for (const q of golden) {
      if (q.expected_behaviour !== "answer" || q.bot || q.category === "request") continue;
      const v = check({ text: `From the help: ${q.must_say.join(", ")}.`, citations: q.expected_sources }, helpIndex.chunks);
      if (v.reasons.includes(REASON_LANGUAGE)) failures.push(`${q.id}: ${v.details}`);
    }
    expect(failures).toEqual([]);
  });
});
