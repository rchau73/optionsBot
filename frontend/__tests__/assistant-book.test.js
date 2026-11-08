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
import { assistantIndex, botBook, setAssistantModel } from "@/lib/assistant/server";
import { answerQuestion, asksAboutIdle, bookPassage, journalPassages } from "@/lib/assistant/service";
import { signedIn, useTestAuth } from "@/test/auth";

// The assistant answers "why is nothing open / why is my margin unused?" from
// the bot's book now (#719): one live:<bot>/book passage from /api/status,
// /api/positions and the journal's latest skip per vacant slot. The fixtures
// are the golden set's idle cases: invented testnet figures. A fake model: no
// test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const byId = Object.fromEntries(golden.map((q) => [q.id, q]));
const idle = golden.filter((q) => q.category === "idle");
const IDLE_IDS = ["idle-gex-wait", "idle-margin-frozen", "idle-slots-full", "idle-status-unknown"];
const [waiting, frozen, full, unknown] = IDLE_IDS.map((id) => byId[id]);

const bookOf = (q) => goldenInputs(q).book[0];

let dir, file;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-book-"));
  file = path.join(dir, "assistant.jsonl");
});
afterEach(() => {
  jest.restoreAllMocks();
  fs.rmSync(dir, { recursive: true, force: true });
});

/** A model that always replies `reply` and records what it was sent. */
function fakeModel(reply) {
  const m = { model: "fake", calls: [], ask: async (args) => (m.calls.push(args), structuredClone(reply)) };
  return m;
}

/** One golden idle question through answerQuestion with its fixture's inputs. */
async function askGolden(q, reply = q.bot.answer, question = q.question) {
  const model = fakeModel(reply);
  const out = await answerQuestion({ question, ...goldenInputs(q), user: { username: "viewer", role: "user" }, model, index: assistantIndex(), file });
  return { out, sent: (model.calls[0]?.passages ?? []).map((p) => p.id) };
}

