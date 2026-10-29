/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { whyQuestion } from "@/lib/assistantPanel";
import { check } from "@/lib/assistant/guard";
import { readAnswers } from "@/lib/assistant/log";
import { FALLBACK_REPLY } from "@/lib/assistant/phrases";
import { assistantIndex } from "@/lib/assistant/server";
import { answerQuestion, explainPassages, journalPassages, mergePassages, retryFeedback } from "@/lib/assistant/service";

// A trade's Why? (#534): the question names the instrument and the time as
// the monitor shows them, so the trade's passage must hold both for an
// answer repeating them to pass the guard; and a rejected answer gets one
// more try before the fallback. The fixture is the golden set's why-17, the
// close that was answered right and shown as the fallback on 2026-10-29. A
// fake model: no test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const why17 = golden.find((q) => q.id === "why-17");
const fixture = why17.bot;
const PUT = "BTC-27NOV26-75000-P";

/** The journal one Why? sends: the trade's facts, pinned, then the bot's journal (as the route merges them). */
const whyJournal = (explain = fixture.explain) =>
  mergePassages(explainPassages("btc", explain), journalPassages("btc", fixture.journal.items, fixture.journal.held));

/** A fake model replying each of `replies` in turn (the last one again after that). */
function fakeModel(...replies) {
  const m = { model: "fake", calls: [] };
  m.ask = async (args) => {
    m.calls.push(args);
    return structuredClone(replies[Math.min(m.calls.length, replies.length) - 1]);
  };
  return m;
}

let dir, file;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-why-"));
  file = path.join(dir, "assistant.jsonl");
});
afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

const ask = (model, { question = why17.question, journal = whyJournal(), takeRetry } = {}) =>
  answerQuestion({ question, bot: "btc", journal, user: { username: "viewer", role: "user" }, model, index: assistantIndex(), file, takeRetry });
const logged = () => [...readAnswers(file).values()];

describe("Scenario: a close's Why? answered from its facts", () => {
  test("the question is the one the trades table's Why? asks", () => {
    expect(whyQuestion({ bot: "btc", kind: "close", instrument: PUT, at: fixture.explain.event.at })).toBe(why17.question);
  });

  test("the trade's passage holds its instrument and its time as the monitor shows it and in UTC", () => {
    const [own] = explainPassages("btc", fixture.explain);
    expect(own.id).toBe("journal:btc/52134");
    expect(own.text).toContain(`Instrument ${PUT}.`);
    expect(own.text).toContain("Time: 2026-10-29 17:47:54 BRT (2026-10-29 20:47:54 UTC).");
  });

  test("an answer repeating the question's instrument and time, citing the trade, is shown", async () => {
    const model = fakeModel(fixture.answer);
    const out = await ask(model);
    expect(out.body).toMatchObject({ outcome: "answered", answer: fixture.answer.text });
    expect(out.body.citations.map((c) => c.id)).toEqual(["journal:btc/52134"]);
    for (const phrase of why17.must_say) expect(out.body.answer).toContain(phrase);
    expect(model.calls).toHaveLength(1);
  });

  test("a made-up strike is still rejected", () => {
    const v = check({ ...fixture.answer, text: `${fixture.answer.text} Its strike was 72,000.` }, whyJournal());
    expect(v.details).toEqual(["ungrounded_number: 72,000"]);
  });

  test("a related event cited by name holds its instrument too", () => {
    const [p] = journalPassages("btc", fixture.journal.items);
    expect(p.text).toContain(`Instrument ${PUT}.`);
  });
});

describe("Scenario: an open's Why? answered from its facts", () => {
  const CALL = "BTC-27NOV26-90000-C";
  const AT = "2026-10-21T14:30:05.120Z";
  const open = {
    event: { seq: 3602, event: "filled", instrument: CALL, option_type: "call", trigger_reason: "entry", at: AT, plain: "Sold the call: a new strangle for an empty slot." },
    rule: "entry",
    detail: "entry: 25-day slot, delta 0.18",
    market: { spot: 84498, dvol: 37.2, gex_regime: "POSITIVE" },
    related: [],
    source: "journal",
  };
  const question = whyQuestion({ bot: "btc", kind: "open", instrument: CALL, at: AT });
  const answer = {
    text: `BTC opened the call ${CALL} on 2026-10-21 11:30:05 BRT as a new strangle for an empty 25-day slot, at delta 0.18, with spot at 84,498 and DVOL 37.2.`,
    citations: ["journal:btc/3602"],
  };

  test("the question names the time the passage holds", () => {
    expect(question).toBe(`Why did BTC open the call ${CALL} on 2026-10-21 11:30:05 BRT?`);
  });

  test("the answer repeating it is shown; a made-up strike is still rejected", async () => {
    const out = await ask(fakeModel(answer), { question, journal: whyJournal(open) });
    expect(out.body).toMatchObject({ outcome: "answered", answer: answer.text });
    const v = check({ ...answer, text: `${answer.text} The strike was 95,000.` }, whyJournal(open));
    expect(v.details).toEqual(["ungrounded_number: 95,000"]);
  });
});

