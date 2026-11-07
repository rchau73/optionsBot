import { render, renderHook, screen, waitFor, within } from "@testing-library/react";
import HowItWorks from "@/components/HowItWorks";
import { HowItWorksScreen } from "@/components/HowItWorksView";
import { useBotHedge } from "@/hooks/useBotHedge";
import { bandSize, deltaFigures, deltaWords, hedgeStory, marketTurnsWords, orderCapWords } from "@/lib/howitworksHedge";

// "How it works" explains the futures hedge (#696). The shape GET
// /api/bots/<bot>/hedge serves (internal/strategy/futureshedgeview.go).
const NOW = Date.parse("2026-11-06T12:00:30Z");

const hedge = (over = {}) => ({
  available: true,
  enabled: true,
  testnet: true,
  kill_switch: false,
  halted: false,
  underlying: "BTC",
  instrument: "BTC-PERPETUAL",
  perp_usd: -17700,
  perp_delta_coin: -0.2305,
  options_delta_coin: 0.2612,
  net_delta_coin: 0.0307,
  band_coin: 0.0578,
  band_pct: 5,
  orders_last_hour: 1,
  max_orders_per_hour: 12,
  perp_as_of: "2026-11-06T12:00:00Z",
  ...over,
});

const off = (over = {}) => hedge({ enabled: false, perp_usd: 0, perp_delta_coin: 0, net_delta_coin: 0.2612, band_coin: 0, ...over });
const paused = (over = {}) => hedge({ halted: true, halt_reason: "the exchange is in maintenance", not_holding: "the exchange is in maintenance", ...over });

const summary = { underlying: "BTC", environment: "testnet", holding: { legs: 0, coin: "BTC" }, strangle: null };

const page = (reading) => render(<HowItWorks summary={summary} hedge={reading ?? { hedge: hedge(), error: null, at: NOW }} />);
const picture4 = () => screen.getByRole("region", { name: "The safety net: a futures hedge" });
const defences = () => screen.getByRole("region", { name: "How the bot defends you before the loss zone" });
const marketTurns = () => within(defences()).getByRole("heading", { name: "Market turns" }).parentElement;

describe("Scenario: hedge ON", () => {
  test("four pictures, the deltas and the band from the API, both legs kept", () => {
    const { container } = page();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("in four pictures");
    expect(screen.getByText(/On this account it also uses a perpetual/)).toBeInTheDocument();
    const p = picture4();
    expect(within(p).getByText("ON")).toBeInTheDocument();
    expect(p).toHaveTextContent("testnet experiment");
    const line = "Options' delta +0.2612 BTC; perpetual −0.2305 BTC; net +0.0307 BTC, inside the hedge band of ±0.0578 BTC.";
    expect(p).toHaveTextContent(line);
    expect(within(p).getByRole("img")).toHaveAttribute("aria-label", `Delta in BTC. ${line}`);
    for (const el of ['[data-bar="options"]', '[data-bar="perpetual"]', '[data-dot="net"]', '[data-zone="band"]']) expect(container.querySelector(el)).not.toBeNull();
    expect(marketTurns()).toHaveTextContent("the bot keeps both legs");
  });

  test("the bars read from zero, the perpetual's the other way, and the band is centred", () => {
    const { container } = page();
    const num = (sel, attr) => Number(container.querySelector(sel).getAttribute(attr));
    const zero = num('[data-bar="options"]', "x");
    expect(num('[data-bar="perpetual"]', "x") + num('[data-bar="perpetual"]', "width")).toBeCloseTo(zero, 6);
    expect(num('[data-zone="band"]', "x") + num('[data-zone="band"]', "width") / 2).toBeCloseTo(zero, 6);
    expect(num('[data-dot="net"]', "cx")).toBeGreaterThan(zero);
  });

  test("the cards hold the API's band and order cap, never typed ones", () => {
    page({ hedge: hedge({ band_coin: 0.1234, band_pct: 7, max_orders_per_hour: 9 }), error: null, at: NOW });
    expect(picture4()).toHaveTextContent("(±0.1234 BTC, 7 % of the margin balance)");
    expect(picture4()).toHaveTextContent("at most 9 orders an hour");
    expect(picture4()).toHaveTextContent("removes the lean, not the curve");
    expect(within(picture4()).getByRole("heading", { name: /What it does not do/ })).toContainElement(screen.getAllByLabelText("loss").at(-1));
    expect(picture4()).toHaveTextContent("Testnet only for now");
  });

  test("a net past the band says outside; a band not known says so", () => {
    const { rerender } = page({ hedge: hedge({ net_delta_coin: -0.09 }), error: null, at: NOW });
    expect(picture4()).toHaveTextContent("net −0.0900 BTC, outside the hedge band");
    rerender(<HowItWorks summary={summary} hedge={{ hedge: hedge({ band_coin: 0 }), error: null, at: NOW }} />);
    expect(picture4()).toHaveTextContent("the hedge band is unknown right now");
    expect(picture4().querySelector('[data-zone="band"]')).toBeNull();
    expect(picture4()).not.toHaveTextContent("±0.0000");
  });

  test("an old reading of the perpetual says it is not updating", () => {
    page({ hedge: hedge(), error: null, at: NOW + 600000 });
    expect(picture4()).toHaveTextContent("Not updating: the perpetual was last read 10m ago.");
  });
});

