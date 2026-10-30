/**
 * @jest-environment node
 */
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GOLDEN_BOT, MAX_TRIES, PAUSE_MS, TEST_RUN_USER, answerLine, goldenInputs, runGolden, runLogError } from "@/lib/assistant/goldenrun";
import { readAnswers } from "@/lib/assistant/log";
import { parseRequestReply } from "@/lib/assistant/model";
import { DONT_KNOW } from "@/lib/assistant/phrases";
import { assistantIndex } from "@/lib/assistant/server";

// The live golden-set run (#536): every golden question through the real
// assistant service, on its own fixtures, into the answers.jsonl the grader
// reads — a test run that no review counts. A fake model: no test calls the
// model API.

const ROOT = path.join(process.cwd(), "..");
const GOLDEN_FILE = path.join(ROOT, "docs", "product", "assistant", "golden.jsonl");
const golden = fs
  .readFileSync(GOLDEN_FILE, "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const byQuestion = new Map(golden.map((q) => [q.question, q]));
const byId = Object.fromEntries(golden.map((q) => [q.id, q]));

/** "I don't know" to every plain question, the golden verdict to every report; `fail` throws for those ids (n times each). */
function fakeModel({ fail = {} } = {}) {
  const m = { model: "fake", asks: [], checks: [] };
  const maybeFail = (question) => {
    const q = byQuestion.get(question);
    if (fail[q.id] > 0) {
      fail[q.id]--;
      throw new Error("model API answered 529");
    }
    return q;
  };
  m.ask = async (args) => {
    m.asks.push(args);
    maybeFail(args.question);
    return { text: DONT_KNOW, citations: [], report: false };
  };
  m.checkRequest = async (args) => {
    m.checks.push(args);
    const q = maybeFail(args.question);
    const verdict = q.request?.verdict ?? { ...byId["req-01"].request.verdict, kind: "none", answer: DONT_KNOW };
    // The golden verdict is the model's raw reply: read as the real client reads it.
    return parseRequestReply({ stop_reason: "end_turn", content: [{ type: "text", text: JSON.stringify(verdict) }] });
  };
  return m;
}

let dir, file, sleeps;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-golden-"));
  file = path.join(dir, "run.jsonl");
  sleeps = [];
});
afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

const run = (model, set = golden) => runGolden({ golden: set, model, index: assistantIndex(), file, sleep: async (ms) => sleeps.push(ms) });

describe("Scenario: every golden question is asked", () => {
  test("one answers.jsonl line per golden line, in order, in the grader's shape", async () => {
    const { answers, modelErrors } = await run(fakeModel());
    expect(answers.map((a) => a.id)).toEqual(golden.map((q) => q.id));
    expect(modelErrors).toBe(0);
    for (const a of answers) {
      expect(Object.keys(a).sort()).toEqual(["citations", "id", "outcome", "text"]);
      expect(typeof a.text).toBe("string");
      expect(Array.isArray(a.citations)).toBe(true);
    }
    // The request cases carry the request check's outcome, as the grader expects.
    for (const id of ["req-01", "req-02", "req-03"]) expect(answers.find((a) => a.id === id).outcome).toBe(byId[id].expected_behaviour);
  });

  test("assistant_grade.py scores it per category without error", async () => {
    const { answers } = await run(fakeModel());
    const out = path.join(dir, "answers.jsonl");
    fs.writeFileSync(out, answers.map((a) => JSON.stringify(a)).join("\n") + "\n");
    const r = spawnSync("python3", [path.join(ROOT, "scripts", "product", "assistant_grade.py"), out, "--json"], { encoding: "utf8" });
    // 1 = some answers fail (the fake says "I don't know" to everything); 2 would be bad input.
    expect([r.status, r.stderr]).toEqual([1, ""]);
    const { scores } = JSON.parse(r.stdout);
    expect(scores.all.total).toBe(golden.length);
    for (const cat of new Set(golden.map((q) => q.category))) expect(scores[cat].total).toBeGreaterThan(0);
    expect(scores.unknown.passed).toBe(scores.unknown.total);
  });

  test("the fixtures reach the model: a bot question its page data and journal, a report its P&L points", async () => {
    const model = fakeModel();
    await run(model, [byId["why-16"], byId["why-17"], byId["req-02"], byId["how-01"]]);
    const sent = (q) => [...model.asks, ...model.checks].find((a) => a.question === q.question).passages.map((p) => p.id);
    expect(sent(byId["why-16"])).toEqual(expect.arrayContaining([`live:${GOLDEN_BOT}/market`, `live:${GOLDEN_BOT}/leg/BTC-27NOV26-75000-P`]));
    expect(sent(byId["why-17"])).toContain(`journal:${GOLDEN_BOT}/52134`);
    expect(sent(byId["req-02"])).toEqual(expect.arrayContaining(["journal:btc/101", "journal:btc/102", "pnl:btc/2026-10-27T12:00:00Z"]));
    expect(sent(byId["how-01"]).some((id) => id.startsWith("live:") || id.startsWith("journal:"))).toBe(false);
  });

  test("goldenInputs: no fixture → no bot", () => {
    expect(goldenInputs(byId["how-01"])).toMatchObject({ bot: null, live: [], journal: [] });
    expect(goldenInputs(byId["req-01"]).bot).toBeNull();
    expect(goldenInputs(byId["why-16"]).bot).toBe(GOLDEN_BOT);
  });
});

