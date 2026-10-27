import { fireEvent, render, screen, within } from "@testing-library/react";
import Monitor from "@/components/Monitor";
import PayoffCard from "@/components/PayoffCard";
import { PositionsScreen } from "@/components/PositionsPage";
import { useMonitor } from "@/hooks/useMonitor";
import { fetchBotState } from "@/lib/api";
import { ALLOWED_ENDPOINTS } from "@/lib/bots";
import { cardTitle, chartGeometry, chartLines, deltaLine, earnedLine, missingLine, nowLabel, textAlternative, todayZones } from "@/lib/payoff";

// The payoff charts (#391): the Positions page, the overview's cards and the
// modal opened from a leg. The poller is mocked: Monitor reads its bots from
// useMonitor; the shapes are those of GET /api/strangles/payoff
// (internal/api/payoff.go).
jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/components/PnlChart", () => () => null);
jest.mock("@/components/CoinPnlChart", () => () => null);

/** A coin-settled short strangle's curves at a few prices. */
function points(put, call, premium, today) {
  const out = [];
  for (let s = 60000; s <= 110000; s += 2500) {
    const expiry = premium - Math.max(put - s, 0) / s - Math.max(s - call, 0) / s;
    out.push({ price: s, expiry_pnl: expiry, today_pnl: today ? expiry - 0.004 : null });
  }
  return out;
}

const leg = (type, strike, extra = {}) => ({
  position_id: `${type}-${strike}`,
  instrument: `BTC-27NOV26-${strike}-${type === "put" ? "P" : "C"}`,
  option_type: type,
  strike,
  qty: 1,
  premium_coin: 0.0047,
  mark: 0.0018,
  iv: 0.5,
  delta: type === "put" ? -0.1 : 0.2,
  unrealised_coin: 0.0029,
  position_delta: type === "put" ? 0.1 : -0.2,
  position_delta_usd_per_1pct: type === "put" ? 82.4 : -164.8,
  stop_loss_mark: 0.0141,
  exit_line: type === "put" ? 77000 : 88000,
  stop_level: type === "put" ? 70000 : 95000,
  ...extra,
});

const strangle = (id, dte, extra = {}) => ({
  strangle_id: id,
  slot: { dte, delta: 0.16 },
  expiry: dte === 25 ? "2026-11-27T08:00:00Z" : "2026-12-25T08:00:00Z",
  dte: dte - 1,
  spot: 82400,
  legs: [leg("put", 75000), leg("call", 90000)],
  missing: [],
  premium_coin: 0.0094,
  unrealised_coin: 0.0058,
  unrealised_usd: 477.92,
  earned_pct: 61.7,
  net_delta_coin: -0.1,
  net_delta_usd_per_1pct: -82.4,
  break_even_low: 74300,
  break_even_high: 90850,
  today: true,
  points: points(75000, 90000, 0.0094, true),
  ...extra,
});

const report = (strangles, extra = {}) => ({
  as_of: "2026-10-27T18:00:00Z",
  underlying: "BTC",
  spot: 82400,
  dvol: 50,
  model: "Black–Scholes on each leg's IV, zero rate",
  delta_exit_threshold: 0.3,
  stop_loss_multiple: 2,
  bot_net_delta: -0.12,
  bot_net_delta_usd_per_1pct: -98.88,
  strangles,
  ...extra,
});