describe("Scenario: a rejected answer gets one more try", () => {
  const ungrounded = { text: `BTC closed the put ${PUT} at a strike of 72,000.`, citations: ["journal:btc/52134"] };

  test("an ungrounded first answer, a grounded second: the second is shown and the log has both", async () => {
    const model = fakeModel(ungrounded, fixture.answer);
    const out = await ask(model);
    expect(out.body).toMatchObject({ outcome: "answered", answer: fixture.answer.text });
    expect(model.calls).toHaveLength(2);
    // The model is told what failed, with its first answer.
    expect(model.calls[1].retry).toEqual({
      text: ungrounded.text,
      citations: ungrounded.citations,
      feedback: "72,000 isn't in the sources you cited: cite the source that has them, or leave them out.",
    });
    const [line] = logged();
    expect(line.first_answer).toEqual({ model_answer: ungrounded.text, citations: ungrounded.citations, verdict: ["ungrounded_number: 72,000"] });
    expect(line).toMatchObject({ model_answer: fixture.answer.text, outcome: "answered", verdict: [] });
  });

  test("two rejected answers: the fallback is shown, and it names no document", async () => {
    const model = fakeModel(ungrounded, { text: "Read OptionStrategy.md for the delta exit.", citations: ["journal:btc/52134"] });
    const out = await ask(model);
    expect(out.body).toMatchObject({ outcome: "rejected", answer: FALLBACK_REPLY, citations: [] });
    expect(out.body.answer).not.toMatch(/see |read |guide|\.md|section|document/i);
    expect(model.calls).toHaveLength(2);
    const [line] = logged();
    expect(line.first_answer.model_answer).toBe(ungrounded.text);
    expect(line.verdict).toEqual(["points_to_document: Read OptionStrategy.md"]);
  });

  test("an answer pointing to a document is retried, and the model told not to", async () => {
    const pointer = { text: "The put was closed by its delta exit: see the guide, OptionStrategy.md.", citations: ["journal:btc/52134"] };
    const model = fakeModel(pointer, fixture.answer);
    expect((await ask(model)).body.outcome).toBe("answered");
    expect(model.calls[1].retry.feedback).toBe(
      "Don't send the person to a document, guide or section (\"see the guide\"): say what the source says, in your own words.",
    );
  });

  test("an unknown citation is retried with the ids it may cite", async () => {
    const model = fakeModel({ text: "The put was closed by its delta exit.", citations: ["journal:btc/9"] }, fixture.answer);
    expect((await ask(model)).body.outcome).toBe("answered");
    expect(model.calls[1].retry.feedback).toBe("journal:btc/9 isn't among the listed sources: cite only the ids in square brackets.");
  });

  test.each([
    ["advice", "You should sell the put again."],
    ["a forecast", "Bitcoin will go up after the expiry."],
    ["advice with an ungrounded number", "You should sell the 72,000 put again."],
  ])("%s is never retried", async (_name, text) => {
    const model = fakeModel({ text, citations: ["journal:btc/52134"] }, fixture.answer);
    const out = await ask(model);
    expect(out.body).toMatchObject({ outcome: "rejected", answer: FALLBACK_REPLY });
    expect(model.calls).toHaveLength(1);
    expect(logged()[0].first_answer).toBeUndefined();
  });

  test("no budget left for the retry: the fallback, without a second call", async () => {
    const model = fakeModel(ungrounded, fixture.answer);
    const out = await ask(model, { takeRetry: () => false });
    expect(out.body).toMatchObject({ outcome: "rejected", answer: FALLBACK_REPLY });
    expect(model.calls).toHaveLength(1);
  });

  test("a retry that fails keeps the first verdict and logs the error", async () => {
    const model = fakeModel(ungrounded);
    model.ask = async (args) => {
      model.calls.push(args);
      if (model.calls.length > 1) throw new Error("model API answered 529");
      return structuredClone(ungrounded);
    };
    const out = await ask(model);
    expect(out.status).toBe(200);
    expect(out.body).toMatchObject({ outcome: "rejected", answer: FALLBACK_REPLY });
    expect(logged()[0]).toMatchObject({ retry_error: "model API answered 529", verdict: ["ungrounded_number: 72,000"] });
  });
});

describe("retryFeedback", () => {
  test.each([
    [{ ok: true, reasons: [], details: [] }, null],
    [{ ok: false, reasons: ["no_citation"], details: ["no_citation: the answer cites no passage"] }, null],
    [{ ok: false, reasons: ["ungrounded_number", "forecast"], details: ["ungrounded_number: 3", "forecast: will go up"] }, null],
    [
      { ok: false, reasons: ["ungrounded_number"], details: ["ungrounded_number: 27", "ungrounded_number: 75000"] },
      "27, 75000 aren't in the sources you cited: cite the source that has them, or leave them out.",
    ],
  ])("%j → %j", (verdict, want) => {
    expect(retryFeedback(verdict)).toBe(want);
  });
});