describe("Scenario: hedge OFF", () => {
  test("the OFF badge, no perpetual bar, the GEX shed and Account", () => {
    const { container } = page({ hedge: off(), error: null, at: NOW });
    expect(within(picture4()).getByText("OFF")).toBeInTheDocument();
    expect(container.querySelector('[data-bar="perpetual"]')).toBeNull();
    expect(container.querySelector('[data-bar="options"]')).not.toBeNull();
    expect(picture4()).toHaveTextContent("no perpetual held");
    expect(picture4()).toHaveTextContent("What it replaces");
    expect(marketTurns()).toHaveTextContent("The GEX shed");
    expect(marketTurns()).toHaveTextContent("turn the futures hedge on in Account");
    expect(screen.queryByText(/On this account it also uses a perpetual/)).toBeNull();
  });

  test("off with a position left open: the sentence, the bar and the warning badge", () => {
    const { container } = page({ hedge: off({ perp_usd: -500, perp_delta_coin: -0.0065 }), error: null, at: NOW });
    expect(screen.getByText(/On this account it also uses a perpetual/)).toBeInTheDocument();
    expect(within(picture4()).getByText("OFF · position still open")).toBeInTheDocument();
    expect(container.querySelector('[data-bar="perpetual"]')).not.toBeNull();
  });

  test.each([
    ["a live bot", { testnet: false }],
    ["a bot with no futures connection", { available: false }],
  ])("%s is not sent to a switch it cannot use", (_, over) => {
    page({ hedge: off(over), error: null, at: NOW });
    expect(marketTurns()).toHaveTextContent("cannot be turned on for this bot");
  });

  test("nothing held: says so, no picture, no zeros", () => {
    page({ hedge: off({ options_delta_coin: 0, net_delta_coin: 0 }), error: null, at: NOW });
    expect(picture4()).toHaveTextContent("holds nothing to hedge");
    expect(within(picture4()).queryByRole("img")).toBeNull();
    expect(picture4()).not.toHaveTextContent("0.0000 BTC");
  });
});

describe("Scenario: hedge paused", () => {
  test("PAUSED, and the shed protects the book until the hedge can trade", () => {
    page({ hedge: paused(), error: null, at: NOW });
    expect(within(picture4()).getByText("PAUSED")).toBeInTheDocument();
    expect(marketTurns()).toHaveTextContent("the GEX shed protects the book until the hedge can trade");
    expect(marketTurns()).toHaveTextContent("the exchange is in maintenance");
    expect(marketTurns()).not.toHaveTextContent("keeps both legs");
  });

  test("on but not holding is the same story: both legs are not kept", () => {
    page({ hedge: hedge({ not_holding: "its last order was rejected" }), error: null, at: NOW });
    expect(within(picture4()).getByText("NOT HOLDING")).toBeInTheDocument();
    expect(marketTurns()).toHaveTextContent("the GEX shed protects the book until the hedge can trade");
    expect(marketTurns()).not.toHaveTextContent("keeps both legs");
  });
});

