/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as ask } from "@/app/api/assistant/route";
import { POST as flag } from "@/app/api/assistant/flag/route";
import { readAnswers } from "@/lib/assistant/log";
import { QUESTIONS_PER_HOUR } from "@/lib/assistant/limit";
import { AI_NOTICE, DONT_KNOW, FALLBACK_REPLY } from "@/lib/assistant/phrases";
import { setAssistantModel } from "@/lib/assistant/server";
import { signedIn, useTestAuth } from "@/test/auth";

// POST /api/assistant and /api/assistant/flag (#191) with a fake model: no
// test calls the model API.

// A source the search returns for "what is a stop-loss": the model may cite only what it was given.
const SRC = "glossary:stop-loss";

let dir, file, cookie, model;
const request = (url, body, headers = {}) =>
  new Request(`http://monitor${url}`, {
    method: "POST",
    headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
const askQ = (body, headers) => ask(request("/api/assistant", body, headers));
const flagA = (body, headers) => flag(request("/api/assistant/flag", body, headers));
const logged = () => [...readAnswers(file).values()];

/** A model that always replies `reply` and records what it was asked. */
function fakeModel(reply) {
  const m = { model: "fake", calls: [], ask: jest.fn(async (args) => (m.calls.push(args), typeof reply === "function" ? reply(args) : reply)) };
  return m;
}

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-"));
  file = path.join(dir, "assistant.jsonl");
  process.env.ASSISTANT_LOG_PATH = file;
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
  ({ cookie } = await signedIn("viewer", "user"));
  model = fakeModel({ text: "A leg is bought back when a rule fires.", citations: [SRC] });
  setAssistantModel(model);
});
afterEach(() => {
  jest.restoreAllMocks();
  setAssistantModel(undefined);
  delete process.env.ASSISTANT_LOG_PATH;
  delete process.env.BOT_APIS;
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Scenario: cites or declines", () => {
  test("an answer without a citation is replaced by \"I don't know\" and logged", async () => {
    setAssistantModel(fakeModel({ text: "The stop-loss buys a leg back.", citations: [] }));
    const res = await askQ({ question: "what is a stop-loss" });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body).toMatchObject({ answer: DONT_KNOW, citations: [], outcome: "no_citation", ai_generated: true, notice: AI_NOTICE });
    const [line] = logged();
    expect(line).toMatchObject({
      id: body.id,
      user: "viewer",
      question: "what is a stop-loss",
      model_answer: "The stop-loss buys a leg back.",
      answer: DONT_KNOW,
      outcome: "no_citation",
      flag: false,
    });
    expect(line.sources.length).toBeGreaterThan(0);
  });

  test("a cited, grounded answer is shown with its sources", async () => {
    const res = await askQ({ question: "what is a stop-loss" });
    const body = await res.json();
    expect(body.outcome).toBe("answered");
    expect(body.answer).toBe("A leg is bought back when a rule fires.");
    expect(body.citations).toEqual([{ id: SRC, source: SRC, title: expect.any(String) }]);
    // The model got the matching help passages and the question.
    expect(model.calls[0].question).toBe("what is a stop-loss");
    expect(model.calls[0].passages.some((p) => p.source === SRC)).toBe(true);
  });

  test.each([
    ["advice", "You should sell more puts."],
    ["a forecast", "Bitcoin will go up after the expiry."],
    ["a number from nowhere", "The stop-loss fires at 7× the premium."],
  ])("an answer with %s is replaced by the fallback reply", async (_name, text) => {
    setAssistantModel(fakeModel({ text, citations: [SRC] }));
    const body = await (await askQ({ question: "what is a stop-loss" })).json();
    expect(body).toMatchObject({ answer: FALLBACK_REPLY, outcome: "rejected", citations: [] });
    expect(logged()[0].model_answer).toBe(text);
  });

  test("a citation the model was not given is rejected", async () => {
    setAssistantModel(fakeModel({ text: "The bot sells options.", citations: ["glossary:no-such-term"] }));
    const body = await (await askQ({ question: "what is a stop-loss" })).json();
    expect(body.outcome).toBe("rejected");
    expect(logged()[0].verdict).toContain("unknown_citation: glossary:no-such-term");
  });

  test("a question the help does not touch is answered \"I don't know\" without a model call", async () => {
    const body = await (await askQ({ question: "zxqv wplm" })).json();
    expect(body).toMatchObject({ answer: DONT_KNOW, outcome: "dont_know" });
    expect(model.ask).not.toHaveBeenCalled();
    expect(logged()[0]).toMatchObject({ outcome: "dont_know", model: null });
  });

  test("a model failure answers 502 and is logged", async () => {
    setAssistantModel({ model: "fake", ask: async () => { throw new Error("model API answered 529"); } });
    const res = await askQ({ question: "what is a stop-loss" });
    expect(res.status).toBe(502);
    expect(logged()[0]).toMatchObject({ outcome: "model_error", answer: null });
  });
});

