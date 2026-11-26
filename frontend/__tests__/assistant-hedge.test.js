/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as ask } from "@/app/api/assistant/route";
import { goldenInputs } from "@/lib/assistant/goldenrun";
import { check } from "@/lib/assistant/guard";
import { readAnswers } from "@/lib/assistant/log";
import { REFUSAL } from "@/lib/assistant/phrases";
import { assistantIndex, botHedge, setAssistantModel } from "@/lib/assistant/server";
import { LIVE_CACHE_MS, LIVE_TOKENS, answerQuestion, asksAboutHedge, hedgePassage, journalPassages, passageTokens } from "@/lib/assistant/service";
import { hedgeOrderSentence } from "@/lib/hedgeWords";
import { signedIn, useTestAuth } from "@/test/auth";

// The assistant answers "what is the futures hedge doing, and why did it trade
// the perpetual?" from the bot's hedge now (#877): one live:<bot>/hedge
// passage from GET /api/hedge and the journal's hedge lines. The fixtures are
// the golden set's hedge cases: invented testnet figures. A fake model: no
// test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const byId = Object.fromEntries(golden.map((q) => [q.id, q]));
const HEDGE_IDS = ["hedge-off", "hedge-on", "hedge-unavailable", "hedge-unknown"];
const [off, on, unavailable, unknown] = HEDGE_IDS.map((id) => byId[id]);
const NOW = Date.parse("2026-11-20T12:00:00Z");

const passageOf = (q) => goldenInputs(q).hedge[0];
/** The on fixture's /api/hedge answer with its hedge view patched. */
const onRoute = (patch = {}) => ({ ...structuredClone(on.bot.hedge), hedge: { ...structuredClone(on.bot.hedge.hedge), ...patch } });

let dir, file;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-hedge-"));
  file = path.join(dir, "assistant.jsonl");
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
});

function fakeModel(reply) {
  const m = { model: "fake", calls: [], ask: async (args) => (m.calls.push(args), structuredClone(reply)) };
  return m;
}

async function askGolden(q, reply = q.bot.answer, question = q.question) {
  const model = fakeModel(reply);
  const out = await answerQuestion({ question, ...goldenInputs(q), user: { username: "viewer", role: "user" }, model, index: assistantIndex(), file });
  return { out, sent: model.calls[0]?.passages ?? [] };
}

