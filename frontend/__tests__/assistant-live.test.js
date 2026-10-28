/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { assistantIndex, botLive, setAssistantModel } from "@/lib/assistant/server";
import { check } from "@/lib/assistant/guard";
import { DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "@/lib/assistant/phrases";
import {
  LIVE_CACHE_MS,
  LIVE_TOKENS,
  answerQuestion,
  journalPassages,
  livePassages,
  namedInstruments,
  passageTokens,
  selectJournal,
} from "@/lib/assistant/service";

// The assistant sees the page's live data and every held leg's whole history
// (#427). The fixture is the golden set's why-16: the question that failed on
// mvp, with the bot's routes' answers and a correct answer. A fake model: no
// test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const why16 = golden.find((q) => q.id === "why-16");
const fixture = why16.bot;
const PUT = "BTC-27NOV26-75000-P";
const CALL = "BTC-27NOV26-100000-C";

const live = () => livePassages("btc", fixture);
const journal = () => journalPassages("btc", fixture.journal.items, fixture.journal.held);
const byId = (ps, id) => ps.find((p) => p.id === id);

describe("live passages", () => {
  test("the market now and one passage per open leg, from the routes' own fields", () => {
    const ps = live();
    expect(ps.map((p) => p.id)).toEqual(["live:btc/market", `live:btc/leg/${PUT}`, `live:btc/leg/${CALL}`]);
    expect(ps.every((p) => p.source === "live:btc")).toBe(true);
    const put = byId(ps, `live:btc/leg/${PUT}`).text;
    for (const s of [
      `${PUT}: short put, strike 75,000, slot 50-day 0.16 delta, expiry 27 Nov 2026, 30.1 days left.`,
      "Qty 0.3, entry price 0.0150, mark 0.0222, P&L −0.00216 BTC, -48% of its premium.",
      "Delta -0.27, OTM 6.4%.",
      "Stop-loss at mark 0.0450 (spot 69,850 on the model); delta-exit line at spot 74,211.",
    ]) {
      expect(put).toContain(s);
    }
    expect(byId(ps, "live:btc/market").text).toBe(
      "Market now (28 Oct 04:30 UTC): spot 80,123, DVOL 44.6, IV percentile 38 %, gamma regime NEGATIVE " +
        "(confirmed regime not negative), trend bear (from sma), gamma flip 83,000, shed band ±3.5 % of spot around the flip. " +
        "IM 14.2 % of margin balance, limit 20 %.",
    );
  });

  test("a missing figure reads unknown, never guessed", () => {
    const positions = structuredClone(fixture.positions);
    delete positions.strangles[0].legs[0].mark;
    delete positions.strangles[0].legs[0].greeks;
    const [put] = livePassages("btc", { positions }).map((p) => p.text);
    expect(put).toContain("mark unknown");
    expect(put).toContain("Delta unknown");
    expect(put).toContain("(spot unknown on the model); delta-exit line at spot unknown.");
    const [market] = livePassages("btc", { status: { market: {} } }).map((p) => p.text);
    expect(market).toBe(
      "Market now (time unknown): spot unknown, DVOL unknown, IV percentile unknown %, gamma regime unknown (confirmation unknown), " +
        "trend unknown, gamma flip unknown, shed band unknown around the flip. IM unknown % of margin balance, limit unknown %.",
    );
  });

  test("a one-legged strangle's leg says why the other side is not held", () => {
    const status = { ...fixture.status, held_legs: [{ strangle_id: "s1", missing: "call", reason: "the call was stopped 20 h ago: cooldown 72 h" }] };
    const put = byId(livePassages("btc", { ...fixture, status }), `live:btc/leg/${PUT}`).text;
    expect(put).toContain("Its call is not held: the call was stopped 20 h ago: cooldown 72 h.");
  });

  test("the budget holds: a full book (8 legs + the market) stays under LIVE_TOKENS", () => {
    const leg = fixture.positions.strangles[0].legs[0];
    const strangles = [0, 1, 2, 3].map((i) => ({
      id: `s${i}`,
      slot: { dte: 25 + i * 10, delta: 0.16 },
      legs: [
        { ...leg, position_id: `p${i}`, instrument: `BTC-27NOV26-${70000 + i * 1000}-P` },
        { ...leg, position_id: `c${i}`, option_type: "call", instrument: `BTC-27NOV26-${100000 + i * 1000}-C` },
      ],
    }));
    const status = {
      ...fixture.status,
      held_legs: [{ strangle_id: "s0", missing: "call", reason: "the regime is negative and the trend bull: no new calls" }],
    };
    const ps = livePassages("btc", { status, positions: { ...fixture.positions, strangles }, payoff: fixture.payoff });
    expect(ps).toHaveLength(9);
    expect(ps.reduce((n, p) => n + passageTokens(p), 0)).toBeLessThanOrEqual(LIVE_TOKENS);
  });
});

describe("journal passages carry the market at each fill", () => {
  test("the 21 Oct repair fill: spot, DVOL, IV percentile and regime from its line", () => {
    expect(byId(journal(), "journal:btc/3602")).toMatchObject({
      text:
        "Sold the option again to complete its strangle (repair) " +
        "Market recorded at 21 Oct 14:30 UTC: spot 84,498, DVOL 37.2, IV percentile 15 %, regime POSITIVE/PINNING.",
      trade: true,
      held: true,
      instrument: PUT,
    });
    expect(byId(journal(), "journal:btc/4100").text).not.toContain("Market recorded");
  });
});

describe("namedInstruments", () => {
  const held = [CALL, PUT, "BTC-27DEC26-70000-P"];
  test.each([
    ["Why the put leg for BTC Strike 75.000 is -48%?", [PUT]],
    ["what about the 75,000 put", [PUT]],
    ["and the 75k strike?", [PUT]],
    [`what happened to ${CALL}`, [CALL]],
    ["why are my puts losing", [PUT, "BTC-27DEC26-70000-P"]],
    ["how is my book doing", held],
    ["the 12345 put", [PUT, "BTC-27DEC26-70000-P"]],
  ])("%s", (question, want) => {
    expect(namedInstruments(question, held)).toEqual(want);
  });
});

describe("selectJournal and the held legs", () => {
  test("Scenario: a leg opened 40 days ago and still held has its entry in the passages", () => {
    // A busy week: 100 fills of other instruments, newer than the held put's entry.
    const busy = Array.from({ length: 100 }, (_, i) => ({
      seq: 5000 + i,
      event: "filled",
      reason: "entry",
      instrument: `BTC-1NOV26-${60000 + i * 100}-P`,
      at: "2026-10-27T10:00:00Z",
      first: "2026-10-27T10:00:00Z",
      last: "2026-10-27T10:00:00Z",
      count: 1,
      plain: "Sold the option to open a new strangle and collect its premium (entry)",
    }));
    const ps = journalPassages("btc", [...fixture.journal.items, ...busy], fixture.journal.held);
    const sent = selectJournal(why16.question, ps, { maxPassages: 5 }).map((p) => p.seq);
    expect(sent).toEqual(expect.arrayContaining([120, 3400, 3602])); // the put's entry, stop and repair
    expect(sent).not.toContain(121); // the call's entry: not the leg asked about
    expect(selectJournal("how is my book doing", ps, { maxPassages: 5 }).map((p) => p.seq)).toEqual(expect.arrayContaining([120, 121]));
  });
});

describe("the question that failed on mvp", () => {
  let dir;
  beforeEach(() => (dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-live-"))));
  afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

  const ask = async (answer) => {
    const model = { model: "fake", calls: [], ask: async (args) => (model.calls.push(args), structuredClone(answer)) };
    const out = await answerQuestion({
      question: why16.question,
      bot: "btc",
      journal: journal(),
      live: live(),
      user: { username: "viewer", role: "user" },
      model,
      index: assistantIndex(),
      file: path.join(dir, "assistant.jsonl"),
    });
    return { out, sent: model.calls[0]?.passages ?? [] };
  };

  test("Scenario: the passages hold the live leg, its 21 Oct fill with the market then, and the market now", async () => {
    const { sent } = await ask(fixture.answer);
    const ids = sent.map((p) => p.id);
    expect(ids).toEqual(expect.arrayContaining([`live:btc/leg/${PUT}`, "live:btc/market", "journal:btc/3602", "journal:btc/120"]));
    expect(byId(sent, "journal:btc/3602").text).toContain("spot 84,498, DVOL 37.2, IV percentile 15 %");
  });

  test("Scenario: a grounded answer citing them is shown, with how to avoid it declined in one sentence", async () => {
    const { out } = await ask(fixture.answer);
    expect(out.status).toBe(200);
    expect(out.body.outcome).toBe("answered");
    expect(out.body.answer).toBe(fixture.answer.text);
    expect(out.body.answer.endsWith(REFUSAL)).toBe(true);
    expect(out.body.citations.map((c) => c.id)).toEqual(fixture.answer.citations);
    for (const phrase of why16.must_say) expect(out.body.answer).toContain(phrase);
  });

  test("the golden answer passes the guard on the passages built from its fixture", () => {
    expect(check(fixture.answer, [...live(), ...journal()])).toEqual({ ok: true, reasons: [], details: [] });
  });

  test("an uncited number is still rejected", async () => {
    // Without the fill it is cited from, 84,498 (and 21 Oct, 37.2, 15) comes from nowhere.
    const { out } = await ask({ ...fixture.answer, citations: [`live:btc/leg/${PUT}`, "live:btc/market"] });
    expect(out.body.outcome).toBe("rejected");
    expect(out.body.answer).toBe(FALLBACK_REPLY);
    const v = check({ ...fixture.answer, citations: [`live:btc/leg/${PUT}`, "live:btc/market"] }, [...live(), ...journal()]);
    expect(v.details).toContain("ungrounded_number: 84,498");
  });

  test("the refusal alone is still a refusal; I don't know plus the refusal is canonical", async () => {
    expect((await ask({ text: REFUSAL, citations: [] })).out.body.outcome).toBe("refused");
    const mixed = await ask({ text: `${DONT_KNOW} ${REFUSAL}`, citations: [] });
    expect(mixed.out.body).toMatchObject({ outcome: "dont_know", answer: `${DONT_KNOW} ${REFUSAL}`, citations: [] });
  });

  test("the budget holds: the whole input stays under 10k tokens with a big journal", async () => {
    const many = Array.from({ length: 2000 }, (_, i) => ({
      seq: 10000 + i,
      event: i % 3 ? "skipped" : "filled",
      reason: i % 3 ? "no_free_expiry" : "entry",
      instrument: i % 2 ? PUT : `BTC-1NOV26-${60000 + i}-P`,
      at: "2026-10-27T10:00:00Z",
      first: "2026-10-27T10:00:00Z",
      last: "2026-10-27T10:00:00Z",
      count: 1,
      plain: `Slot ${i} waits: every expiry in its window is held by another slot, the put at 75,000 among them.`,
      market: { spot: 80000 + i, dvol: 40, iv_percentile: 20, gex_regime: "NEGATIVE" },
    }));
    const model = { model: "fake", calls: [], ask: async (args) => (model.calls.push(args), structuredClone(fixture.answer)) };
    await answerQuestion({
      question: why16.question,
      bot: "btc",
      journal: journalPassages("btc", [...fixture.journal.items, ...many], fixture.journal.held),
      live: live(),
      user: { username: "viewer", role: "user" },
      model,
      index: assistantIndex(),
      file: path.join(dir, "assistant.jsonl"),
    });
    const tokens = model.calls[0].passages.reduce((n, p) => n + passageTokens(p), 0);
    expect(tokens).toBeLessThanOrEqual(10_000);
  });
});

describe("botLive", () => {
  const NOW = Date.parse("2026-10-28T04:30:00Z");
  let warn;
  beforeEach(() => {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    setAssistantModel(undefined); // a fresh cache
    warn = jest.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => {
    jest.restoreAllMocks();
    delete process.env.BOT_APIS;
  });

  const routes = { "/api/status": fixture.status, "/api/positions": fixture.positions, "/api/strangles/payoff": fixture.payoff };
  const serve = (down = []) =>
    jest.spyOn(global, "fetch").mockImplementation(async (url) => {
      const route = new URL(url).pathname;
      if (down.includes(route)) throw new Error("connect refused");
      return Response.json(routes[route]);
    });

  test("reads status, positions and payoff once per question, kept 10 s", async () => {
    const fetchSpy = serve();
    const first = await botLive("btc", NOW);
    expect(first.map((p) => p.id)).toEqual(live().map((p) => p.id));
    expect(fetchSpy).toHaveBeenCalledTimes(3);
    expect(await botLive("btc", NOW + LIVE_CACHE_MS - 1)).toBe(first);
    expect(fetchSpy).toHaveBeenCalledTimes(3);
    await botLive("btc", NOW + LIVE_CACHE_MS);
    expect(fetchSpy).toHaveBeenCalledTimes(6);
  });

  test("without the payoff the exit lines read unknown", async () => {
    serve(["/api/strangles/payoff"]);
    const ps = await botLive("btc", NOW);
    expect(byId(ps, `live:btc/leg/${PUT}`).text).toContain("delta-exit line at spot unknown.");
    expect(warn.mock.calls.some(([l]) => l.includes("bot payoff unavailable"))).toBe(true);
  });

  test("Scenario: the bot is down → [] with a WARN, and the help alone answers", async () => {
    const fetchSpy = serve(["/api/status", "/api/positions", "/api/strangles/payoff"]);
    expect(await botLive("btc", NOW)).toEqual([]);
    expect(warn.mock.calls.some(([l]) => l.includes('"level":"WARN"') && l.includes("bot live data unavailable"))).toBe(true);
    await botLive("btc", NOW + 1); // not cached: the next question tries again
    expect(fetchSpy.mock.calls.length).toBeGreaterThan(3);
  });

  test("a bot that is not configured is null", async () => {
    expect(await botLive("doge", NOW)).toBeNull();
  });
});
