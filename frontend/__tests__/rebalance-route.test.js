/**
 * @jest-environment node
 */
import { POST } from "@/app/api/bots/[bot]/rebalance/route";

const post = (bot, body, headers = {}) =>
  POST(
    new Request(`http://monitor/api/bots/${bot}/rebalance`, {
      method: "POST",
      headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", ...headers },
      body: JSON.stringify(body),
    }),
    { params: Promise.resolve({ bot }) },
  );

describe("rebalance proxy", () => {
  const realFetch = global.fetch;
  beforeEach(() => {
    process.env.BOT_APIS = "eth=http://bot-eth:8082";
    process.env.BOT_ADMIN_TOKEN = "s3cret";
  });
  afterEach(() => {
    global.fetch = realFetch;
    delete process.env.BOT_ADMIN_TOKEN;
  });

  test("forwards dry_run with the token kept on the server", async () => {
    global.fetch = jest.fn().mockResolvedValue(new Response('{"dry_run":true,"actions":[]}', { status: 200 }));
    const res = await post("eth", { dry_run: true });
    expect(res.status).toBe(200);
    expect(global.fetch).toHaveBeenCalledWith(
      "http://bot-eth:8082/api/rebalance?dry_run=true",
      expect.objectContaining({ method: "POST", headers: { Authorization: "Bearer s3cret" } }),
    );
  });

  test("passes the bot's status and Retry-After through", async () => {
    global.fetch = jest.fn().mockResolvedValue(new Response('{"error":"try again in 4m0s"}', { status: 429, headers: { "Retry-After": "240" } }));
    const res = await post("eth", { dry_run: false });
    expect(res.status).toBe(429);
    expect(res.headers.get("retry-after")).toBe("240");
  });

  test("refuses cross-origin, non-JSON, bad bodies, unknown bots and a missing token", async () => {
    global.fetch = jest.fn();
    expect((await post("eth", { dry_run: true }, { origin: "http://evil" })).status).toBe(403);
    expect((await post("eth", { dry_run: true }, { "content-type": "text/plain" })).status).toBe(415);
    expect((await post("eth", { dry_run: "yes" })).status).toBe(400);
    expect((await post("btc", { dry_run: true })).status).toBe(404);
    delete process.env.BOT_ADMIN_TOKEN;
    expect((await post("eth", { dry_run: true })).status).toBe(503);
    expect(global.fetch).not.toHaveBeenCalled();
  });
});
