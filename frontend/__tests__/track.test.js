import { resetTracking, SESSION_IDLE_MS, setAnalyticsConsent, track } from "@/lib/track";

const realFetch = global.fetch;
beforeEach(() => {
  resetTracking();
  global.fetch = jest.fn().mockResolvedValue({ ok: true, status: 204 });
});
afterEach(() => {
  global.fetch = realFetch;
});

const sent = () => global.fetch.mock.calls.map(([, init]) => JSON.parse(init.body));

describe("Scenario: opt-out", () => {
  test("a person who opted out: track() sends nothing", () => {
    setAnalyticsConsent(false);
    expect(track("screen_viewed", { screen: "overview" })).toBe(false);
    expect(global.fetch).not.toHaveBeenCalled();
  });

  test("a person not asked yet: track() sends nothing", () => {
    expect(track("screen_viewed", { screen: "overview" })).toBe(false);
    expect(global.fetch).not.toHaveBeenCalled();
  });

  test("opting out after saying yes stops the very next event", () => {
    setAnalyticsConsent(true);
    track("screen_viewed", { screen: "overview" });
    setAnalyticsConsent(false);
    track("panel_expanded", { panel: "held" });
    expect(sent().map((b) => b.event)).toEqual(["screen_viewed"]);
  });
});

describe("track() after yes", () => {
  beforeEach(() => setAnalyticsConsent(true));

  test("posts the event to the monitor's own route, same origin", () => {
    expect(track("panel_expanded", { panel: "held" })).toBe(true);
    const [url, init] = global.fetch.mock.calls[0];
    expect(url).toBe("/api/events");
    expect(init).toEqual(expect.objectContaining({ method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" } }));
    expect(sent()[0]).toEqual({ event: "panel_expanded", session_id: expect.stringMatching(/^[0-9a-f]{32}$/), props: { panel: "held" } });
  });

  test("an event outside the plan never leaves the page", () => {
    expect(track("panel_expanded", { panel: "held", qty: 2 })).toBe(false);
    expect(track("tip_opened", { tip_id: "tip.strike_90000" })).toBe(false);
    expect(track("unknown_event")).toBe(false);
    expect(global.fetch).not.toHaveBeenCalled();
  });

  test("one session id until 30 min without an event", () => {
    const t0 = Date.UTC(2026, 9, 22, 12);
    track("screen_viewed", {}, t0);
    track("screen_viewed", {}, t0 + SESSION_IDLE_MS);
    track("screen_viewed", {}, t0 + 2 * SESSION_IDLE_MS + 1);
    const [a, b, c] = sent().map((x) => x.session_id);
    expect(a).toBe(b);
    expect(c).not.toBe(b);
  });

  test("a failed or throwing fetch never breaks the page", async () => {
    global.fetch = jest.fn().mockRejectedValue(new Error("offline"));
    expect(track("screen_viewed")).toBe(true);
    global.fetch = jest.fn(() => {
      throw new Error("no fetch");
    });
    expect(track("screen_viewed")).toBe(false);
  });
});
