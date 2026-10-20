/**
 * @jest-environment node
 */
import { POST } from "@/app/api/bots/[bot]/rebalance/route";
import { signedIn, useTestAuth } from "@/test/auth";

let cookie;
const post = (bot, body, headers = {}) =>
  POST(
    new Request(`http://monitor/api/bots/${bot}/rebalance`, {
      method: "POST",
      headers: { origin: "http://monitor", host: "monitor", "content-type": "application/json", cookie, ...headers },
      body: JSON.stringify(body),
    }),
    { params: Promise.resolve({ bot }) },
  );

describe("rebalance proxy", () => {
  const realFetch = global.fetch;
  beforeEach(async () => {
    process.env.BOT_APIS = "eth=http://bot-eth:8082";
    process.env.BOT_ADMIN_TOKEN = "s3cret";
    useTestAuth();
    cookie = (await signedIn()).cookie;
  });

  test("refuses without a signed-in session, before reaching the bot", async () => {
    global.fetch = jest.fn();
    expect((await post("eth", { dry_run: true }, { cookie: "" })).status).toBe(401);
    expect((await post("eth", { dry_run: true }, { cookie: "access_token=forged.token.here" })).status).toBe(401);
    expect(global.fetch).not.toHaveBeenCalled();
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
      expect.objectContaining({
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: "Bearer s3cret" },
        body: JSON.stringify({ actor: { user: "admin", role: "admin" } }),
      }),
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
