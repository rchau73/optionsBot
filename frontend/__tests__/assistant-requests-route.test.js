/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { POST as ask } from "@/app/api/assistant/route";
import { GET as list, POST as send } from "@/app/api/assistant/requests/route";
import { appendLine } from "@/lib/assistant/log";
import { myRequests, nextRequestId, readLines, sentToday } from "@/lib/assistant/requests";
import { parseRequestReply } from "@/lib/assistant/model";
import { setAssistantModel } from "@/lib/assistant/server";
import { REQUESTS_PER_DAY, requestFields, statusLabel } from "@/lib/assistantRequests";
import { signedIn, useTestAuth } from "@/test/auth";

// The private request queue (#409): POST and GET /api/assistant/requests —
// sign-in, same origin, the daily limit, own requests only, and the draft's
// sources taken from the review log, never from the browser.

let dir, log, queue, status, cookie;
const headers = (extra = {}) => ({ origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...extra });
const sendR = (body, extra) => send(new Request("http://monitor/api/assistant/requests", { method: "POST", headers: headers(extra), body: JSON.stringify(body) }));
const listR = (extra = {}) => list(new Request("http://monitor/api/assistant/requests", { headers: { host: "monitor", cookie, ...extra } }));
const lines = () => readLines(queue);

const DRAFT = {
  kind: "bug",
  title: "P&L spike at 14:00 UTC",
  what: "The total P&L dropped for one hour",
  where: "Overview · btc",
  expected: "No drop without a journal entry",
  evidence: [
    { text: "Total P&L -0.048 at 14:00 UTC", citation: "pnl:btc/2026-10-27T14:00:00Z" },
    { text: "No journal entry between 09:12 and 18:40", citation: "journal:btc" },
  ],
  steps: [],
};

/** A logged answer carrying a draft, as the request check writes it. */
function loggedDraft(id, user = "viewer", draft = DRAFT) {
  appendLine(log, { kind: "answer", id, ts: new Date().toISOString(), user, role: "user", outcome: "draft", request: { checklist: [], draft } });
}
const body = (answerId, fields = requestFields("bug", DRAFT), kind = "bug") => ({ answer_id: answerId, kind, fields });