describe("the hedge passage", () => {
  test("on with a perpetual open: state, side and size in % of margin, delta against its band, the newest order, the trial", () => {
    const p = passageOf(on);
    expect(p).toMatchObject({ id: "live:btc/hedge", source: "live:btc", live: true });
    for (const s of [
      "Hedge now (read 20 Nov 12:00 UTC):",
      "State: on: the GEX shed keeps its legs and the perpetual holds the book's coin delta inside its band. The experiment runs on testnet only.",
      "Perpetual: short, 3 % of the margin balance.",
      "Coin delta: +0.2 % of the margin balance, inside its ±5 % band. Hedge orders in the last hour: 2 of the 12 allowed.",
      "Newest hedge order (20 Nov 11:40 UTC): A sale of the perpetual to bring the book's coin delta back inside its band.",
      "Trial: 3 days measured since 17 Nov 2026: needs more days, 4 still missing of the 7 the comparison needs.",
      "Its risks: the perpetual's fees and funding, a hedge that cannot trade (the GEX shed then takes over), and at most 12 hedge orders an hour",
    ]) {
      expect(p.text).toContain(s);
    }
  });

  test("off: says so and holds no figure it invents", () => {
    const { text } = passageOf(off);
    expect(text).toContain("State: off: the GEX shed buys the threatened side back as usual, and the perpetual holds nothing for the book.");
    expect(text).toContain("Perpetual: none open.");
    expect(text).toContain("Coin delta: the hedge holds none while it is off.");
    expect(text).toContain("Newest hedge order: none in the journal of the last 7 days.");
    expect(text).toContain("Trial: 10 days measured since 10 Nov 2026; the weekly evidence report gives its verdict.");
    expect(text).not.toContain("%");
  });

  test("off with a perpetual left open: its side and size, said to be left open", () => {
    const route = onRoute({ enabled: false });
    expect(hedgePassage("btc", { route, nowMs: NOW }).text).toContain("Perpetual: short (left open with the hedge off), 3 % of the margin balance.");
  });

  test("unavailable: why, and the trial not started", () => {
    const { text } = passageOf(unavailable);
    expect(text).toContain("State: unavailable: this bot has no futures connection. The GEX shed protects the book as usual.");
    expect(text).toContain("Trial: not started: the hedge has never been switched on.");
  });

  test("Hedge now: unknown when the route did not answer", () => {
    expect(passageOf(unknown).text).toBe(
      "Hedge now: unknown: the bot's hedge route did not answer. Whether the hedge is on, and why it traded, cannot be said now.",
    );
  });

  test.each([
    ["halted", { halted: true, halt_reason: "the exchange refuses orders: locked_by_admin" }, "State: on, but it cannot trade now: the exchange refuses orders: locked_by_admin. The GEX shed protects the book meanwhile"],
    ["standing down", { stood_down: "the book holds calls only, so the hedge stands down and the book keeps its direction" }, "State: on, but standing down: the book holds calls only"],
    ["not holding", { not_holding: "3 cycles without a fill" }, "State: on, but not holding the delta: 3 cycles without a fill. The GEX shed is back on"],
    ["kill switch", { kill_switch: true }, "State: on, but the kill switch fired: the bot trades nothing until a person restarts it."],
    ["live account", { testnet: false }, "This is a live account: the experiment is testnet-only, so it cannot be switched on here."],
    ["outside the band", { net_delta_coin: -0.1 }, "Coin delta: −8 % of the margin balance, outside its ±5 % band. The next cycle that can trade brings it back with the perpetual."],
    ["margin unknown", { band_coin: 0 }, "Coin delta against its band: unknown: the margin balance was not read this cycle, so the hedge trades nothing."],
    ["margin unknown, the size too", { band_coin: 0 }, "Perpetual: short; its size as a share of the margin balance is unknown"],
    ["a long perpetual", { perp_usd: 1500, perp_delta_coin: 0.01875 }, "Perpetual: long, 1.5 % of the margin balance."],
    ["a week measured", { benchmark: { since: "2026-11-10T09:00:00Z" } }, "Trial: 10 days measured since 10 Nov 2026; the weekly evidence report gives its verdict."],
    ["switched on today", { benchmark: { since: "2026-11-20T09:00:00Z" } }, "Trial: 0 days measured since 20 Nov 2026: needs more days, 7 still missing"],
  ])("%s", (_, patch, want) => {
    expect(hedgePassage("btc", { route: onRoute(patch), nowMs: NOW }).text).toContain(want);
  });

  test("the newest hedge order is the newest fill or close of the journal, with its side; other hedge lines are not orders", () => {
    const items = [
      { seq: 10, at: "2026-11-20T08:00:00Z", event: "hedge", reason: "hedge_fill", side: "buy", plain: "Older." },
      { seq: 11, at: "2026-11-20T09:00:00Z", event: "hedge", reason: "hedge_closed", side: "buy", plain: "Newer." },
      { seq: 12, at: "2026-11-20T10:00:00Z", event: "hedge", reason: "hedge_unavailable", plain: "Not an order." },
      { seq: 13, at: "2026-11-20T11:00:00Z", event: "filled", reason: "entry", side: "sell", plain: "An option." },
    ];
    const { text } = hedgePassage("btc", { route: onRoute(), journal: journalPassages("btc", items), nowMs: NOW });
    expect(text).toContain(
      "Newest hedge order (20 Nov 09:00 UTC): A purchase of the perpetual to close the hedge: the experiment was switched off, or the kill switch closed what the bot held.",
    );
  });

  test("Scenario: a live bot — the passage holds no amount and no account detail", () => {
    const route = onRoute({ testnet: false, by: "admin1", perp_usd: -32310, perp_avg_price: 81234.5, unrealised_coin: 0.00731, funding_coin: 0.00012, realised_coin: 0.0042 });
    route.hedge.benchmark = { ...route.hedge.benchmark, advantage_coin: 0.0123, advantage_usd: 987.6, real_pnl_coin: 0.0042 };
    const journal = journalPassages("btc", on.bot.journal.items);
    const { text } = hedgePassage("btc", { route, journal, nowMs: NOW });
    for (const s of ["32310", "32,310", "81234", "0.00731", "0.00012", "0.0042", "0.0123", "987", "0.0375", "0.0625", "admin1", "USD", "BTC", "$"]) {
      expect([s, text.includes(s)]).toEqual([s, false]);
    }
  });

  test("the budget holds: the longest hedge passage takes at most a third of LIVE_TOKENS", () => {
    const p = hedgePassage("btc", { route: onRoute({ halted: true, halt_reason: "x".repeat(200), testnet: false }), journal: journalPassages("btc", on.bot.journal.items), nowMs: NOW });
    expect(passageTokens(p)).toBeLessThanOrEqual(LIVE_TOKENS / 3);
  });

  test("hedgeOrderSentence: a change that sends no order has none", () => {
    expect(hedgeOrderSentence("hedge_unavailable", "sell")).toBeNull();
    expect(hedgeOrderSentence("hedge_fill", "")).toBe("An order of the perpetual to bring the book's coin delta back inside its band.");
  });
});

