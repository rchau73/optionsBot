/**
 * @jest-environment node
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { GET, POST } from "@/app/api/notifications/route";
import { signedIn, useTestAuth } from "@/test/auth";
import { parseReleaseNote, readReleaseNotes } from "@/lib/releases";

// #447: the notification bell's list, read marks and settings, per person on
// the server. The bot's journal events come from its /api/events (fetch is
// faked), the requests from the private queue, the notes from a folder.

const DAY = 24 * 3600;
const NOW = Date.UTC(2026, 9, 28, 12);
const nowSec = Math.floor(NOW / 1000);
const iso = (daysAgo) => new Date(NOW - daysAgo * DAY * 1000).toISOString();

let cookie, user, auth, dir, botEvents, botUp, weekNet;

const req = (method, body, { bot = "btc", ...headers } = {}) =>
  new Request(`http://monitor/api/notifications${bot ? `?bot=${bot}` : ""}`, {
    method,
    headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
const get = async (over) => (await GET(req("GET", undefined, over))).json();
const post = (body, over) => POST(req("POST", body, over));

/** A journal event as /api/events serves it. */
const ev = (seq, event, data = {}, plain = `plain ${seq}`, daysAgo = 1) => ({ seq, event, at: iso(daysAgo), data, plain });
const stopLoss = (seq) => ev(seq, "closed", { close_reason: "stop_loss", instrument: "BTC-27NOV26-60000-P" }, "Bought back the put at the stop-loss: its price doubled.");

/** NPS due on the user's row (#446): consent, 20 days old, 5 sign-ins, never shown. */
const surveyDue = () => {
  auth.store.setAnalyticsConsent(user.id, true);
  auth.store.db.run("UPDATE users SET created_at = ?, sign_in_count = 5, nps_last_shown_at = NULL WHERE id = ?", [nowSec - 20 * DAY, user.id]);
};
const writeLines = (file, lines) => fs.writeFileSync(file, lines.map((l) => JSON.stringify(l)).join("\n") + "\n");