describe("the book passage", () => {
  test("waiting for GEX: entries wait for the first fresh reading, every slot vacant with its latest skip", () => {
    const p = bookOf(waiting);
    expect(p).toMatchObject({ id: "live:btc/book", source: "live:btc", live: true });
    for (const s of [
      "Book now (8 Nov 10:00 UTC):",
      "Entries: waiting for the first fresh GEX reading: the bot sells nothing before it knows the market's gamma regime and trend.",
      "IM 0 % of margin balance, limit 20 %.",
      "Slots: 0 of 4 hold an open strangle, 4 vacant. Vacant slot",
      "Vacant slot 30-day 0.1 delta: latest skip (8 Nov 09:59 UTC): No new risk yet: the market makers' positioning data (GEX) is missing or out of date, so the bot waits for a fresh reading.",
      "Vacant slot 30-day 0.16 delta: no skip for it in the journal of the last 7 days.",
    ]) {
      expect(p.text).toContain(s);
    }
  });

  test("frozen: the freeze in the monitor's words with the policy's reason, IM and limit as percentages", () => {
    const { text } = bookOf(frozen);
    expect(text).toContain(
      "Entries: frozen by the margin policy: the expected swing changed and must hold for a few days " +
        "(DVOL moved to band ≥50 (IM 15%) (IV percentile 55), not yet confirmed). Exits, rolls and repairs continue.",
    );
    expect(text).toContain("IM 8.5 % of margin balance, limit 20 %.");
    expect(text).toContain("Slots: 1 of 4 hold an open strangle, 3 vacant.");
  });

  test("fully used: every slot holds a strangle and IM sits near its limit", () => {
    const { text } = bookOf(full);
    expect(text).toContain("Entries: allowed.");
    expect(text).toContain("IM 19.4 % of margin balance, limit 20 %.");
    expect(text).toContain("Slots: all 4 hold an open strangle, so the bot opens a new one only when one of them frees.");
  });

  test("data missing: the status not read makes the whole book unknown, with why", () => {
    expect(bookOf(unknown).text).toBe(
      "Book now: unknown: the bot's status route did not answer. Why nothing is open, or why the margin is unused, cannot be said now.",
    );
  });

  test.each([
    ["halted", { halted: true }, "Entries: stopped: the kill switch fired"],
    ["a stale GEX reading", { gex_as_of: "2026-11-08T09:50:00Z" }, "Entries: waiting for a fresh GEX reading (the last one was read 10m ago, more than 5 minutes)"],
    ["margin unknown", { risk: { error: "account summary is 3 cycles old" } }, "Entries: blocked: the margin figures are unknown this cycle (account summary is 3 cycles old)"],
    ["entries held otherwise", { risk: { im_pct: 5, status: { limit_im_pct: 20 }, entries_allowed: false } }, "Entries: held this cycle; each vacant slot's latest reason is below."],
    ["no entry state", { risk: { im_pct: 5, status: { limit_im_pct: 20 } } }, "Entries: unknown: the status does not say."],
    ["no slots", { pnl: [] }, "Slots: unknown: the status lists no slot."],
  ])("%s", (_, patch, want) => {
    const status = { ...structuredClone(full.bot.status), ...patch };
    expect(bookPassage("btc", { status, positions: full.bot.positions }).text).toContain(want);
  });

  test("positions not read: the count from the status, which slots are vacant unknown", () => {
    const { text } = bookPassage("btc", { status: frozen.bot.status });
    expect(text).toContain("Slots: 1 open strangles for 4 slots; which slots are vacant is unknown: the positions route did not answer.");
  });

  test("a vacant slot's latest skip is the newest of its journal lines; a held repair says why", () => {
    const items = [
      { seq: 10, at: "2026-11-08T08:00:00Z", event: "skipped", reason: "no_strike", slot: { dte: 30, delta: 0.1 }, plain: "Older skip." },
      { seq: 11, at: "2026-11-08T09:00:00Z", event: "skipped", reason: "no_free_expiry", slot: { dte: 30, delta: 0.1 }, plain: "Newer skip." },
      { seq: 12, at: "2026-11-08T09:30:00Z", event: "filled", reason: "entry", slot: { dte: 30, delta: 0.1 }, plain: "Not a skip." },
    ];
    const status = {
      ...structuredClone(frozen.bot.status),
      held_legs: [
        { strangle_id: "s1", slot: { dte: 50, delta: 0.16 }, missing: "call", rule: "cooldown", reason: "the call was stopped 20 h ago: cooldown 72 h" },
        { strangle_id: "s9", slot: { dte: 50, delta: 0.1 }, missing: "put", rule: "", reason: "" },
      ],
    };
    const { text } = bookPassage("btc", { status, positions: frozen.bot.positions, journal: journalPassages("btc", items) });
    expect(text).toContain("Vacant slot 30-day 0.1 delta: latest skip (8 Nov 09:00 UTC): Newer skip.");
    expect(text).not.toContain("Older skip");
    expect(text).toContain("One-legged strangle in slot 50-day 0.16 delta: its call is not sold again yet: the call was stopped 20 h ago: cooldown 72 h.");
    expect(text).not.toContain("50-day 0.1 delta: its put");
  });

  test("the 7-day use and the alerts as percentages", () => {
    const status = { ...structuredClone(frozen.bot.status), utilisation_7d: { mean_pct: 41.6, allowed_share: 0.62 } };
    status.risk = { ...status.risk, im_under_use: true };
    const { text } = bookPassage("btc", { status, positions: frozen.bot.positions });
    expect(text).toContain("IM has stayed well under its limit while entries were allowed (the under-use alert).");
    expect(text).toContain("Over the last 7 days IM averaged 42 % of its limit while entries were allowed, which they were 62 % of the time.");
  });

  test("no amount or account detail reaches the passage", () => {
    const status = structuredClone(frozen.bot.status);
    status.risk = { ...status.risk, margin_balance: 1.23456, equity: 1.98765 };
    status.account = { equity: 1.98765, margin_used: 0.10493 };
    status.pnl = status.pnl.map((r) => ({ ...r, realised: 0.00731, total: 0.00731 }));
    const { text } = bookPassage("btc", { status, positions: frozen.bot.positions });
    for (const amount of ["1.23456", "1.98765", "0.10493", "0.00731", "BTC"]) expect(text).not.toContain(amount);
  });
});

describe("Scenario: golden set", () => {
  test("four idle cases: idle waiting, frozen, fully used, unknown", () => {
    expect(idle.map((q) => q.id)).toEqual(IDLE_IDS);
  });

  test("every golden idle answer passes the guard on the passages built from its fixture, citing live:btc/book", () => {
    for (const q of idle) {
      const { live, journal, book } = goldenInputs(q);
      expect(q.bot.answer.citations).toContain("live:btc/book");
      expect([q.id, check(q.bot.answer, [...live, ...book, ...journal])]).toEqual([q.id, { ok: true, reasons: [], details: [] }]);
    }
  });
});