describe("Scenario: hedge data unavailable", () => {
  test("the explanation without numbers, and that the live figures are unavailable", () => {
    const { container } = page({ hedge: null, error: "bot btc is unreachable", at: NOW });
    expect(picture4()).toHaveTextContent("Live figures unavailable: bot btc is unreachable.");
    expect(picture4()).toHaveTextContent("What it does");
    expect(picture4()).toHaveTextContent("a limited number of orders an hour");
    expect(picture4()).not.toHaveTextContent(/undefined|NaN|±|ON\b/);
    expect(within(picture4()).queryByRole("img")).toBeNull();
    expect(container).toHaveTextContent("in four pictures");
    expect(marketTurns()).toHaveTextContent("With the futures hedge on, both legs stay");
    expect(screen.queryByText(/On this account it also uses a perpetual/)).toBeNull();
  });

  test("before the first answer it says not yet, never a state", () => {
    render(<HowItWorks summary={summary} />);
    expect(picture4()).toHaveTextContent("Not yet: reading the hedge's live figures");
    expect(within(picture4()).queryByText("OFF")).toBeNull();
  });

  test("the page shell hands the reading to the picture", () => {
    render(<HowItWorksScreen bot="btc" summary={summary} hedge={{ hedge: hedge(), error: null, at: NOW }} />);
    expect(within(picture4()).getByText("ON")).toBeInTheDocument();
  });
});

describe("the kill switch", () => {
  test("neither the hedge nor the shed acts", () => {
    page({ hedge: hedge({ kill_switch: true }), error: null, at: NOW });
    expect(within(picture4()).getByText("KILL SWITCH")).toBeInTheDocument();
    expect(marketTurns()).toHaveTextContent("neither the hedge nor the GEX shed acts");
  });
});

describe("lib/howitworksHedge", () => {
  test("hedgeStory reads every state", () => {
    expect(hedgeStory(null)).toBeNull();
    expect(hedgeStory(hedge())).toBe("on");
    expect(hedgeStory(paused())).toBe("paused");
    expect(hedgeStory(hedge({ not_holding: "x" }))).toBe("not_holding");
    expect(hedgeStory(off())).toBe("off");
    expect(hedgeStory(off({ not_holding: "x" }))).toBe("off");
    expect(hedgeStory(off({ perp_usd: 1 }))).toBe("off_open");
    expect(hedgeStory(hedge({ kill_switch: true }))).toBe("idle");
  });

  test("deltaFigures: nothing to draw, a dust perpetual, the net when the API has none", () => {
    expect(deltaFigures(null)).toBeNull();
    expect(deltaFigures(hedge({ options_delta_coin: undefined }))).toBeNull();
    expect(deltaFigures(off({ options_delta_coin: 0 }))).toBeNull();
    expect(deltaFigures(off({ perp_usd: 0.2, perp_delta_coin: 0.00001 })).perp).toBeNull();
    expect(deltaFigures(hedge({ net_delta_coin: undefined })).net).toBeCloseTo(0.0307, 6);
    const f = deltaFigures(hedge({ band_coin: 1 }));
    expect(f.scale).toBeCloseTo(1.1, 6);
    expect(deltaWords(deltaFigures(off()), "ETH")).toContain("no perpetual held");
  });

  test("the numbers' words fall back without a figure", () => {
    expect(orderCapWords(null)).toBe("a limited number of orders an hour");
    expect(orderCapWords({ max_orders_per_hour: 12 })).toBe("at most 12 orders an hour");
    expect(bandSize(null, "BTC")).toBe("");
    expect(bandSize({ band_coin: 0 }, "BTC")).toBe("");
    expect(bandSize({ band_coin: 0.05 }, "ETH")).toBe(" (±0.0500 ETH)");
    expect(marketTurnsWords(null)).toContain("The GEX shed");
  });
});

describe("useBotHedge", () => {
  const reply = (status, body) => ({ ok: status < 300, status, json: async () => body });
  afterEach(() => {
    delete global.fetch;
  });

  test("reads the bot's hedge; a failed read keeps the error and no figures", async () => {
    global.fetch = jest.fn().mockResolvedValue(reply(200, { hedge: hedge() }));
    const { result, unmount } = renderHook(() => useBotHedge("btc"));
    expect(result.current).toEqual({ hedge: null, error: null, at: null });
    await waitFor(() => expect(result.current.hedge?.instrument).toBe("BTC-PERPETUAL"));
    expect(global.fetch.mock.calls[0][0]).toBe("/api/bots/btc/hedge");
    unmount();

    global.fetch = jest.fn().mockResolvedValue(reply(502, { error: "bot btc is unreachable" }));
    const failed = renderHook(() => useBotHedge("btc"));
    await waitFor(() => expect(failed.result.current.error).toBe("bot btc is unreachable"));
    expect(failed.result.current.hedge).toBeNull();
    failed.unmount();
  });

  test("no bot chosen: nothing is fetched", () => {
    global.fetch = jest.fn();
    renderHook(() => useBotHedge(null));
    expect(global.fetch).not.toHaveBeenCalled();
  });
});
