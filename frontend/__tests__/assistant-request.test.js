/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as ask } from "@/app/api/assistant/route";
import { readAnswers } from "@/lib/assistant/log";
import { parseRequestReply } from "@/lib/assistant/model";
import { REQUEST_ANSWERED, REQUEST_DRAFT, REQUEST_NEEDS_INFO, REQUEST_NONE, checkEvidence, checkRequest, looksLikeReport } from "@/lib/assistant/request";
import { setAssistantModel } from "@/lib/assistant/server";
import { pnlPassages } from "@/lib/assistant/service";
import { CHECKLIST, DRAFT_TEXT, NEEDS_INFO_TEXT } from "@/lib/assistantRequests";
import { signedIn, useTestAuth } from "@/test/auth";

// The request check (#409): the filter's outcomes on the golden cases, the
// guard on a draft's evidence numbers, and the acceptance scenarios through
// POST /api/assistant with a fake model — no test calls the model API.

const golden = fs
  .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
  .split("\n")
  .filter((l) => l.trim())
  .map((l) => JSON.parse(l));
const cases = golden.filter((q) => q.category === "request");
const byId = Object.fromEntries(cases.map((q) => [q.id, q]));
const unticked = (checklist) => checklist.filter((c) => !c.ok).map((c) => c.key);

describe("the filter on the golden cases", () => {
  test("there is a vague, an answered and a draft case", () => {
    expect(new Set(cases.map((q) => q.expected_behaviour))).toEqual(new Set(["needs_info", "answered", "draft"]));
  });

  test.each(cases.map((q) => [q.id, q]))("%s: the outcome, the checklist and the draft kind", (_id, q) => {
    const r = checkRequest(q.request.verdict, q.request.passages);
    expect(r.outcome).toBe(q.expected_behaviour);
    expect(r.checklist.map((c) => c.key)).toEqual(CHECKLIST.map((c) => c.key));
    expect(unticked(r.checklist)).toEqual(q.request.unticked);
    expect(r.draft?.kind ?? null).toBe(q.request.draft_kind ?? null);
    expect(r.details).toEqual([]);
  });

  test("every golden report reads as one; no refusal question does (advice keeps its refusal)", () => {
    for (const q of cases) expect([q.id, looksLikeReport(q.question)]).toEqual([q.id, true]);
    for (const q of golden.filter((x) => x.category.startsWith("refuse_"))) expect([q.id, looksLikeReport(q.question)]).toEqual([q.id, false]);
  });

  test.each([
    ["the chart is wrong", true],
    ["The P&L chart is broken since this morning", true],
    ["Can you add a CSV export?", true],
    ["The table should show the fees", true],
    ["Should I sell more puts?", false],
    ["What should I do now?", false],
    ["What is a short strangle?", false],
  ])("looksLikeReport(%p) = %p", (q, want) => {
    expect(looksLikeReport(q)).toBe(want);
  });

  test("a verdict that is not a report is answered as any other question", () => {
    expect(checkRequest({ ...byId["req-01"].request.verdict, kind: "none" }, []).outcome).toBe(REQUEST_NONE);
  });

  test("two reproducible steps count as evidence", () => {
    const v = { ...byId["req-01"].request.verdict, what: "The Ask button does nothing", where: { page: "Overview", bot: "", time: "" }, steps: ["Open Overview", "Press Ask"] };
    expect(checkRequest(v, []).outcome).toBe(REQUEST_DRAFT);
    expect(checkRequest({ ...v, steps: ["Press Ask"] }, []).outcome).toBe(REQUEST_NEEDS_INFO);
  });

  test("an explanation must cite a passage it was given", () => {
    const q = byId["req-02"];
    const r = checkRequest({ ...q.request.verdict, explained_by: ["journal:btc/999"] }, q.request.passages);
    expect(r.outcome).toBe(REQUEST_DRAFT); // nothing given explains it: the evidence stands
    expect(checkRequest(q.request.verdict, q.request.passages).outcome).toBe(REQUEST_ANSWERED);
  });
});