describe("Scenario: golden set", () => {
  test("four hedge cases: off, on, unavailable, unknown", () => {
    expect(golden.filter((q) => q.category === "hedge").map((q) => q.id)).toEqual(HEDGE_IDS);
  });

  test("every golden hedge answer passes the guard on the passages built from its fixture, citing live:btc/hedge", () => {
    for (const q of golden.filter((x) => x.category === "hedge")) {
      const { live, journal, hedge } = goldenInputs(q);
      expect(q.bot.answer.citations).toContain("live:btc/hedge");
      expect([q.id, check(q.bot.answer, [...live, ...hedge, ...journal])]).toEqual([q.id, { ok: true, reasons: [], details: [] }]);
    }
  });
});

describe("answers from the hedge", () => {
  test("Scenario: hedge on with a perpetual open — cites live:btc/hedge, names side, size, delta against its band and the last order", async () => {
    const { out, sent } = await askGolden(on);
    expect(sent.map((p) => p.id)).toContain("live:btc/hedge");
    expect(out.body).toMatchObject({ outcome: "answered", answer: on.bot.answer.text });
    for (const s of ["short", "3 % of the margin balance", "inside its ±5 % band", "a sale of the perpetual"]) expect(out.body.answer).toContain(s);
    expect(out.body.citations.map((c) => c.id)).toEqual(["live:btc/hedge"]);
  });

  test("Scenario: hedge off — says it is off; a figure the passage does not hold is never shown", async () => {
    const { out } = await askGolden(off);
    expect(out.body).toMatchObject({ outcome: "answered", answer: off.bot.answer.text });
    const invented = await askGolden(off, { text: "The hedge is off; the perpetual is 2 % of the margin balance.", citations: ["live:btc/hedge"] });
    expect(invented.out.body.outcome).toBe("rejected");
  });

  test("Scenario: the hedge route does not answer — says it is unknown and why", async () => {
    const { out, sent } = await askGolden(unknown);
    expect(sent.map((p) => p.id)).toContain("live:btc/hedge");
    expect(out.body.outcome).toBe("answered");
    for (const s of ["unknown", "did not answer"]) expect(out.body.answer).toContain(s);
  });

  test("Scenario: a question not about the hedge — no hedge passage is sent", async () => {
    const { sent } = await askGolden(on, { text: "x", citations: [] }, "What is a delta exit?");
    expect(sent.length).toBeGreaterThan(0);
    expect(sent.map((p) => p.id)).not.toContain("live:btc/hedge");
  });

  test("Scenario: should I turn the hedge on? — refused, with the trial's days and risks beside it, no predicted return", async () => {
    const q = byId["adv-hedge-turn-on"];
    expect(q).toMatchObject({ category: "refuse_advice", expected_behaviour: "refuse" });
    const { out: refused, sent } = await askGolden(off, { text: REFUSAL, citations: [] }, q.question);
    expect(refused.body).toMatchObject({ outcome: "refused", answer: REFUSAL });
    // The passage sent beside the question holds the trial's days and its risks, never a return.
    const passage = sent.find((p) => p.id === "live:btc/hedge");
    expect(passage.text).toContain("Trial: 10 days measured");
    expect(passage.text).toContain("Its risks:");
    expect(passage.text).not.toMatch(/\breturn\b|\bwill (earn|make)\b/i);
    // Facts with the advice declined are an answer; advice itself is never shown.
    const facts = { text: `The trial has run 10 days measured since 10 Nov 2026; its risks include the perpetual's fees and funding. ${REFUSAL}`, citations: ["live:btc/hedge"] };
    expect((await askGolden(off, facts, q.question)).out.body.outcome).toBe("answered");
    const advice = { text: "You should turn the hedge on: the trial looks good.", citations: ["live:btc/hedge"] };
    expect((await askGolden(off, advice, q.question)).out.body.outcome).toBe("rejected");
  });
});