beforeEach(async () => {
  jest.useFakeTimers({ now: NOW, doNotFake: ["nextTick", "setImmediate", "queueMicrotask"] });
  jest.spyOn(console, "log").mockImplementation(() => {});
  jest.spyOn(console, "error").mockImplementation(() => {});
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "notifications-"));
  process.env.BOT_APIS = "btc=http://bot-btc:8081";
  process.env.ASSISTANT_REQUESTS_PATH = path.join(dir, "requests.jsonl");
  process.env.RELEASE_NOTES_DIR = path.join(dir, "releases");
  botEvents = [];
  botUp = true;
  weekNet = 0.01;
  global.fetch = jest.fn(async (url) => {
    if (!botUp) throw new Error("connect ECONNREFUSED");
    if (url.startsWith("http://bot-btc:8081/api/events")) return Response.json({ events: botEvents });
    if (url.startsWith("http://bot-btc:8081/api/performance")) return Response.json({ net_pnl: weekNet, net_pnl_known: true });
    throw new Error(`unexpected ${url}`);
  });
  useTestAuth();
  ({ cookie, user, auth } = await signedIn("viewer", "user"));
});
afterEach(() => {
  jest.useRealTimers();
  jest.restoreAllMocks();
  delete process.env.BOT_APIS;
  delete process.env.ASSISTANT_REQUESTS_PATH;
  delete process.env.RELEASE_NOTES_DIR;
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Scenario: badge counts only what needs the person", () => {
  test("a survey is due and 3 bot updates are unread → the badge shows 1", async () => {
    surveyDue();
    botEvents = [stopLoss(10), stopLoss(11), ev(12, "risk_limit", { change: "im_over_limit" })];
    const view = await get();
    expect(view.needs_you).toBe(1);
    expect(view.items.filter((i) => i.group === "updates" && !i.read)).toHaveLength(3);
    expect(view.items.filter((i) => i.group === "needs_you").map((i) => i.kind)).toEqual(["survey"]);
  });

  test("a losing week postpones the survey, as the Ask button does (#446)", async () => {
    surveyDue();
    weekNet = -0.01;
    expect((await get()).needs_you).toBe(0);
  });

  test("a request that needs more is counted; its other statuses are updates", async () => {
    const file = process.env.ASSISTANT_REQUESTS_PATH;
    writeLines(file, [
      { id: "R-0001", ts: iso(5), user: "viewer", kind: "bug", fields: { title: "Chart gap" } },
      { id: "R-0002", ts: iso(5), user: "viewer", kind: "enhancement", fields: { title: "Dark PDF" } },
      { id: "R-0003", ts: iso(5), user: "viewer", kind: "bug", fields: { title: "Wrong label" } },
      { id: "R-0004", ts: iso(5), user: "viewer", kind: "bug", fields: { title: "Old one" } },
      { id: "R-0005", ts: iso(5), user: "someone", kind: "bug", fields: { title: "Not mine" } },
      { id: "R-0006", ts: iso(5), user: "viewer", kind: "bug", fields: { title: "Shipped one" } },
      { id: "R-0007", ts: iso(5), user: "viewer", kind: "bug", fields: { title: "Waiting" } },
    ]);
    writeLines(path.join(dir, "requests_status.jsonl"), [
      { id: "R-0001", status: "needs_info", note: "Which bot?", ts: iso(1) },
      { id: "R-0002", status: "filed", issue_url: "https://github.com/o/r/issues/512", ts: iso(2) },
      { id: "R-0003", status: "not_planned", note: "Out of scope for now.", ts: iso(3) },
      { id: "R-0005", status: "needs_info", ts: iso(1) },
      { id: "R-0006", status: "fixed", ts: iso(4) },
    ]);
    const view = await get();
    expect(view.needs_you).toBe(1);
    const titles = view.items.map((i) => i.title);
    expect(titles).toEqual([
      "Your request R-0001 needs more",
      "Your request R-0002 was accepted as public #512",
      "Your request R-0003 was declined: not planned",
      "Your request R-0006 has shipped",
    ]);
    expect(view.items[0].text).toMatch(/Which bot\?/);
    expect(view.items[1].href).toBe("https://github.com/o/r/issues/512");
    expect(view.items[2].text).toMatch(/Out of scope for now\./);
  });
});

describe("Scenario: a stop-loss appears with its reason", () => {
  test("Updates lists it with the event's plain sentence and the facts Why? asks for", async () => {
    botEvents = [stopLoss(42)];
    const [item] = (await get()).items;
    expect(item).toMatchObject({
      kind: "bot_event",
      group: "updates",
      read: false,
      title: "BTC · Stop-loss",
      text: "Bought back the put at the stop-loss: its price doubled.",
      why: { bot: "btc", seq: 42, kind: "close", instrument: "BTC-27NOV26-60000-P" },
    });
  });

  test.each([
    ["a delta exit", ev(1, "closed", { close_reason: "delta_exit" }), "BTC · Delta exit"],
    ["the kill switch", ev(1, "closed", { close_reason: "kill_switch" }), "BTC · Kill switch"],
    ["a sell cancelled by a freeze", ev(1, "cancelled", { trigger_reason: "risk_frozen" }), "BTC · New risk frozen"],
    ["IM over the limit", ev(1, "risk_limit", { change: "im_over_limit" }), "BTC · Margin over the limit"],
  ])("%s is a bot update", async (_n, event, title) => {
    botEvents = [event];
    const [item] = (await get()).items;
    expect(item.title).toBe(title);
    // Not a close: Why? names the event itself.
    if (event.event !== "closed") expect(item.why.question).toMatch(/^Why did BTC record/);
  });

  test("only the page's bot: no ?bot= lists no bot update", async () => {
    botEvents = [stopLoss(1)];
    expect((await get({ bot: null })).items).toEqual([]);
  });
});

