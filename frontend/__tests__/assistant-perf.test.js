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
import { FALLBACK_REPLY, REFUSAL } from "@/lib/assistant/phrases";
import { assistantIndex, botPerformance, setAssistantModel } from "@/lib/assistant/server";
import { PERF_CACHE_MS, PERF_TOKENS, answerQuestion, asksAboutResults, journalPassages, passageTokens, perfPassages } from "@/lib/assistant/service";
import { signedIn, useTestAuth } from "@/test/auth";

// The assistant answers "how did I do this week / month?" from the bot's
// /api/performance figures (#648). The fixtures are the golden set's results
// questions: invented testnet figures. A fake model: no test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const byId = Object.fromEntries(golden.map((q) => [q.id, q]));
const results = golden.filter((q) => q.category === "results");
const month = byId["res-month"];
const fixture = month.bot;
const perf7 = fixture.perf["7d"];
const perf30 = fixture.perf["30d"];
const passage = (ps, id) => ps.find((p) => p.id === id);

let dir, file;
beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-perf-"));
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

const askService = (question, reply, perf = perfPassages("btc", perf7, perf30)) => {
  const model = fakeModel(reply);
  return answerQuestion({
    question,
    bot: "btc",
    journal: journalPassages("btc", fixture.journal.items),
    live: [],
    perf,
    user: { username: "viewer", role: "user" },
    model,
    index: assistantIndex(),
    file,
  }).then((out) => ({ out, model, sent: (model.calls[0]?.passages ?? []).map((p) => p.id) }));
};

describe("perf passages", () => {
  test("one passage per window, the route's own figures signed and with their unit", () => {
    const ps = perfPassages("btc", perf7, perf30);
    expect(ps.map((p) => p.id)).toEqual(["perf:btc/7d", "perf:btc/30d"]);
    expect(ps.every((p) => p.source === "perf:btc")).toBe(true);
    expect(passage(ps, "perf:btc/30d").text).toBe(
      "Last 30 days (6 Oct 2026 to 5 Nov 2026), testnet figures, past results, not a forecast. " +
        "Net P&L after fees: +0.01200 BTC (+$756 at the window's last spot). " +
        "Return on equity: +1.2% (equity 1.00000 BTC at the start). " +
        "Holding the coin over the same window: +5.0% in USD, 0.0% in BTC (a held coin stays one coin). " +
        "The bot's return in USD: +6.3%, +1.3 percentage points against holding. " +
        "Max drawdown: −0.00800 BTC (−0.8% of equity). " +
        "Worst week: 27 Oct 2026 to 3 Nov 2026, −0.00600 BTC (−0.6% of equity). " +
        "Days with no P&L history: 1 of 31 (2026-10-14), not filled in.",
    );
  });

  test("a live bot's figures say live; a window not read is left out", () => {
    const ps = perfPassages("btc", null, { ...perf30, label: "live" });
    expect(ps.map((p) => p.id)).toEqual(["perf:btc/30d"]);
    expect(ps[0].text).toContain("live figures, past results");
    expect(perfPassages("btc", null, null)).toEqual([]);
  });

  test("the budget holds: both windows, a month of missing days listed, stay under PERF_TOKENS", () => {
    const days = Array.from({ length: 31 }, (_, i) => `2026-10-${String((i % 30) + 1).padStart(2, "0")}`);
    const ps = perfPassages("btc", { ...perf7, missing_days: days.slice(0, 8) }, { ...perf30, missing_days: days });
    expect(ps.reduce((n, p) => n + passageTokens(p), 0)).toBeLessThanOrEqual(PERF_TOKENS);
  });

  test("every golden results answer passes the guard on the passages built from its fixture", () => {
    for (const q of results) {
      const { live, journal, perf } = goldenInputs(q);
      expect([q.id, check(q.bot.answer, [...live, ...journal, ...perf])]).toEqual([q.id, { ok: true, reasons: [], details: [] }]);
    }
  });
});