describe("Scenario: signed-in only", () => {
  test.each([
    ["the assistant", () => askQ({ question: "what is a stop-loss" }, { cookie: "" })],
    ["the flag", () => flagA({ id: "x" }, { cookie: "" })],
  ])("an unauthenticated request to %s gets 401", async (_name, send) => {
    expect((await send()).status).toBe(401);
    expect(model.ask).not.toHaveBeenCalled();
    expect(logged()).toEqual([]);
  });
});

describe("the request", () => {
  test("off without ANTHROPIC_API_KEY (503)", async () => {
    setAssistantModel(null);
    expect((await askQ({ question: "what is a stop-loss" })).status).toBe(503);
  });

  test.each([
    ["no question", {}],
    ["an empty question", { question: "  " }],
    ["a question too long", { question: "a".repeat(501) }],
    ["a bot that is not a name", { question: "why", bot: 3 }],
  ])("%s is refused (400)", async (_name, body) => {
    expect((await askQ(body)).status).toBe(400);
    expect(logged()).toEqual([]);
  });

  test("a cross-origin request is refused", async () => {
    expect((await askQ({ question: "what is a stop-loss" }, { origin: "http://evil" })).status).toBe(403);
  });

  test(`more than ${QUESTIONS_PER_HOUR} questions an hour get 429 with Retry-After`, async () => {
    for (let i = 0; i < QUESTIONS_PER_HOUR; i++) expect((await askQ({ question: "what is a stop-loss" })).status).toBe(200);
    const res = await askQ({ question: "what is a stop-loss" });
    expect(res.status).toBe(429);
    expect(Number(res.headers.get("retry-after"))).toBeGreaterThan(0);
    // Another person still may ask.
    ({ cookie } = await signedIn("other", "user"));
    expect((await askQ({ question: "what is a stop-loss" })).status).toBe(200);
  });

  test("with a bot, its journal facts are sources the answer may cite", async () => {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    const events = [{ seq: 41, event: "closed", at: "2026-10-27T10:00:00Z", plain: "The bot bought back the put: its stop-loss fired at 2× the premium." }];
    const fetchSpy = jest.spyOn(global, "fetch").mockResolvedValue(Response.json({ events }));
    setAssistantModel(fakeModel({ text: "Its stop-loss fired at 2× the premium.", citations: ["journal:btc/41"] }));
    const body = await (await askQ({ question: "why did the bot close the put", bot: "btc" })).json();
    expect(fetchSpy).toHaveBeenCalledWith("http://bot-btc:8081/api/events?limit=20", expect.anything());
    expect(body.outcome).toBe("answered");
    expect(body.citations).toEqual([{ id: "journal:btc/41", source: "journal:btc", title: "btc journal: closed at 2026-10-27T10:00:00Z" }]);
    expect(logged()[0].sources).toContain("journal:btc/41");
  });

  test("an unknown bot gets 404", async () => {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    expect((await askQ({ question: "why", bot: "doge" })).status).toBe(404);
  });

  test("an unreachable bot: answered from the help alone", async () => {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    jest.spyOn(global, "fetch").mockRejectedValue(new Error("connect refused"));
    const res = await askQ({ question: "what is a stop-loss", bot: "btc" });
    expect(res.status).toBe(200);
    expect(logged()[0].sources.some((s) => s.startsWith("journal:"))).toBe(false);
  });
});

describe("flag", () => {
  const answerId = async () => (await (await askQ({ question: "what is a stop-loss" })).json()).id;

  test("the person who asked flags their answer (204) and the log shows it", async () => {
    const id = await answerId();
    expect((await flagA({ id, note: "wrong section" })).status).toBe(204);
    expect(readAnswers(file).get(id).flag).toBe(true);
    const lines = fs.readFileSync(file, "utf8").trim().split("\n").map((l) => JSON.parse(l));
    expect(lines[1]).toMatchObject({ kind: "flag", id, user: "viewer", note: "wrong section" });
    // The answer line is never rewritten.
    expect(lines[0].flag).toBe(false);
  });

  test("another user may not flag it, a manager may", async () => {
    const id = await answerId();
    ({ cookie } = await signedIn("someone", "user"));
    expect((await flagA({ id })).status).toBe(403);
    ({ cookie } = await signedIn("boss", "manager"));
    expect((await flagA({ id })).status).toBe(204);
  });

  test.each([
    ["an unknown id", { id: "nope" }, 404],
    ["no id", {}, 400],
    ["a note too long", { id: "x", note: "a".repeat(501) }, 400],
  ])("%s → %i", async (_name, body, status) => {
    await answerId();
    expect((await flagA(body)).status).toBe(status);
  });
});