beforeEach(async () => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "requests-"));
  log = path.join(dir, "assistant.jsonl");
  queue = path.join(dir, "requests.jsonl");
  status = path.join(dir, "requests_status.jsonl");
  process.env.ASSISTANT_LOG_PATH = log;
  process.env.ASSISTANT_REQUESTS_PATH = queue;
  process.env.MONITOR_DEPLOYMENT = "mvp";
  jest.spyOn(console, "error").mockImplementation(() => {});
  jest.spyOn(console, "log").mockImplementation(() => {});
  useTestAuth();
  ({ cookie } = await signedIn("viewer", "user"));
});
afterEach(() => {
  jest.restoreAllMocks();
  setAssistantModel(undefined);
  for (const k of ["ASSISTANT_LOG_PATH", "ASSISTANT_REQUESTS_PATH", "MONITOR_DEPLOYMENT", "BOT_APIS"]) delete process.env[k];
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Scenario: Given Send, then one line lands in requests.jsonl and My requests shows it In review", () => {
  test("a draft from the request check, sent once", async () => {
    // The draft comes from a real ask: the fake model's request check returns the spike verdict.
    const golden = fs
      .readFileSync(path.join(process.cwd(), "..", "docs", "product", "assistant", "golden.jsonl"), "utf8")
      .split("\n")
      .filter((l) => l.includes('"id":"req-03"'))
      .map((l) => JSON.parse(l))[0];
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    // The journal is read through /api/journal (#417) and kept 60 s per bot.
    globalThis[Symbol.for("optionsbot.monitor.assistant")]?.journals?.clear();
    jest.spyOn(global, "fetch").mockImplementation(async (url) =>
      url.includes("/api/pnl/history")
        ? Response.json({
            points: [
              { t: "2026-10-27T13:00:00Z", total: 0.012, realised: 0.004, unrealised: 0.008 },
              { t: "2026-10-27T14:00:00Z", total: -0.048, realised: 0.004, unrealised: -0.052 },
              { t: "2026-10-27T15:00:00Z", total: 0.0118, realised: 0.004, unrealised: 0.0078 },
            ],
          })
        : Response.json({
            items: [
              { seq: 97, event: "skipped", at: "2026-10-27T09:12:00Z", plain: "No new strangle: the slot's expiry is held by another slot." },
              { seq: 98, event: "skipped", at: "2026-10-27T18:40:00Z", plain: "No new strangle: the slot's expiry is held by another slot." },
            ],
          }),
    );
    const text = JSON.stringify(golden.request.verdict);
    setAssistantModel({ model: "fake", ask: jest.fn(), checkRequest: async () => parseRequestReply({ content: [{ type: "text", text }] }) });
    const answer = await (
      await ask(new Request("http://monitor/api/assistant", { method: "POST", headers: headers(), body: JSON.stringify({ question: golden.question, bot: "btc" }) }))
    ).json();
    expect(answer.outcome).toBe("draft");
    // Nothing is queued before the click.
    expect(lines()).toEqual([]);

    const res = await sendR(body(answer.id, requestFields("bug", answer.draft)));
    expect(res.status).toBe(201);
    expect(await res.json()).toEqual({ id: "R-0001", status: "in_review" });
    const [line] = lines();
    expect(line).toMatchObject({
      id: "R-0001",
      user: "viewer",
      role: "user",
      environment: "mvp",
      kind: "bug",
      answer_id: answer.id,
      fields: { title: golden.request.verdict.title },
    });
    expect(line.citations).toEqual(expect.arrayContaining(["pnl:btc/2026-10-27T14:00:00Z", "journal:btc"]));
    expect(line.journal_refs).toEqual(line.citations);
    expect(lines()).toHaveLength(1);

    const mine = await (await listR()).json();
    expect(mine.requests).toEqual([expect.objectContaining({ id: "R-0001", kind: "bug", title: golden.request.verdict.title, status: "in_review", note: null, issue_url: null })]);

    // The same draft sent again: refused, still one line.
    expect((await sendR(body(answer.id, requestFields("bug", answer.draft)))).status).toBe(409);
    expect(lines()).toHaveLength(1);
  });

  test("the person's edits are stored; the sources stay the logged ones", async () => {
    loggedDraft("a1");
    const fields = requestFields("bug", { ...DRAFT, title: "Edited title", evidence: [{ ...DRAFT.evidence[0], text: "My own words" }] });
    expect((await sendR(body("a1", fields))).status).toBe(201);
    const [line] = lines();
    expect(line.fields.title).toBe("Edited title");
    expect(line.fields.evidence).toEqual([{ text: "My own words", citation: "pnl:btc/2026-10-27T14:00:00Z" }]);
    expect(line.citations).toEqual(["pnl:btc/2026-10-27T14:00:00Z", "journal:btc"]);
  });

  test("evidence with a source the draft did not cite is refused", async () => {
    loggedDraft("a1");
    const fields = requestFields("bug", { ...DRAFT, evidence: [{ text: "x", citation: "journal:eth/1" }] });
    expect((await sendR(body("a1", fields))).status).toBe(400);
    expect(lines()).toEqual([]);
  });
});