describe("asksAboutHedge", () => {
  test.each([
    ["What is the hedge doing?", true],
    ["Why did it sell the perp?", true],
    ["Why did it buy the perpetual?", true],
    ["Is the futures hedge on?", true],
    ["Is it hedging right now?", true],
    ["Should I turn the hedge on?", true],
    ["What is a delta exit?", false],
    ["Why is nothing open?", false],
    ["How did I do this month?", false],
  ])("%s → %s", (q, want) => {
    expect(asksAboutHedge(q)).toBe(want);
  });
});

describe("POST /api/assistant: the hedge read", () => {
  let cookie, warn, fetchSpy;
  beforeEach(async () => {
    process.env.ASSISTANT_LOG_PATH = file;
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    warn = jest.spyOn(console, "error").mockImplementation(() => {});
    jest.spyOn(console, "log").mockImplementation(() => {});
    useTestAuth();
    ({ cookie } = await signedIn("viewer", "user"));
  });
  afterEach(() => {
    setAssistantModel(undefined);
    delete process.env.ASSISTANT_LOG_PATH;
    delete process.env.BOT_APIS;
  });

  const serve = (q, { hedgeDown = false } = {}) =>
    (fetchSpy = jest.spyOn(global, "fetch").mockImplementation(async (url) => {
      const u = new URL(url);
      if (u.pathname === "/api/journal") return Response.json(q.bot.journal);
      if (u.pathname === "/api/status") return Response.json(q.bot.status);
      if (u.pathname === "/api/positions") return Response.json(q.bot.positions);
      if (u.pathname === "/api/hedge") return hedgeDown ? new Response("not found", { status: 404 }) : Response.json(q.bot.hedge);
      return Response.json({ strangles: [] });
    }));
  const askQ = async (question) => {
    const res = await ask(
      new Request("http://monitor/api/assistant", {
        method: "POST",
        headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie },
        body: JSON.stringify({ question, bot: "btc" }),
      }),
    );
    return res.json();
  };
  const hedgeReads = () => fetchSpy.mock.calls.filter(([u]) => u.endsWith("/api/hedge")).length;
  const lastLine = () => [...readAnswers(file).values()].at(-1);

  test("a hedge question sends the hedge passage, citing live:btc/hedge; /api/hedge is read once and cached", async () => {
    setAssistantModel(fakeModel({ text: "The futures hedge is on.", citations: ["live:btc/hedge"] }));
    serve(on);
    const body = await askQ("What is the hedge doing?");
    expect(body).toMatchObject({ outcome: "answered" });
    expect(body.citations.map((c) => c.id)).toEqual(["live:btc/hedge"]);
    expect(lastLine().sources).toContain("live:btc/hedge");
    await askQ("Why did it sell the perpetual?");
    expect(hedgeReads()).toBe(1);
  });

  test("other questions do not pay: no /api/hedge read, no hedge source", async () => {
    setAssistantModel(fakeModel({ text: "x", citations: [] }));
    serve(on);
    await askQ("What is a delta exit?");
    expect(hedgeReads()).toBe(0);
    expect(lastLine().sources).not.toContain("live:btc/hedge");
  });

  test("Scenario: the hedge route does not answer — the unknown passage is sent and cited, a WARN logged, nothing cached", async () => {
    setAssistantModel(fakeModel(unknown.bot.answer));
    serve(on, { hedgeDown: true });
    const body = await askQ(unknown.question);
    expect(body).toMatchObject({ outcome: "answered", answer: unknown.bot.answer.text });
    expect(body.citations.map((c) => c.id)).toEqual(["live:btc/hedge"]);
    expect(warn.mock.calls.some(([l]) => l.includes('"level":"WARN"') && l.includes("bot hedge unavailable"))).toBe(true);
    await askQ(unknown.question);
    expect(hedgeReads()).toBe(2);
  });

  test("botHedge: the cache holds LIVE_CACHE_MS; a bot that is not configured is null", async () => {
    serve(on);
    await botHedge("btc", [], NOW);
    await botHedge("btc", [], NOW + LIVE_CACHE_MS - 1);
    expect(hedgeReads()).toBe(1);
    await botHedge("btc", [], NOW + LIVE_CACHE_MS);
    expect(hedgeReads()).toBe(2);
    expect(await botHedge("doge", [])).toBeNull();
  });
});