describe("Scenario: one chart per strangle, by coin and DTE", () => {
  test("Positions shows two BTC charts ordered by DTE, each with its curves and lines", () => {
    render(<PositionsScreen names={["btc", "eth"]} bot="btc" payoff={report([strangle("a", 25), strangle("b", 50)])} />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Positions · BTC");
    const titles = screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(titles).toEqual([expect.stringMatching(/^BTC · 25 d slot · 2026-11-2\d$/), expect.stringMatching(/^BTC · 50 d slot · 2026-12-2\d$/)]);
    for (const chart of screen.getAllByTestId("payoff-chart")) {
      expect(chart.querySelector('[data-curve="expiry"]')).not.toBeNull();
      expect(chart.querySelector('[data-curve="today"]')).not.toBeNull();
      const kinds = [...chart.querySelectorAll("[data-line]")].map((l) => l.dataset.line);
      expect(kinds).toEqual(["strike", "strike", "breakeven", "breakeven", "exit", "exit", "stop", "stop"]);
      expect(chart).toHaveTextContent("floor 75,000");
      expect(chart).toHaveTextContent("ceiling 90,000");
    }
    expect(screen.getByRole("button", { name: "BTC", pressed: true })).toBeInTheDocument();
  });

  test("the coin switch picks the other bot", () => {
    const onSelect = jest.fn();
    render(<PositionsScreen names={["btc", "eth"]} bot="btc" payoff={report([])} onSelect={onSelect} />);
    expect(screen.getByText(/Nothing open right now/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "ETH" }));
    expect(onSelect).toHaveBeenCalledWith("eth");
  });

  test("missing data says unknown, never zeros", () => {
    render(<PositionsScreen names={["btc"]} bot="btc" payoff={null} error="bot btc is unreachable" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Unknown: bot btc is unreachable");
    expect(screen.queryByTestId("payoff-chart")).toBeNull();
  });

  test("each legend item toggles its kind of line", () => {
    render(<PayoffCard sp={strangle("a", 25)} report={report([])} />);
    const chart = screen.getByTestId("payoff-chart");
    const stops = screen.getByRole("button", { name: "Stop-loss levels" });
    expect(stops).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(stops);
    expect(stops).toHaveAttribute("aria-pressed", "false");
    expect(chart.querySelectorAll('[data-line="stop"]')).toHaveLength(0);
    expect(chart.querySelectorAll('[data-line="exit"]')).toHaveLength(2);
  });
});

describe("Scenario: the earned premium fills as time passes", () => {
  test("the area under the today curve is filled as a gain and the bar reads 62 %", () => {
    render(<PayoffCard sp={strangle("a", 25)} report={report([])} />);
    expect(screen.getByText("62 % of the premium earned · +0.0058 of +0.0094 BTC")).toBeInTheDocument();
    const chart = screen.getByTestId("payoff-chart");
    expect(chart.querySelector('[data-zone="gain"]')).not.toBeNull();
    expect(chart.querySelector('[data-zone="loss"]').getAttribute("fill")).toMatch(/^url\(#.*-hatch\)$/);
    expect(chart).toHaveTextContent("now 82,400 · +0.0058 BTC (+$478)");
    expect(chart.getAttribute("aria-label")).toContain("in the gain area");
  });

  test("the area is drawn between zero and the today curve", () => {
    const g = chartGeometry(strangle("a", 25), { width: 640, height: 300 });
    expect(g.area.startsWith(`${g.x(60000).toFixed(1)},${g.zero.toFixed(1)} ${g.today}`)).toBe(true);
  });
});

describe("Scenario: a leg in loss", () => {
  test("the spot point sits in the hatched loss area and its label shows the loss in coin and USD", () => {
    const sp = strangle("a", 25, { spot: 68000, unrealised_coin: -0.004, unrealised_usd: -272, earned_pct: -42.6 });
    render(<PayoffCard sp={sp} report={report([])} />);
    const chart = screen.getByTestId("payoff-chart");
    expect(chart).toHaveTextContent("now 68,000 · −0.0040 BTC (−$272)");
    expect(chart.getAttribute("aria-label")).toContain("in the loss area");
    const dot = chart.querySelector('[data-marker="spot"] circle');
    const g = chartGeometry(sp, { width: 640, height: 300 });
    expect(Number(dot.getAttribute("cy"))).toBeGreaterThan(g.zero); // below zero = the loss side
    expect(screen.getByText("−43 % of the premium earned · −0.0040 of +0.0094 BTC")).toHaveClass("text-loss");
  });
});

describe("Scenario: delta exposure", () => {
  test("the delta box shows the strangle's net delta in coin and USD per 1 %, each leg's, and the bot's", () => {
    render(<PayoffCard sp={strangle("a", 25)} report={report([])} />);
    const box = screen.getByRole("complementary", { name: "Delta" });
    expect(box).toHaveTextContent("Δ −0.10 BTC · −$82 per +1 %");
    expect(box).toHaveTextContent("put Δ +0.10 BTC · +$82 per +1 %");
    expect(box).toHaveTextContent("call Δ −0.20 BTC · −$165 per +1 %");
    expect(box).toHaveTextContent("Whole bot: Δ −0.12 BTC · −$99 per +1 %");
  });
});

describe("Scenario: open from a leg", () => {
  const bot = () => ({
    name: "btc",
    stale: false,
    status: { as_of: new Date().toISOString(), underlying: "BTC", environment: "testnet", market: { spot: 82400 }, held_legs: [] },
    positions: {
      strategy_id: "btc-strangle",
      strangles: [
        {
          id: "a",
          slot: { dte: 25, delta: 0.16 },
          legs: [{ position_id: "p1", instrument: "BTC-27NOV26-75000-P", option_type: "put", side: "sell", strike: 75000, qty: 1, greeks: {} }],
        },
      ],
    },
    payoff: report([strangle("a", 25)]),
  });

  beforeEach(() => {
    window.localStorage.clear();
    useMonitor.mockReturnValue({ names: ["btc"], bots: [bot()], feed: [], history: [], trades: {}, error: null, pollMs: 1000, polledAt: Date.now() });
  });

  test("a leg in the positions table opens the modal with that leg's strangle; Esc closes it and focus returns to the row", () => {
    render(<Monitor role="manager" />);
    const row = screen.getByRole("button", { name: "BTC-27NOV26-75000-P" });
    row.focus();
    fireEvent.click(row);
    const dialog = screen.getByRole("dialog", { name: "Payoff of the strangle" });
    expect(within(dialog).getByRole("heading", { level: 3 })).toHaveTextContent(/^BTC · 25 d slot/);
    expect(within(dialog).getByRole("button", { name: "Close" })).toHaveFocus();
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(row).toHaveFocus();
  });

  test("focus is trapped inside the dialog; × closes it", () => {
    render(<Monitor role="manager" />);
    fireEvent.click(screen.getByRole("button", { name: "BTC-27NOV26-75000-P" }));
    const dialog = screen.getByRole("dialog");
    const close = within(dialog).getByRole("button", { name: "Close" });
    const toggles = within(dialog).getAllByRole("button", { pressed: true });
    toggles[toggles.length - 1].focus();
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(close).toHaveFocus();
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(toggles[toggles.length - 1]).toHaveFocus();
    expect(dialog.className).toMatch(/h-full w-full/); // full screen on a phone
    fireEvent.click(close);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  test("a held leg opens its strangle's chart too", () => {
    const b = bot();
    b.status.held_legs = [{ strangle_id: "a", slot: { dte: 25, delta: 0.16 }, missing: "call", open_leg: "BTC-27NOV26-75000-P", rule: "stopped", reason: "stopped out" }];
    useMonitor.mockReturnValue({ names: ["btc"], bots: [b], feed: [], history: [], trades: {}, error: null, pollMs: 1000, polledAt: Date.now() });
    render(<Monitor role="manager" />);
    const held = within(screen.getByTestId("held-btc:a:call")).getByRole("button", { name: "BTC-27NOV26-75000-P" });
    fireEvent.click(held);
    expect(within(screen.getByRole("dialog")).getByRole("heading", { level: 3 })).toHaveTextContent(/^BTC · 25 d slot/);
  });

  test("the overview shows each strangle as a compact chart that opens the modal", () => {
    render(<Monitor role="user" />);
    const cards = screen.getByRole("region", { name: "BTC · strangles" });
    const open = within(cards).getByRole("button", { name: /BTC · 25 d slot .* open the payoff chart/ });
    expect(within(cards).getByText("Δ −0.10 BTC · −$82 per +1 %")).toBeInTheDocument();
    fireEvent.click(open);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  test("a strangle that is gone says so in the modal", () => {
    const b = bot();
    b.payoff = report([]);
    useMonitor.mockReturnValue({ names: ["btc"], bots: [b], feed: [], history: [], trades: {}, error: null, pollMs: 1000, polledAt: Date.now() });
    render(<Monitor role="manager" />);
    fireEvent.click(screen.getByRole("button", { name: "BTC-27NOV26-75000-P" }));
    expect(within(screen.getByRole("dialog")).getByRole("alert")).toHaveTextContent("Chart unknown: this strangle is no longer open");
  });
});

describe("one-legged strangles and missing IV", () => {
  test("the missing side is marked with its hold reason", () => {
    const sp = strangle("a", 25, { legs: [leg("put", 75000)], missing: [{ option_type: "call", rule: "stopped", reason: "stopped out; cooldown until 30 Oct" }] });
    render(<PayoffCard sp={sp} report={report([])} />);
    expect(screen.getByText("no call held: repair waiting (stopped out; cooldown until 30 Oct)")).toBeInTheDocument();
    expect(missingLine({ option_type: "put", rule: "", reason: "" })).toBe("no put held: repair may sell it now");
  });

  test("a missing IV gives the bot's reason, and no today curve", () => {
    const sp = strangle("a", 25, { today: false, today_reason: "unknown: no implied volatility for BTC-27NOV26-90000-C", points: points(75000, 90000, 0.0094, false) });
    sp.legs = sp.legs.map((l) => ({ ...l, exit_line: null, stop_level: null }));
    render(<PayoffCard sp={sp} report={report([])} />);
    const chart = screen.getByTestId("payoff-chart");
    expect(chart.querySelector('[data-curve="today"]')).toBeNull();
    expect(chart.querySelector('[data-zone="gain"]')).toBeNull();
    expect(screen.getByText(/Today's curve, exit lines and stop levels: unknown: no implied volatility/)).toBeInTheDocument();
    expect(textAlternative(sp, "BTC")).toContain("Today's curve: unknown: no implied volatility");
  });
});

describe("lib/payoff", () => {
  test("words", () => {
    const sp = strangle("a", 25);
    expect(cardTitle("BTC", sp)).toMatch(/^BTC · 25 d slot · 2026-11-2\d$/);
    expect(earnedLine(sp, "BTC")).toBe("62 % of the premium earned · +0.0058 of +0.0094 BTC");
    expect(earnedLine({ ...sp, earned_pct: null }, "BTC")).toMatch(/unknown/);
    expect(nowLabel({ ...sp, spot: 0 }, "BTC")).toMatch(/unknown: no spot price/);
    expect(deltaLine(null, null, "BTC")).toBe("Δ unknown: no delta reported");
    expect(chartLines(sp).map((l) => l.label)).toEqual([
      "floor 75,000",
      "ceiling 90,000",
      "break-even 74,300",
      "break-even 90,850",
      "put exit 77,000",
      "call exit 88,000",
      "put stop 70,000",
      "call stop 95,000",
    ]);
  });

  test("the text alternative lists the zones and the current point", () => {
    const sp = strangle("a", 25);
    const zones = todayZones(sp);
    expect(zones.map((z) => z.gain)).toEqual([false, true, false]);
    const text = textAlternative(sp, "BTC");
    expect(text).toMatch(/Bought back today: loss from 60,000 to .*; gain from .*; loss from .* to 110,000\./);
    expect(text).toContain("zero at the break-evens 74,300 and 90,850");
    expect(text).toContain("now 82,400 · +0.0058 BTC (+$478), in the gain area.");
  });

  test("no points → no chart, with a reason", () => {
    expect(chartGeometry({ points: [] })).toBeNull();
    render(<PayoffCard sp={strangle("a", 25, { points: [] })} report={report([])} />);
    expect(screen.getByText(/Chart unknown: the bot sent no curve/)).toBeInTheDocument();
  });
});

describe("API", () => {
  test("the proxy forwards /api/strangles/payoff (read-only allow-list)", () => {
    expect(ALLOWED_ENDPOINTS).toContain("strangles/payoff");
  });

  test("fetchBotState carries the payoff, and keeps the bot when only the payoff fails", async () => {
    const realFetch = global.fetch;
    global.fetch = jest.fn(async (url) =>
      url.endsWith("/strangles/payoff")
        ? { ok: false, status: 404, headers: new Headers(), json: async () => ({ error: "unknown bot or endpoint" }) }
        : { ok: true, status: 200, headers: new Headers(), json: async () => ({ url }) },
    );
    try {
      const state = await fetchBotState("btc");
      expect(state.status).toEqual({ url: "/api/bots/btc/status" });
      expect(state.payoff).toBeNull();
      expect(state.payoffError).toBe("unknown bot or endpoint");
      global.fetch = jest.fn(async (url) => ({ ok: true, status: 200, headers: new Headers(), json: async () => ({ url }) }));
      expect((await fetchBotState("btc")).payoff).toEqual({ url: "/api/bots/btc/strangles/payoff" });
    } finally {
      global.fetch = realFetch;
    }
  });
});