describe("Scenario: rate limit respected", () => {
  test("it paces itself between questions, outside the per-person limit", async () => {
    const set = golden.slice(0, 40); // more than the 30 an hour a person may ask
    const { answers } = await run(fakeModel(), set);
    expect(answers).toHaveLength(40);
    expect(sleeps).toEqual(Array(39).fill(PAUSE_MS));
  });

  test("a failed model call is asked once more; failed again, it is a model_error line, never lost or retried without limit", async () => {
    const model = fakeModel({ fail: { "how-01": 1, "how-02": 99 } });
    const set = [byId["how-01"], byId["how-02"], byId["how-03"]];
    const { answers, modelErrors } = await run(model, set);
    expect(answers.map((a) => a.id)).toEqual(["how-01", "how-02", "how-03"]);
    expect(answers[0].outcome).toBe("dont_know");
    expect(answers[1]).toEqual({ id: "how-02", text: "", citations: [], outcome: "model_error" });
    expect(modelErrors).toBe(1);
    expect(model.asks.filter((a) => a.question === byId["how-02"].question)).toHaveLength(MAX_TRIES);
  });
});

describe("Scenario: test questions stay out of the user-facing numbers", () => {
  test("every line of the run's own log is marked test_run, by no person; no review log or request queue is written", async () => {
    await run(fakeModel());
    const lines = [...readAnswers(file).values()];
    expect(lines).toHaveLength(golden.length);
    for (const e of lines) expect(e).toMatchObject({ test_run: true, user: TEST_RUN_USER.username, role: TEST_RUN_USER.role });
    expect(fs.readdirSync(dir).sort()).toEqual(["run.jsonl"]);
  });

  test("the run refuses the monitor's own files as its log, before asking anything", async () => {
    for (const name of ["assistant.jsonl", "requests.jsonl", "requests_status.jsonl"]) {
      expect(runLogError(path.join(dir, name))).toMatch(/monitor's own file/);
      const model = fakeModel();
      await expect(runGolden({ golden, model, index: assistantIndex(), file: path.join(dir, name), sleep: async () => {} })).rejects.toThrow(/own file/);
      expect(model.asks).toHaveLength(0);
    }
    expect(runLogError("")).toMatch(/required/);
    expect(runLogError(file)).toBeNull();
  });

  test("answerLine: the shown answer and the ids of its citations", () => {
    const body = { answer: "A strangle is…", citations: [{ id: "glossary:strangle/1", source: "glossary:strangle", title: "Strangle" }], outcome: "answered" };
    expect(answerLine("jar-01", { status: 200, body })).toEqual({ id: "jar-01", text: "A strangle is…", citations: ["glossary:strangle/1"], outcome: "answered" });
  });
});

describe("Scenario: no key", () => {
  test("the CLI stops with one clear message and writes nothing", () => {
    const env = { ...process.env };
    delete env.ANTHROPIC_API_KEY;
    const out = path.join(dir, "answers.jsonl");
    const r = spawnSync(process.execPath, [path.join(process.cwd(), "scripts", "assistant-run.mjs"), "--out", out, "--log", file], { env, encoding: "utf8" });
    expect(r.status).toBe(2);
    expect(r.stderr.trim()).toBe("assistant-run: ANTHROPIC_API_KEY is not set: nothing was asked and no answer or score was written");
    expect(fs.readdirSync(dir)).toEqual([]);
  });

  test("with a key, the CLI still refuses the people's review log as its own, asking nothing", () => {
    const env = { ...process.env, ANTHROPIC_API_KEY: "not-a-key" };
    const out = path.join(dir, "answers.jsonl");
    const r = spawnSync(process.execPath, [path.join(process.cwd(), "scripts", "assistant-run.mjs"), "--out", out, "--log", path.join(dir, "assistant.jsonl")], { env, encoding: "utf8" });
    expect(r.status).toBe(2);
    expect(r.stderr).toMatch(/assistant.jsonl is the monitor's own file/);
    expect(fs.readdirSync(dir)).toEqual([]);
  });
});
