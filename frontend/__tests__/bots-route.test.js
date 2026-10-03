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