describe("Scenario: A month question gets the month's numbers with their source", () => {
  test("the answer states net P&L in coin and USD, net of fees, and the hold-the-coin comparison, citing perf:btc/30d", async () => {
    const { out, sent } = await askService(month.question, fixture.answer);
    expect(sent).toEqual(expect.arrayContaining(["perf:btc/7d", "perf:btc/30d"]));
    expect(out.body.outcome).toBe("answered");
    expect(out.body.answer).toBe(fixture.answer.text);
    for (const s of ["+0.01200 BTC", "+$756", "net P&L after fees", "Holding the coin", "+5.0% in USD"]) expect(out.body.answer).toContain(s);
    expect(out.body.citations.map((c) => c.id)).toEqual(["perf:btc/30d"]);
    // Every number in it is in the cited passage.
    expect(check(fixture.answer, perfPassages("btc", perf7, perf30)).ok).toBe(true);
  });

  test("a number from another window is not grounded in the one cited", () => {
    const v = check({ text: "The net P&L after fees was −0.00400 BTC.", citations: ["perf:btc/30d"] }, perfPassages("btc", perf7, perf30));
    expect(v.details).toContain("ungrounded_number: 0.00400");
  });
});

describe("Scenario: A loss is as visible as a gain", () => {
  test("the 7-day loss carries its minus sign and unit, in the same words as a gain", () => {
    const loss = passage(perfPassages("btc", perf7, null), "perf:btc/7d").text;
    expect(loss).toContain("Net P&L after fees: −0.00400 BTC (−$252 at the window's last spot).");
    expect(loss).toContain("Return on equity: −0.4%");
    const gain = passage(perfPassages("btc", { ...perf7, net_pnl: 0.004, net_pnl_usd: 252, return_pct: 0.4 }, null), "perf:btc/7d").text;
    expect(gain).toContain("Net P&L after fees: +0.00400 BTC (+$252 at the window's last spot).");
    // The same sentence, only the signs differ.
    const head = (t) => t.split(" Holding")[0];
    expect(head(loss).replaceAll("−", "+")).toBe(head(gain));
  });

  test('"Am I up or down this week?" is a results question: the 7-day passage is sent', async () => {
    expect(asksAboutResults("Am I up or down this week?")).toBe(true);
    const reply = { text: "Over the last 7 days the net P&L after fees was −0.00400 BTC, on testnet.", citations: ["perf:btc/7d"] };
    const { out, sent } = await askService("Am I up or down this week?", reply);
    expect(sent).toContain("perf:btc/7d");
    expect(out.body).toMatchObject({ outcome: "answered", answer: reply.text });
  });
});