describe("Scenario: the guard rejects a draft whose evidence numbers are not in its sources", () => {
  const q = byId["req-03"];

  test("a number its cited P&L point does not hold rejects the draft (ungrounded_number)", () => {
    const evidence = q.request.verdict.evidence.map((e, i) => (i === 1 ? { ...e, text: "Total P&L -0.9 at 14:00 UTC" } : e));
    const r = checkRequest({ ...q.request.verdict, evidence }, q.request.passages);
    expect(r.outcome).toBe(REQUEST_NEEDS_INFO);
    expect(r.draft).toBeNull();
    expect(unticked(r.checklist)).toEqual(["evidence"]);
    expect(r.details).toEqual(["ungrounded_number: 0.9"]);
  });

  test("a number from another passage than the one cited is not grounded", () => {
    const { good, details } = checkEvidence([{ text: "Total P&L 0.0118", citation: "pnl:btc/2026-10-27T13:00:00Z" }], q.request.passages);
    expect(good).toEqual([]);
    expect(details).toEqual(["ungrounded_number: 0.0118"]);
  });

  test("evidence citing nothing it was given is rejected", () => {
    const { details } = checkEvidence([{ text: "Total P&L 0.012", citation: "pnl:btc/elsewhere" }, { text: "no source", citation: "" }], q.request.passages);
    expect(details).toEqual(["unknown_citation: pnl:btc/elsewhere", "unknown_citation: (none)"]);
  });

  test("a journal time is read as the person writes it (09:12, not T09:12)", () => {
    const { good } = checkEvidence([{ text: "No journal entry between 09:12 and 18:40 on 2026-10-27", citation: "journal:btc" }], q.request.passages);
    expect(good).toHaveLength(1);
  });
});

describe("pnlPassages", () => {
  const points = Array.from({ length: 60 }, (_, i) => ({
    t: `2026-10-27T${String(Math.floor(i / 4)).padStart(2, "0")}:${String((i % 4) * 15).padStart(2, "0")}:00Z`,
    total: i === 30 ? -0.048 : 0.012,
    realised: 0.004,
    unrealised: i === 30 ? -0.052 : 0.008,
    total_usd: 800.123,
    spot: 67000,
  }));

  test("keeps the points around the largest step, at most 24, oldest first", () => {
    const got = pnlPassages("btc", points);
    expect(got.length).toBeLessThanOrEqual(24);
    const ids = got.map((p) => p.id);
    expect(ids).toEqual(expect.arrayContaining(["pnl:btc/2026-10-27T07:15:00Z", "pnl:btc/2026-10-27T07:30:00Z"]));
    expect([...ids].sort()).toEqual(ids);
    expect(ids).toContain("pnl:btc/2026-10-27T14:45:00Z"); // the newest
  });

  test("a point in words, its time readable", () => {
    const [p] = pnlPassages("btc", [points[30]]);
    expect(p).toEqual({
      id: "pnl:btc/2026-10-27T07:30:00Z",
      source: "pnl:btc",
      title: "btc P&L history at 2026-10-27T07:30:00Z",
      text: "P&L recorded on 2026-10-27 07:30 UTC: total -0.048, realised 0.004, unrealised -0.052, total USD 800.12, spot 67000.",
    });
  });

  test("no points, no passages", () => {
    expect(pnlPassages("btc", undefined)).toEqual([]);
    expect(pnlPassages("btc", [{ t: "x" }])).toEqual([]);
  });
});