describe("Scenario: no fills", () => {
  test("20 fills (and skips, cancels of other causes, take-profits) create no notification", async () => {
    botEvents = [
      ...Array.from({ length: 20 }, (_, i) => ev(i + 1, "filled", { instrument: "BTC-27NOV26-90000-C" })),
      ev(30, "skipped", { skip_reason: "risk_frozen" }),
      ev(31, "cancelled", { trigger_reason: "fill_timeout" }),
      ev(32, "closed", { close_reason: "roi_target" }),
      ev(33, "risk_limit", { change: "frozen" }),
    ];
    const view = await get();
    expect(view.items).toEqual([]);
    expect(view.needs_you).toBe(0);
  });
});

describe("Scenario: read on one device, read on all", () => {
  test("marking all read on the laptop leaves the phone with no unread item", async () => {
    surveyDue();
    botEvents = [stopLoss(1), stopLoss(2)];
    const res = await post({ action: "read_all" });
    expect(res.status).toBe(200);
    expect((await res.json()).needs_you).toBe(0);
    // The phone: another session of the same person.
    const phone = await signedIn("viewer", "user");
    const view = await get({ cookie: phone.cookie });
    expect(view.items).toHaveLength(3);
    expect(view.items.filter((i) => !i.read)).toEqual([]);
    expect(view.needs_you).toBe(0);
  });

  test("one item read; another person's list is untouched", async () => {
    botEvents = [stopLoss(1), stopLoss(2)];
    const view = await (await post({ action: "read", ids: ["bot:btc:1"] })).json();
    expect(view.items.map((i) => [i.id, i.read])).toEqual([
      ["bot:btc:2", false],
      ["bot:btc:1", true],
    ]);
    const other = await signedIn("someone", "manager");
    expect((await get({ cookie: other.cookie })).items.every((i) => !i.read)).toBe(true);
  });

  test("read marks older than 90 days are pruned", async () => {
    auth.store.db.run("INSERT INTO notification_reads (user_id, item_id, read_at) VALUES (?, ?, ?)", [user.id, "bot:btc:7", nowSec - 91 * DAY]);
    await post({ action: "read", ids: ["bot:btc:8"] });
    expect([...auth.store.notificationReads(user.id)]).toEqual(["bot:btc:8"]);
  });
});

describe("rules", () => {
  test("items older than 90 days are not listed", async () => {
    botEvents = [ev(1, "closed", { close_reason: "stop_loss" }, "old", 91), ev(2, "closed", { close_reason: "stop_loss" }, "new", 89)];
    expect((await get()).items.map((i) => i.id)).toEqual(["bot:btc:2"]);
  });

  test("an Updates kind turned off is not listed, on every device; turned on again it is", async () => {
    botEvents = [stopLoss(1)];
    const off = await (await post({ action: "setting", kind: "bot_event", enabled: false })).json();
    expect(off.items).toEqual([]);
    expect(off.settings).toEqual({ bot_event: false, request_status: true, whats_new: true });
    const phone = await signedIn("viewer", "user");
    expect((await get({ cookie: phone.cookie })).items).toEqual([]);
    expect((await (await post({ action: "setting", kind: "bot_event", enabled: true })).json()).items).toHaveLength(1);
  });

  test.each(["survey", "request_needs_info", "nonsense"])("the kind %s cannot be turned off → 400", async (kind) => {
    const res = await post({ action: "setting", kind, enabled: false });
    expect(res.status).toBe(400);
    expect((await res.json()).error).toMatch(/Needs you cannot be turned off/);
  });

  test("every role gets every kind", async () => {
    botEvents = [stopLoss(1)];
    for (const role of ["admin", "manager", "user"]) {
      const p = await signedIn(`p-${role}`, role);
      expect((await get({ cookie: p.cookie })).items.map((i) => i.kind)).toEqual(["bot_event"]);
    }
  });

  test("a bot that does not answer: its events are named unreadable, the rest is listed", async () => {
    fs.mkdirSync(process.env.RELEASE_NOTES_DIR);
    fs.writeFileSync(path.join(process.env.RELEASE_NOTES_DIR, "v0.3.0.md"), "---\ndate: 2026-10-27\ntitle: The bell\n---\nA bell in the menu bar.\n");
    botUp = false;
    const view = await get();
    expect(view.errors).toEqual({ bot_events: "bot btc is unreachable" });
    expect(view.items.map((i) => i.title)).toEqual(["What's new in v0.3.0: The bell"]);
  });

  test("a user deleted takes their read marks and settings with them", async () => {
    await post({ action: "read", ids: ["bot:btc:1"] });
    await post({ action: "setting", kind: "whats_new", enabled: false });
    auth.store.deleteUser(user.id);
    expect(auth.store.all("SELECT * FROM notification_reads")).toEqual([]);
    expect(auth.store.all("SELECT * FROM notification_settings")).toEqual([]);
  });
});