describe("Scenario: Unknown figures are not guessed", () => {
  const noEquity = {
    ...perf30,
    equity: null,
    equity_known: false,
    equity_reason: "no P&L history line in the window carries Deribit's margin balance",
    return_pct: null,
    return_usd_pct: null,
    excess_return_pct: null,
    max_drawdown: { coin: -0.008, pct: null },
    worst_week: { ...perf30.worst_week, pct: null },
  };

  test("the return reads unknown with the route's reason; nothing is computed", () => {
    const text = passage(perfPassages("btc", null, noEquity), "perf:btc/30d").text;
    expect(text).toContain("Return on equity: unknown: no P&L history line in the window carries Deribit's margin balance.");
    expect(text).toContain("The bot's return in USD: unknown: no P&L history line");
    expect(text).toContain("Max drawdown: −0.00800 BTC (% of equity unknown: no P&L history line");
    expect(text).not.toMatch(/1\.2%/);
  });

  test("a worst week's % carries the route's pct_note; no history and no spot read unknown", () => {
    const note = { ...perf30, worst_week: { ...perf30.worst_week, pct: null, pct_note: "the week began before the first balance" } };
    expect(perfPassages("btc", null, note)[0].text).toContain("(% of equity unknown: the week began before the first balance)");
    const empty = perfPassages("btc", { ...perf7, net_pnl_known: false, net_pnl: 0, net_pnl_usd: null, hold_known: false, worst_week: { known: false } }, null)[0].text;
    expect(empty).toContain("Net P&L after fees: unknown: no P&L history point in the window.");
    expect(empty).toContain("Holding the coin over the same window: unknown");
    expect(empty).toContain("Worst week: unknown: the window does not hold 7 days of P&L history yet.");
    expect(empty).not.toContain("+0.00000");
  });

  test('"What was my return this month?": a return the model computes itself is never shown', async () => {
    // +1.5 %: net P&L ÷ a guessed equity, in no passage (1.2 % would read as the passage's 0.012, as 0.30 = 30 %).
    const reply = { text: "Your return this month was +1.5%.", citations: ["perf:btc/30d"] };
    const { out, model } = await askService("What was my return this month?", reply, perfPassages("btc", null, noEquity));
    expect(out.body).toMatchObject({ outcome: "rejected", answer: FALLBACK_REPLY });
    expect(model.calls).toHaveLength(2); // asked once more, still ungrounded
    const unknown = { text: "Your return this month is unknown: no P&L history line in the window carries Deribit's margin balance.", citations: ["perf:btc/30d"] };
    expect((await askService("What was my return this month?", unknown, perfPassages("btc", null, noEquity))).out.body.outcome).toBe("answered");
  });
});

describe("Scenario: A forecast is refused", () => {
  const question = byId["fct-make-money-next-month"].question;

  test("the refusal sentence, with no number about the future; the past figures are not sent", async () => {
    expect(asksAboutResults(question)).toBe(false);
    const { out, sent } = await askService(question, { text: REFUSAL, citations: [] });
    expect(out.body).toMatchObject({ outcome: "refused", answer: REFUSAL });
    expect(sent.some((id) => id.startsWith("perf:"))).toBe(false);
  });

  test("a forecast built on the past figures is never shown, nor retried", async () => {
    const { out, model } = await askService(question, { text: "You will make about +0.01200 BTC next month.", citations: ["perf:btc/30d"] });
    expect(out.body).toMatchObject({ outcome: "rejected", answer: FALLBACK_REPLY });
    expect(model.calls).toHaveLength(1);
  });
});

describe("Scenario: Other questions do not pay for the extra sources", () => {
  test.each([
    ["What is a delta exit?", false],
    ["Will I make money next month?", false],
    ["How did I do this month?", true],
    ["What was my worst week?", true],
    ["Did I beat just holding my coin?", true],
    ["Why is my result negative this week?", true],
    ["What is my drawdown over the last 30 days?", true],
    ["How much did I make net of fees?", true],
  ])("%s → results question %s", (q, want) => {
    expect(asksAboutResults(q)).toBe(want);
  });

  test('"What is a delta exit?": no perf passage is sent to the model', async () => {
    const { sent } = await askService("What is a delta exit?", { text: "x", citations: [] });
    expect(sent.length).toBeGreaterThan(0);
    expect(sent.some((id) => id.startsWith("perf:"))).toBe(false);
  });
});