describe("answers from the book", () => {
  test("Scenario: entries waiting for fresh GEX — says it waits for a fresh reading, cites live:btc/book", async () => {
    const { out, sent } = await askGolden(waiting);
    expect(sent).toContain("live:btc/book");
    expect(out.body).toMatchObject({ outcome: "answered", answer: waiting.bot.answer.text });
    expect(out.body.answer).toContain("fresh GEX reading");
    expect(out.body.citations.map((c) => c.id)).toEqual(["live:btc/book"]);
  });

  test("Scenario: margin frozen — names the freeze with the IM and limit percentages from the passage", async () => {
    const { out } = await askGolden(frozen);
    expect(out.body.outcome).toBe("answered");
    for (const s of ["frozen", "8.5 %", "20 %"]) expect(out.body.answer).toContain(s);
    expect(out.body.citations.map((c) => c.id)).toEqual(["live:btc/book"]);
  });

  test("Scenario: margin frozen — a percentage the passage does not hold is never shown", async () => {
    const reply = { text: "New risk is frozen; IM is 12 % of a 20 % limit.", citations: ["live:btc/book"] };
    const { out } = await askGolden(frozen, reply);
    expect(out.body.outcome).toBe("rejected");
  });

  test("Scenario: fully used — says the slots and margin are in use, suggests raising no limit", async () => {
    const { out } = await askGolden(full);
    expect(out.body.outcome).toBe("answered");
    expect(out.body.answer).toContain("in use");
    expect(out.body.answer.toLowerCase()).not.toMatch(/\braise\b|increase the limit/);
  });

  test("Scenario: fully used — an answer advising a higher limit is never shown", async () => {
    const reply = { text: "All 4 slots are in use; you should raise the margin limit to trade more.", citations: ["live:btc/book"] };
    const { out } = await askGolden(full, reply);
    expect(out.body.outcome).toBe("rejected");
  });

  test("Scenario: data missing — says the reason is unknown and why, invents none", async () => {
    const { out, sent } = await askGolden(unknown);
    expect(sent).toContain("live:btc/book");
    expect(out.body.outcome).toBe("answered");
    expect(out.body.answer).toContain("unknown");
    expect(out.body.answer).toContain("did not answer");
  });

  test("Scenario: advice still refused — the canonical Refusal", async () => {
    const { out } = await askGolden(full, { text: REFUSAL, citations: [] }, "Should I raise the margin limit so it trades more?");
    expect(out.body).toMatchObject({ outcome: "refused", answer: REFUSAL });
  });

  test("other questions do not grow: no book passage for a question about a rule", async () => {
    const { sent } = await askGolden(full, { text: "x", citations: [] }, "What is a delta exit?");
    expect(sent.length).toBeGreaterThan(0);
    expect(sent).not.toContain("live:btc/book");
  });
});

describe("asksAboutIdle", () => {
  test.each([
    ["Why is nothing open right now?", true],
    ["Why is my margin not being used?", true],
    ["Why isn't it opening more trades?", true],
    ["Why is nothing open?", true],
    ["Why is the bot not trading?", true],
    ["Why are there no positions?", true],
    ["Why so much unused margin?", true],
    ["Why is the bot waiting?", true],
    ["What is a delta exit?", false],
    ["How did I do this month?", false],
    ["Why did the bot close my put?", false],
  ])("%s → %s", (q, want) => {
    expect(asksAboutIdle(q)).toBe(want);
  });
});

describe("POST /api/assistant: the book read", () => {
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

  const serve = (q, { statusDown = false } = {}) =>
    (fetchSpy = jest.spyOn(global, "fetch").mockImplementation(async (url) => {
      const u = new URL(url);
      if (u.pathname === "/api/journal") return Response.json(q.bot.journal);
      if (u.pathname === "/api/status") {
        if (statusDown) return new Response("down", { status: 503 });
        return Response.json(q.bot.status);
      }
      if (u.pathname === "/api/positions") return Response.json(q.bot.positions);
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
  const statusReads = () => fetchSpy.mock.calls.filter(([u]) => u.endsWith("/api/status")).length;
  const lastLine = () => [...readAnswers(file).values()].at(-1);

  test("an idle question sends the book, citing live:btc/book; status is read once for the live data and the book", async () => {
    setAssistantModel(fakeModel(waiting.bot.answer));
    serve(waiting);
    const body = await askQ(waiting.question);
    expect(body).toMatchObject({ outcome: "answered", answer: waiting.bot.answer.text });
    expect(body.citations.map((c) => c.id)).toEqual(["live:btc/book"]);
    expect(statusReads()).toBe(1);
    expect(lastLine().sources).toContain("live:btc/book");
  });

  test("other questions do not pay: no book source", async () => {
    setAssistantModel(fakeModel({ text: "x", citations: [] }));
    serve(full);
    await askQ("What is a delta exit?");
    expect(lastLine().sources).not.toContain("live:btc/book");
  });

  test("Scenario: data missing — the status route down: the unknown book is sent and cited, a WARN logged", async () => {
    setAssistantModel(fakeModel(unknown.bot.answer));
    serve(unknown, { statusDown: true });
    const body = await askQ(unknown.question);
    expect(body).toMatchObject({ outcome: "answered", answer: unknown.bot.answer.text });
    expect(body.citations.map((c) => c.id)).toEqual(["live:btc/book"]);
    expect(warn.mock.calls.some(([l]) => l.includes('"level":"WARN"') && l.includes("bot live data unavailable"))).toBe(true);
  });

  test("botBook: a bot that is not configured is null", async () => {
    expect(await botBook("doge", [])).toBeNull();
  });
});