describe("refused requests", () => {
  test("signed out → 401", async () => {
    expect((await GET(req("GET", undefined, { cookie: "" }))).status).toBe(401);
  });

  test("cross-origin → 403, nothing written", async () => {
    expect((await post({ action: "read_all" }, { origin: "http://evil" })).status).toBe(403);
    expect(auth.store.notificationReads(user.id).size).toBe(0);
  });

  test("not JSON → 415", async () => {
    expect((await post({ action: "read_all" }, { "content-type": "text/plain" })).status).toBe(415);
  });

  test.each([
    ["no action", {}, /action/],
    ["an unknown action", { action: "delete" }, /action/],
    ["no ids", { action: "read", ids: [] }, /ids/],
    ["an id that is not one", { action: "read", ids: ["<script>"] }, /ids/],
    ["201 ids", { action: "read", ids: Array.from({ length: 201 }, (_, i) => `bot:btc:${i}`) }, /ids/],
    ["enabled not a boolean", { action: "setting", kind: "bot_event", enabled: "no" }, /enabled/],
  ])("%s → 400", async (_n, body, error) => {
    const res = await post(body);
    expect(res.status).toBe(400);
    expect((await res.json()).error).toMatch(error);
  });
});

describe("release notes (docs/product/releases/<tag>.md)", () => {
  test("a note is its tag, date, title and a short body", () => {
    expect(parseReleaseNote("v0.3.0.md", "---\ndate: 2026-10-27\ntitle: The bell\n---\nA bell\nin the menu bar.\n")).toEqual({
      tag: "v0.3.0",
      date: "2026-10-27T00:00:00.000Z",
      title: "The bell",
      body: "A bell in the menu bar.",
    });
  });

  test.each([
    ["README.md", "---\ndate: 2026-10-27\ntitle: x\n---\nbody"],
    ["v0.3.0.md", "no front matter"],
    ["v0.3.0.md", "---\ntitle: x\n---\nbody"],
    ["v0.3.0.md", "---\ndate: 2026-13-45\ntitle: x\n---\nbody"],
    ["v0.3.0.md", "---\ndate: 2026-10-27\n---\nbody"],
    ["v0.3.0.md", "---\ndate: 2026-10-27\ntitle: x\n---\n"],
  ])("%s %j is not a note", (file, text) => {
    expect(parseReleaseNote(file, text)).toBeNull();
  });

  test("no folder → no notes; notes newest first", () => {
    expect(readReleaseNotes(path.join(dir, "none"))).toEqual([]);
    const d = path.join(dir, "notes");
    fs.mkdirSync(d);
    fs.writeFileSync(path.join(d, "v0.1.0.md"), "---\ndate: 2026-09-01\ntitle: One\n---\nFirst.");
    fs.writeFileSync(path.join(d, "v0.2.0.md"), "---\ndate: 2026-10-01\ntitle: Two\n---\nSecond.");
    expect(readReleaseNotes(d).map((n) => n.tag)).toEqual(["v0.2.0", "v0.1.0"]);
  });
});
