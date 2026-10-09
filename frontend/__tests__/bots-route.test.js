/**
 * @jest-environment node
 */
import { parseBots, pollIntervalMs } from "@/lib/bots";
import { GET as listBots } from "@/app/api/bots/route";
import { GET as proxy } from "@/app/api/bots/[bot]/[...path]/route";

const call = (bot, path, search = "") =>
  proxy(new Request(`http://monitor/api/bots/${bot}/${path.join("/")}${search}`), { params: Promise.resolve({ bot, path }) });

describe("bot configuration", () => {
  test("parses name=url pairs and drops malformed entries", () => {
    expect(parseBots("btc=http://bot-btc:8081/, ETH = http://bot-eth:8082,bad,=http://x,x=ftp://y")).toEqual([
      { name: "btc", url: "http://bot-btc:8081" },
      { name: "eth", url: "http://bot-eth:8082" },
    ]);
  });
});

describe("poll interval", () => {
  test("defaults to 1 s and is clamped to a sane range", () => {
    expect(pollIntervalMs(undefined)).toBe(1000);
    expect(pollIntervalMs("5000")).toBe(5000);
    expect(pollIntervalMs("10")).toBe(500);
    expect(pollIntervalMs("999999")).toBe(60000);
    expect(pollIntervalMs("abc")).toBe(1000);
  });
});

describe("read-only proxy", () => {
  const realFetch = global.fetch;
  beforeEach(() => {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
  });
  afterEach(() => {
    global.fetch = realFetch;
  });

  test("lists bots by name only", async () => {
    const body = await (await listBots()).json();
    expect(body).toEqual({ bots: [{ name: "btc" }], pollMs: 1000 });
  });

  test("forwards allow-listed endpoints with the query string", async () => {
    global.fetch = jest.fn().mockResolvedValue(new Response('{"events":[]}', { status: 200 }));
    const res = await call("btc", ["events"], "?since=5");
    expect(global.fetch).toHaveBeenCalledWith("http://bot-btc:8081/api/events?since=5", expect.objectContaining({ cache: "no-store" }));
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ events: [] });
  });

  test("forwards the P&L history endpoint", async () => {
    global.fetch = jest.fn().mockResolvedValue(new Response('{"points":[]}', { status: 200 }));
    const res = await call("btc", ["pnl", "history"], "?range=1w");
    expect(global.fetch).toHaveBeenCalledWith("http://bot-btc:8081/api/pnl/history?range=1w", expect.anything());
    expect(res.status).toBe(200);
  });

  test("refuses unknown bots and endpoints", async () => {
    global.fetch = jest.fn();
    expect((await call("eth", ["status"])).status).toBe(404);
    expect((await call("btc", ["kill"])).status).toBe(404);
    expect((await call("btc", ["status", "extra"])).status).toBe(404);
    expect(global.fetch).not.toHaveBeenCalled();
  });

  test("reports an unreachable bot as 502", async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error("ECONNREFUSED"));
    const res = await call("btc", ["status"]);
    expect(res.status).toBe(502);
    expect((await res.json()).error).toMatch(/unreachable/);
  });
});

describe("manual close proxy", () => {
  const { POST: close } = require("@/app/api/bots/[bot]/close/route");
  const realFetch = global.fetch;
  const post = (bot, body, headers = {}) =>
    close(
      new Request(`http://localhost:3000/api/bots/${bot}/close`, {
        method: "POST",
        headers: { "content-type": "application/json", origin: "http://localhost:3000", host: "localhost:3000", ...headers },
        body: JSON.stringify(body),
      }),
      { params: Promise.resolve({ bot }) },
    );
  beforeEach(() => {
    process.env.BOT_APIS = "btc=http://bot-btc:8081";
    process.env.BOT_ADMIN_TOKEN = "s3cret";
  });
  afterEach(() => {
    global.fetch = realFetch;
    delete process.env.BOT_ADMIN_TOKEN;
  });

  test("forwards to the bot with the token, which never reaches the browser", async () => {
    global.fetch = jest.fn().mockResolvedValue(new Response('{"results":[]}', { status: 200 }));
    const res = await post("btc", { position_ids: ["p-2"] });
    expect(res.status).toBe(200);
    expect(global.fetch).toHaveBeenCalledWith(
      "http://bot-btc:8081/api/positions/close",
      expect.objectContaining({ method: "POST", headers: expect.objectContaining({ Authorization: "Bearer s3cret" }) }),
    );
    expect(await res.text()).not.toContain("s3cret");
  });

  test("refuses without a token, cross-origin, or with bad ids", async () => {
    global.fetch = jest.fn();
    expect((await post("btc", { position_ids: ["p"] }, { origin: "http://evil.example" })).status).toBe(403);
    expect((await post("btc", { position_ids: [] })).status).toBe(400);
    expect((await post("nope", { position_ids: ["p"] })).status).toBe(404);
    delete process.env.BOT_ADMIN_TOKEN;
    expect((await post("btc", { position_ids: ["p"] })).status).toBe(503);
    expect(global.fetch).not.toHaveBeenCalled();
  });
});
