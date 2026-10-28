/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { assistantIndex, botJournal, setAssistantModel } from "@/lib/assistant/server";
import {
  JOURNAL_CACHE_MS,
  JOURNAL_PASSAGES,
  JOURNAL_TOKENS,
  answerQuestion,
  explainPassages,
  journalPassages,
  passageTokens,
  selectJournal,
} from "@/lib/assistant/service";

// The assistant's week of journal facts (#417): what one question sends and
// the 60 s cache of /api/journal. A fake model: no test calls the model API.

const DAY = 24 * 3600_000;
const NOW = Date.parse("2026-10-27T12:00:00Z");
const iso = (ms) => new Date(ms).toISOString();

/** A /api/journal item; count > 1 is a collapsed run from first to at. */
function item(seq, event, reason, atMs, plain, { count = 1, firstMs = atMs } = {}) {
  return { seq, event, reason, at: iso(atMs), first: iso(firstMs), last: iso(atMs), count, plain };
}

/** A week as the bot serves it: a stop-loss 6 days ago among many notices. */
function week() {
  const closeAt = NOW - 6 * DAY;
  const items = [
    item(10, "filled", "entry", closeAt - 2 * DAY, "Sold the put of the 25-day strangle (entry)."),
    item(20, "risk_limit", "frozen", closeAt - 10 * 60_000, "New risk is frozen: the margin band changed."),
    item(21, "closed", "stop_loss", closeAt, "Bought back the put: it lost twice its premium (stop-loss)."),
    item(22, "skipped", "repair_held", closeAt + 5 * 60_000, "A missing leg is not sold again yet: that side was closed by a stop-loss.", {
      count: 180,
      firstMs: closeAt + 5 * 60_000 - 0,
    }),
  ];
  // 300 identical gex_wait skips in a day, collapsed by the bot into one item.
  items.push(item(900, "skipped", "gex_wait", NOW - 2 * 3600_000, "No new risk until fresh market-maker data arrives.", {
    count: 300,
    firstMs: NOW - DAY,
  }));
  // Many unrelated notices.
  for (let i = 0; i < 200; i++) {
    items.push(item(1000 + i, "skipped", `no_free_expiry`, NOW - 3 * DAY + i * 60_000, `Slot ${i} waits: every expiry in its window is held by another slot.`));
  }
  items.sort((a, b) => a.seq - b.seq);
  return items;
}

describe("journal passages", () => {
  test("a collapsed item reads ×N, first → last: plain", () => {
    const [p] = journalPassages("btc", [
      item(42, "skipped", "gex_wait", Date.parse("2026-10-21T14:30:00Z"), "No new risk yet.", {
        count: 42,
        firstMs: Date.parse("2026-10-20T08:00:00Z"),
      }),
    ]);
    expect(p).toMatchObject({ id: "journal:btc/42", source: "journal:btc", text: "×42, 20 Oct 08:00 → 21 Oct 14:30: No new risk yet.", trade: false });
  });

  test("a single item is its sentence; opens and closes are trades", () => {
    const ps = journalPassages("btc", week());
    expect(ps.find((p) => p.id === "journal:btc/21")).toMatchObject({ text: "Bought back the put: it lost twice its premium (stop-loss).", trade: true });
    expect(ps.filter((p) => p.trade).map((p) => p.seq)).toEqual([10, 21]);
  });
});