describe("the requests API: who and how", () => {
  test.each([
    ["sending", () => sendR(body("a1"), { cookie: "" })],
    ["listing", () => listR({ cookie: "" })],
  ])("%s without a session gets 401", async (_name, call) => {
    loggedDraft("a1");
    expect((await call()).status).toBe(401);
    expect(lines()).toEqual([]);
  });

  test("a cross-origin send is refused", async () => {
    loggedDraft("a1");
    expect((await sendR(body("a1"), { origin: "http://evil" })).status).toBe(403);
    expect(lines()).toEqual([]);
  });

  test("a body that is not JSON is refused (415)", async () => {
    loggedDraft("a1");
    expect((await sendR(body("a1"), { "content-type": "text/plain" })).status).toBe(415);
  });

  test.each([
    ["no answer id", { kind: "bug", fields: {} }],
    ["an unknown kind", { answer_id: "a1", kind: "question", fields: {} }],
    ["no fields", { answer_id: "a1", kind: "bug" }],
    ["no title", body("a1", { ...requestFields("bug", DRAFT), title: " " })],
    ["too many steps", body("a1", { ...requestFields("bug", DRAFT), steps: Array(11).fill("s") })],
  ])("%s is refused (400)", async (_name, b) => {
    loggedDraft("a1");
    expect((await sendR(b)).status).toBe(400);
  });

  test("another person's draft, an answer without a draft or an unknown id: 404", async () => {
    loggedDraft("theirs", "someone");
    appendLine(log, { kind: "answer", id: "plain", user: "viewer", outcome: "answered" });
    for (const id of ["theirs", "plain", "nope"]) expect((await sendR(body(id))).status).toBe(404);
    expect(lines()).toEqual([]);
  });

  test(`at most ${REQUESTS_PER_DAY} a UTC day per person (429), another person unaffected`, async () => {
    for (let i = 0; i < REQUESTS_PER_DAY; i++) {
      loggedDraft(`a${i}`);
      expect((await sendR(body(`a${i}`))).status).toBe(201);
    }
    loggedDraft("extra");
    expect((await sendR(body("extra"))).status).toBe(429);
    expect(lines()).toHaveLength(REQUESTS_PER_DAY);
    expect(lines().at(-1).id).toBe("R-0010");

    ({ cookie } = await signedIn("other", "user"));
    loggedDraft("o1", "other");
    expect((await sendR(body("o1"))).status).toBe(201);
  });

  test("My requests: own only, newest first, with the review's latest status", async () => {
    loggedDraft("a1");
    loggedDraft("a2");
    await sendR(body("a1"));
    await sendR(body("a2", { ...requestFields("bug", DRAFT), title: "Second" }));
    appendLine(queue, { id: "R-0003", ts: new Date().toISOString(), user: "someone", kind: "bug", fields: { title: "Not mine" } });
    appendLine(status, { id: "R-0001", status: "needs_info", note: "Which page?", ts: "t1" });
    appendLine(status, { id: "R-0001", status: "filed", issue_url: "https://github.com/rchau73/optionsBot/issues/500", note: "Filed", ts: "t2" });
    appendLine(status, { id: "R-0002", status: "made_up" });
    const got = (await (await listR()).json()).requests;
    expect(got.map((r) => [r.id, r.title, r.status])).toEqual([
      ["R-0002", "Second", "in_review"],
      ["R-0001", "P&L spike at 14:00 UTC", "filed"],
    ]);
    expect(got[1]).toMatchObject({ note: "Filed", issue_url: "https://github.com/rchau73/optionsBot/issues/500" });
  });

  test("off without a queue file (an in-memory user database, no ASSISTANT_REQUESTS_PATH): 503", async () => {
    delete process.env.ASSISTANT_REQUESTS_PATH;
    expect((await listR()).status).toBe(503);
    expect((await sendR(body("a1"))).status).toBe(503);
  });
});

describe("the queue's pure helpers", () => {
  test("ids count up from the highest", () => {
    expect(nextRequestId([])).toBe("R-0001");
    expect(nextRequestId([{ id: "R-0009" }, { id: "R-0012" }, { id: "x" }])).toBe("R-0013");
  });

  test("sentToday counts the person's requests of the UTC day", () => {
    const now = Date.parse("2026-10-28T10:00:00Z");
    const reqs = [
      { user: "a", ts: "2026-10-28T00:01:00Z" },
      { user: "a", ts: "2026-10-27T23:59:00Z" },
      { user: "b", ts: "2026-10-28T09:00:00Z" },
    ];
    expect(sentToday(reqs, "a", now)).toBe(1);
  });

  test("an issue link that is not https is dropped", () => {
    const [r] = myRequests([{ id: "R-0001", user: "a", fields: { title: "t" } }], [{ id: "R-0001", status: "filed", issue_url: "javascript:alert(1)" }], "a");
    expect(r.issue_url).toBeNull();
  });

  test("the PM agent's not_planned status (#410) is shown, its issue link kept", () => {
    const statuses = [
      { id: "R-0001", status: "filed", issue: 7, issue_url: "https://github.com/o/r/issues/7" },
      { id: "R-0001", status: "not_planned", issue: 7, issue_url: "https://github.com/o/r/issues/7", note: "The issue was closed as not planned" },
    ];
    const [r] = myRequests([{ id: "R-0001", user: "a", fields: { title: "t" } }], statuses, "a");
    expect(r.status).toBe("not_planned");
    expect(statusLabel(r.status)).toBe("Not planned");
    expect(r.issue_url).toBe("https://github.com/o/r/issues/7");
  });
});
