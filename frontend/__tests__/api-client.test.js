import { closePositions, rebalanceBot } from "@/lib/api/actions";
import { fetchBotPerformance, fetchMonitorConfig } from "@/lib/api/bots";
import { getJSON, sendJSON } from "@/lib/api/http";
import { createUser } from "@/lib/api/users";

// #466: the API client split by domain over one shared fetch (lib/api/http.js).

const reply = (status, body) => ({
  ok: status >= 200 && status < 300,
  status,
  headers: { get: () => null },
  json: body === undefined ? () => Promise.reject(new SyntaxError("not JSON")) : () => Promise.resolve(body),
});

beforeEach(() => {
  global.fetch = jest.fn();
});

describe("sendJSON", () => {
  test("sends JSON with the method and returns the body", async () => {
    fetch.mockResolvedValue(reply(200, { ok: true }));
    await expect(sendJSON("/api/x", "PATCH", { a: 1 })).resolves.toEqual({ ok: true });
    expect(fetch).toHaveBeenCalledWith("/api/x", {
      method: "PATCH",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ a: 1 }),
    });
  });

  test.each([
    ["the server's reason", reply(409, { error: "frozen" }), "frozen"],
    ["the status when the body is not JSON", reply(502), "502"],
  ])("throws with %s", async (_, res, message) => {
    fetch.mockResolvedValue(res);
    await expect(sendJSON("/api/x", "POST", {})).rejects.toThrow(message);
  });

  test("an empty answer is null", async () => {
    fetch.mockResolvedValue(reply(204));
    await expect(sendJSON("/api/x", "POST", {})).resolves.toBeNull();
  });
});

test("getJSON throws with the server's reason", async () => {
  fetch.mockResolvedValue(reply(404, { error: "not in recent history" }));
  await expect(getJSON("/api/x")).rejects.toThrow("not in recent history");
});

test("the bot calls keep their paths", async () => {
  fetch.mockResolvedValue(reply(200, { bots: [{ name: "btc" }] }));
  await expect(fetchMonitorConfig()).resolves.toEqual({ names: ["btc"], pollMs: 1000 });
  fetch.mockResolvedValue(reply(200, {}));
  await fetchBotPerformance("b t", "7d");
  expect(fetch).toHaveBeenLastCalledWith("/api/bots/b%20t/performance?range=7d", expect.anything());
});

test("the actions post to the bot's proxy and return its answer", async () => {
  fetch.mockResolvedValue(reply(200, { results: [{ id: "p1" }] }));
  await expect(closePositions("btc", ["p1"])).resolves.toEqual([{ id: "p1" }]);
  expect(fetch).toHaveBeenLastCalledWith("/api/bots/btc/close", expect.objectContaining({ method: "POST", body: '{"position_ids":["p1"]}' }));
  fetch.mockResolvedValue(reply(200, { dry_run: true }));
  await expect(rebalanceBot("btc", true)).resolves.toEqual({ dry_run: true });
  fetch.mockResolvedValue(reply(429, { error: "cooldown" }));
  await expect(rebalanceBot("btc", false)).rejects.toThrow("cooldown");
});

test("an admin call with an empty answer does not crash", async () => {
  fetch.mockResolvedValue(reply(200));
  await expect(createUser({ username: "ann" })).resolves.toBeUndefined();
});