describe("botPerformance", () => {
  const NOW = Date.parse("2026-11-05T12:00:00Z");
  let warn, fetchSpy;
  beforeEach(() => {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    setAssistantModel(undefined); // a fresh cache
    warn = jest.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => delete process.env.BOT_APIS);

  const serve = (down = false) =>
    (fetchSpy = jest.spyOn(global, "fetch").mockImplementation(async (url) => {
      const u = new URL(url);
      if (down) throw new Error("connect refused");
      return Response.json(u.searchParams.get("range") === "7d" ? perf7 : perf30);
    }));

  test("reads both windows together, kept 10 s", async () => {
    serve();
    const first = await botPerformance("btc", NOW);
    expect(first.map((p) => p.id)).toEqual(["perf:btc/7d", "perf:btc/30d"]);
    expect(fetchSpy.mock.calls.map(([u]) => u).sort()).toEqual([
      "http://bot-btc:8081/api/performance?range=30d",
      "http://bot-btc:8081/api/performance?range=7d",
    ]);
    expect(await botPerformance("btc", NOW + PERF_CACHE_MS - 1)).toBe(first);
    expect(fetchSpy).toHaveBeenCalledTimes(2);
    await botPerformance("btc", NOW + PERF_CACHE_MS);
    expect(fetchSpy).toHaveBeenCalledTimes(4);
  });

  test("the route down → [] with a WARN, not cached", async () => {
    serve(true);
    expect(await botPerformance("btc", NOW)).toEqual([]);
    expect(warn.mock.calls.some(([l]) => l.includes('"level":"WARN"') && l.includes("bot performance unavailable"))).toBe(true);
    await botPerformance("btc", NOW + 1);
    expect(fetchSpy).toHaveBeenCalledTimes(4);
  });

  test("a bot that is not configured is null", async () => {
    expect(await botPerformance("doge", NOW)).toBeNull();
  });
});

describe("POST /api/assistant with a bot", () => {
  let cookie, warn, fetchSpy, model;
  beforeEach(async () => {
    process.env.ASSISTANT_LOG_PATH = file;
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    warn = jest.spyOn(console, "error").mockImplementation(() => {});
    jest.spyOn(console, "log").mockImplementation(() => {});
    useTestAuth();
    ({ cookie } = await signedIn("viewer", "user"));
    model = fakeModel(fixture.answer);
    setAssistantModel(model);
  });
  afterEach(() => {
    setAssistantModel(undefined);
    delete process.env.ASSISTANT_LOG_PATH;
    delete process.env.BOT_APIS;
  });

  const serve = ({ perfDown = false } = {}) =>
    (fetchSpy = jest.spyOn(global, "fetch").mockImplementation(async (url) => {
      const u = new URL(url);
      if (u.pathname === "/api/performance") {
        if (perfDown) throw new Error("connect refused");
        return Response.json(u.searchParams.get("range") === "7d" ? perf7 : perf30);
      }
      if (u.pathname === "/api/journal") return Response.json(fixture.journal);
      if (u.pathname === "/api/status") return Response.json(fixture.status);
      if (u.pathname === "/api/positions") return Response.json({ underlying: "BTC", strangles: [] });
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
  const perfReads = () => fetchSpy.mock.calls.filter(([u]) => u.includes("/api/performance")).length;
  const lastLine = () => [...readAnswers(file).values()].at(-1);

  test("a month question reads the performance and cites perf:btc/30d; the sources log names it", async () => {
    serve();
    const body = await askQ(month.question);
    expect(body).toMatchObject({ outcome: "answered", answer: fixture.answer.text });
    expect(body.citations.map((c) => c.id)).toEqual(["perf:btc/30d"]);
    expect(perfReads()).toBe(2);
    expect(lastLine().sources).toEqual(expect.arrayContaining(["perf:btc/7d", "perf:btc/30d"]));
  });

  test("Scenario: Other questions do not pay — no performance read, no perf source", async () => {
    serve();
    await askQ("What is a delta exit?");
    expect(perfReads()).toBe(0);
    expect(lastLine().sources.some((id) => id.startsWith("perf:"))).toBe(false);
  });

  test("Scenario: The performance route is down — a fallback that names no document, a WARN, not cached", async () => {
    serve({ perfDown: true });
    const body = await askQ(month.question);
    expect(body.outcome).toBe("rejected");
    expect(body.answer).toBe(FALLBACK_REPLY);
    expect(body.citations).toEqual([]);
    expect(lastLine().sources.some((id) => id.startsWith("perf:"))).toBe(false);
    expect(warn.mock.calls.some(([l]) => l.includes('"level":"WARN"') && l.includes("bot performance unavailable"))).toBe(true);
    const reads = perfReads();
    await askQ(month.question);
    expect(perfReads()).toBeGreaterThan(reads);
  });
});