describe("selectJournal", () => {
  const passages = journalPassages("btc", week());

  test("every open and close is sent, even for a question that does not name them", () => {
    const sent = selectJournal("zxqv wplm", passages);
    expect(sent.map((p) => p.seq)).toEqual(expect.arrayContaining([10, 21]));
  });

  test("Scenario: a close 6 days ago and the decisions around it are in the passages", () => {
    const sent = selectJournal("why did the bot buy back the put", passages).map((p) => p.seq);
    expect(sent).toEqual(expect.arrayContaining([21, 20, 22])); // the close, the freeze 10 min before, the hold after
  });

  test("Scenario: 300 identical gex_wait lines are one passage with its count and first and last times", () => {
    const sent = selectJournal("why is the bot waiting for market-maker data", passages);
    const gex = sent.filter((p) => p.reason === "gex_wait");
    expect(gex).toHaveLength(1);
    expect(gex[0].text).toBe("×300, 26 Oct 12:00 → 27 Oct 10:00: No new risk until fresh market-maker data arrives.");
  });

  test("the most relevant notices first, within the budget", () => {
    const sent = selectJournal("slot 7 expiry window", passages, { maxPassages: 3 });
    expect(sent).toHaveLength(3);
    expect(sent.filter((p) => p.trade)).toHaveLength(2);
    expect(sent.find((p) => !p.trade).text).toContain("Slot 7 waits");
  });

  test("the budget holds: at most JOURNAL_PASSAGES passages and JOURNAL_TOKENS tokens, oldest first", () => {
    const sent = selectJournal("slot waits expiry window held", passages);
    expect(sent.length).toBeLessThanOrEqual(JOURNAL_PASSAGES);
    expect(sent.reduce((n, p) => n + passageTokens(p), 0)).toBeLessThanOrEqual(JOURNAL_TOKENS);
    expect(sent.map((p) => p.seq)).toEqual([...sent.map((p) => p.seq)].sort((a, b) => a - b));
    const small = selectJournal("slot waits", passages, { maxTokens: 100 });
    expect(small.reduce((n, p) => n + passageTokens(p), 0)).toBeLessThanOrEqual(100);
  });

  test("trades alone over the budget: the newest are kept", () => {
    const many = journalPassages(
      "btc",
      Array.from({ length: 10 }, (_, i) => item(i + 1, "filled", "entry", NOW - i * 3600_000, `Sold leg ${i}.`)),
    );
    expect(selectJournal("anything", many, { maxPassages: 3 }).map((p) => p.seq)).toEqual([8, 9, 10]);
  });

  test("a Why? keeps the facts of the trade asked about, outside the budget", () => {
    const why = explainPassages("btc", {
      event: { seq: 2, event: "closed", at: new Date(NOW - 6 * 86400_000).toISOString(), plain: "Bought back the put: delta exit." },
      rule: "delta_exit",
      related: [],
    });
    const sent = selectJournal("zxqv", [...why, ...passages], { maxPassages: 1, maxTokens: 1 });
    expect(sent.map((p) => p.id)).toEqual(["journal:btc/2"]);
    expect(sent[0].pinned).toBe(true);
  });
});

describe("answerQuestion with a week of journal", () => {
  let dir;
  beforeEach(() => (dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-journal-"))));
  afterEach(() => fs.rmSync(dir, { recursive: true, force: true }));

  test("the close of 6 days ago can be cited", async () => {
    const model = { model: "fake", calls: [], ask: async (args) => (model.calls.push(args), { text: "It lost twice its premium.", citations: ["journal:btc/21"] }) };
    const out = await answerQuestion({
      question: "why did the bot buy back the put",
      bot: "btc",
      journal: journalPassages("btc", week()),
      user: { username: "viewer", role: "user" },
      model,
      index: assistantIndex(),
      file: path.join(dir, "assistant.jsonl"),
    });
    expect(out.body.outcome).toBe("answered");
    expect(out.body.citations[0].id).toBe("journal:btc/21");
    const sent = model.calls[0].passages.filter((p) => p.source === "journal:btc");
    expect(sent.length).toBeLessThanOrEqual(JOURNAL_PASSAGES);
    expect(sent.some((p) => / pnl /.test(p.title))).toBe(false);
  });
});

describe("botJournal", () => {
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

  test("reads /api/journal?days=7 once a minute per bot", async () => {
    const fetchSpy = jest.spyOn(global, "fetch").mockImplementation(async () => Response.json({ items: week() }));
    const first = await botJournal("btc", NOW);
    expect(fetchSpy).toHaveBeenCalledWith("http://bot-btc:8081/api/journal?days=7", expect.anything());
    expect(first.length).toBe(week().length);
    expect(await botJournal("btc", NOW + JOURNAL_CACHE_MS - 1)).toBe(first);
    expect(fetchSpy).toHaveBeenCalledTimes(1);
    await botJournal("btc", NOW + JOURNAL_CACHE_MS);
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });

  test("Scenario: the bot is down → [] with a WARN, and the next question tries again", async () => {
    const fetchSpy = jest.spyOn(global, "fetch").mockRejectedValue(new Error("connect refused"));
    expect(await botJournal("btc", NOW)).toEqual([]);
    expect(warn.mock.calls.some(([l]) => l.includes('"level":"WARN"') && l.includes("bot journal unavailable"))).toBe(true);
    await botJournal("btc", NOW + 1);
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });

  test("a bot that is not configured is null", async () => {
    expect(await botJournal("doge", NOW)).toBeNull();
  });
});