// Through the route: the fake model answers the request check with a golden verdict.
describe("Scenario: reports through POST /api/assistant", () => {
  let dir, file, cookie;
  const askQ = (body) =>
    ask(
      new Request("http://monitor/api/assistant", {
        method: "POST",
        headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie },
        body: JSON.stringify(body),
      }),
    );
  // The golden verdict is the model's raw reply: read as the real client reads it.
  const model = (verdict, reply = { text: "", citations: [], report: false }) => {
    const read = () => parseRequestReply({ stop_reason: "end_turn", content: [{ type: "text", text: JSON.stringify(verdict) }] });
    const m = { model: "fake", ask: jest.fn(async () => reply), checkRequest: jest.fn(async () => read()) };
    setAssistantModel(m);
    return m;
  };

  beforeEach(async () => {
    dir = fs.mkdtempSync(path.join(os.tmpdir(), "assistant-request-"));
    file = path.join(dir, "assistant.jsonl");
    process.env.ASSISTANT_LOG_PATH = file;
    jest.spyOn(console, "error").mockImplementation(() => {});
    jest.spyOn(console, "log").mockImplementation(() => {});
    useTestAuth();
    ({ cookie } = await signedIn("viewer", "user"));
  });
  afterEach(() => {
    jest.restoreAllMocks();
    setAssistantModel(undefined);
    delete process.env.ASSISTANT_LOG_PATH;
    delete process.env.BOT_APIS;
    fs.rmSync(dir, { recursive: true, force: true });
  });

  test('Given "the chart is wrong", then no draft and the four-item checklist shows what is missing', async () => {
    const m = model(byId["req-01"].request.verdict);
    const body = await (await askQ({ question: "the chart is wrong" })).json();
    expect(body.outcome).toBe("needs_info");
    expect(body.answer).toBe(NEEDS_INFO_TEXT);
    expect(body.draft).toBeUndefined();
    expect(body.checklist).toHaveLength(4);
    expect(unticked(body.checklist)).toEqual(["what", "where", "evidence"]);
    expect(m.ask).not.toHaveBeenCalled(); // the words read as a report: one call, the request check
    const [line] = [...readAnswers(file).values()];
    expect(line).toMatchObject({ outcome: "needs_info", request: { draft: null } });
  });

  test("a report the help and the journal cannot touch gets the checklist without a model call", async () => {
    const m = model(byId["req-01"].request.verdict);
    const body = await (await askQ({ question: "zxqv is broken" })).json();
    expect(body.outcome).toBe("needs_info");
    expect(unticked(body.checklist)).toEqual(["what", "where", "evidence"]);
    expect(m.checkRequest).not.toHaveBeenCalled();
  });

  /** Mocks the btc bot: its journal (/api/journal items) and its P&L history, from a golden case's passages. */
  function botFrom(q, points) {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    const events = q.request.passages
      .filter((p) => p.source === "journal:btc")
      .map((p) => ({ seq: Number(p.id.split("/")[1]), event: p.title.split(": ")[1].split(" at ")[0], at: p.title.split(" at ")[1], plain: p.text }));
    // The journal is read through /api/journal (#417) and kept 60 s per bot: each case starts from its own.
    globalThis[Symbol.for("optionsbot.monitor.assistant")]?.journals?.clear();
    return jest.spyOn(global, "fetch").mockImplementation(async (url) =>
      url.includes("/api/pnl/history") ? Response.json({ points }) : Response.json({ items: events }),
    );
  }

  test("Given a report whose figures agree with the journal, then the reply shows the check and no bug draft", async () => {
    const q = byId["req-02"];
    botFrom(q, [{ t: "2026-10-27T12:00:00Z", total: 0.0042, realised: 0.0042, unrealised: 0 }]);
    model(q.request.verdict);
    const body = await (await askQ({ question: q.question, bot: "btc" })).json();
    expect(body.outcome).toBe("answered");
    expect(body.answer).toBe(q.request.verdict.answer);
    expect(body.draft).toBeUndefined();
    expect(unticked(body.checklist)).toEqual(["not_explained"]);
    expect(body.citations.map((c) => c.id)).toEqual(["journal:btc/101", "journal:btc/102", "pnl:btc/2026-10-27T12:00:00Z"]);
  });

  test("an explained report whose answer has a number from nowhere gets the fallback, no checklist", async () => {
    const q = byId["req-02"];
    botFrom(q, [{ t: "2026-10-27T12:00:00Z", total: 0.0042, realised: 0.0042, unrealised: 0 }]);
    model({ ...q.request.verdict, answer: "The overview shows 0.9 BTC." });
    const body = await (await askQ({ question: q.question, bot: "btc" })).json();
    expect(body.outcome).toBe("rejected");
    expect(body.checklist).toBeUndefined();
  });

  test("Given a report with a P&L spike and an empty journal window, then a bug draft cites both", async () => {
    const q = byId["req-03"];
    const spy = botFrom(q, [
      { t: "2026-10-27T13:00:00Z", total: 0.012, realised: 0.004, unrealised: 0.008 },
      { t: "2026-10-27T14:00:00Z", total: -0.048, realised: 0.004, unrealised: -0.052 },
      { t: "2026-10-27T15:00:00Z", total: 0.0118, realised: 0.004, unrealised: 0.0078 },
    ]);
    const m = model(q.request.verdict);
    const body = await (await askQ({ question: q.question, bot: "btc" })).json();
    expect(spy).toHaveBeenCalledWith("http://bot-btc:8081/api/pnl/history?range=1d", expect.anything());
    // The model got the P&L points and the journal.
    const given = m.checkRequest.mock.calls[0][0].passages.map((p) => p.id);
    expect(given).toEqual(expect.arrayContaining(["pnl:btc/2026-10-27T14:00:00Z", "journal:btc/97", "journal:btc/98"]));
    expect(body.outcome).toBe("draft");
    expect(body.answer).toBe(DRAFT_TEXT);
    expect(body.draft).toMatchObject({ kind: "bug", title: q.request.verdict.title, where: "Overview · btc · 2026-10-27 14:00 UTC" });
    const sources = new Set(body.citations.map((c) => c.source));
    expect(sources).toEqual(new Set(["pnl:btc", "journal:btc"]));
    // A draft is logged with the answer for review, never queued: no requests file exists.
    expect([...readAnswers(file).values()][0]).toMatchObject({ outcome: "draft", request: { draft: { kind: "bug" } } });
    expect(fs.existsSync(path.join(dir, "requests.jsonl"))).toBe(false);
  });

  test("the model's report flag on a plain answer runs the request check", async () => {
    const q = byId["req-01"];
    const m = model(q.request.verdict, { text: "x", citations: [], report: true });
    const body = await (await askQ({ question: "what is a stop-loss" })).json();
    expect(m.ask).toHaveBeenCalledTimes(1);
    expect(m.checkRequest).toHaveBeenCalledTimes(1);
    expect(body.outcome).toBe("needs_info");
  });
});
