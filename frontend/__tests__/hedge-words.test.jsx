import fs from "fs";
import path from "path";
import { act, render, renderHook, screen, within } from "@testing-library/react";
import { ActivityView } from "@/components/ActivityPage";
import { useMonitor } from "@/hooks/useMonitor";
import { activityFromEvent, activityGroups, filterActivity, buildActivity } from "@/lib/activity";
import { fetchBotEvents, fetchBotState, fetchBotTrades, fetchMonitorConfig } from "@/lib/api/bots";
import { HEDGE_CHANGES, hedgeResult, hedgeSentence, isHedgeEvent, isHedgeRisk } from "@/lib/hedgeWords";

// #679 (epic E3): the futures hedge's journal lines in plain words. The lines
// are shaped as internal/strategy/futureshedge.go journals them.

jest.mock("@/lib/api/bots", () => ({
  fetchBotEvents: jest.fn(),
  fetchBotState: jest.fn(),
  fetchBotTrades: jest.fn(),
  fetchMonitorConfig: jest.fn(),
}));

const NOW = Date.parse("2026-11-06T15:00:00Z");
const AT = "2026-11-06T13:05:00Z";

const hedgeEv = (seq, data, plain = "The bot's own sentence.") => ({ seq, event: "hedge", at: AT, data, plain });
const FILL = { change: "hedge_fill", side: "sell", amount_usd: 300, filled_usd: 300, price: 75000, ledger_price: 75100, options_delta: 0.5, perp_delta: -0.08, band_coin: 0.3, instrument: "BTC-PERPETUAL" };
const fillEv = (over = {}) => hedgeEv(1, { ...FILL, ...over });
const SHED = {
  seq: 2,
  event: "skipped",
  at: AT,
  data: { skip_reason: "shed_hedged BTC-27NOV26-60000-P", instrument: "BTC-27NOV26-60000-P", detail: "shed — kept: the futures hedge holds the delta; the shed would have bought 1.5 at 0.0123" },
  plain: "The GEX shed would have bought this option back.",
};

const goFile = (...parts) => fs.readFileSync(path.join(process.cwd(), "..", ...parts), "utf8");
const goHedgeChanges = () => [...goFile("internal", "orders", "journal.go").matchAll(/^\s*Hedge\w+\s*=\s*"(\w+)"/gm)].map((m) => m[1]);

// Every change with figures, as the journal gives them.
const LINE_OF = {
  hedge_closed: { side: "buy", amount_usd: 32310, filled_usd: 32310, price: 76000, options_delta: 0.42, perp_delta: -0.42, realised_coin: -0.0012, detail: "futures hedge switched off by bob" },
  hedge_failed: { side: "sell", amount_usd: 300 },
  hedge_fill: FILL,
  hedge_halted: { detail: "locked_by_admin: no hedge orders; looks again every 10m0s" },
  hedge_mode_changed: { enabled: true, actor: { user: "bob", role: "admin" } },
  hedge_price_away: { side: "sell", amount_usd: 300, price: 78000, options_delta: 0.5, perp_delta: -0.08, band_coin: 0.3 },
  hedge_rate_limited: { side: "buy", amount_usd: 300, options_delta: -0.5, perp_delta: 0.08, band_coin: 0.3 },
  hedge_resumed: { options_delta: 0.4 },
  hedge_unavailable: { options_delta: 0.4, detail: "the last hedge orders found nothing to fill: the GEX shed works as with the mode off until the hedge can trade" },
  perp_left_open: { amount_usd: 500, options_delta: 0, perp_delta: 0.0066 },
};

describe("Scenario: every hedge change name has a sentence", () => {
  test("the Go constants and the sentences are the same list", () => {
    const names = goHedgeChanges();
    expect(names).toEqual(expect.arrayContaining(["hedge_fill", "hedge_unavailable", "perp_left_open"]));
    expect([...HEDGE_CHANGES].sort()).toEqual([...names].sort());
  });

  test("the skip's name is the Go constant's", () => {
    expect(goFile("internal", "strategy", "futureshedge.go")).toContain('const SkipShedHedged = "shed_hedged"');
    expect(isHedgeEvent(SHED)).toBe(true);
  });

  test.each(HEDGE_CHANGES)("%s reads as plain words, with no raw name or underscore", (change) => {
    const text = hedgeSentence(hedgeEv(1, { change, ...LINE_OF[change] }), "BTC");
    expect(text).toMatch(/^[A-Zb].*\.$/);
    expect(text).not.toContain("_");
    expect(text).not.toMatch(/undefined|NaN|null/);
  });

  test("a change the list does not know keeps the bot's sentence", () => {
    expect(hedgeSentence(hedgeEv(1, { change: "hedge_new" }), "BTC")).toBeNull();
    expect(activityFromEvent(hedgeEv(1, { change: "hedge_new" }, "A new step."), "BTC").headline).toBe("A new step");
  });
});

describe("Scenario: a hedge fill in the feed", () => {
  test("the row is one plain sentence with side, size, reason and the delta before and after, with no raw event name", () => {
    render(<ActivityView bot="btc" events={[fillEv()]} now={NOW} />);
    const row = screen.getByTestId("activity-entry");
    expect(row).toHaveTextContent("Sold 300 USD of the perpetual to bring the book's coin delta back inside its band (it was +0.4200 BTC, about +0.4160 BTC after; the band is ±0.3000 BTC)");
    expect(row).not.toHaveTextContent("hedge_fill");
    expect(row).not.toHaveTextContent("The bot's own sentence");
    expect(within(row).getByRole("img", { name: "Protection" })).toBeInTheDocument();
  });

  test("a buy moves the delta up", () => {
    expect(hedgeSentence(fillEv({ side: "buy", options_delta: -0.5, perp_delta: 0.08 }), "ETH")).toBe(
      "Bought 300 USD of the perpetual to bring the book's coin delta back inside its band (it was −0.4200 ETH, about −0.4160 ETH after; the band is ±0.3000 ETH).",
    );
  });

  test("a part fill says how much of the order filled", () => {
    expect(hedgeSentence(fillEv({ filled_usd: 200 }), "BTC")).toContain("Sold 200 USD of the perpetual out of 300 USD due to bring");
  });

  test("an order that filled nothing says so, is no trade, and shows no delta after", () => {
    const ev = fillEv({ filled_usd: undefined });
    const text = hedgeSentence(ev, "BTC");
    expect(text).toContain("Tried to sell 300 USD of the perpetual");
    expect(text).toContain("but nothing filled (it was +0.4200 BTC; the band is ±0.3000 BTC)");
    const { groups } = activityFromEvent(ev, "BTC");
    expect(groups).toMatchObject({ money: false, protection: false, waiting: true });
  });

  test("a line without a price gives no delta after, never a made-up one", () => {
    expect(hedgeSentence(fillEv({ price: undefined, ledger_price: undefined }), "BTC")).toContain("(it was +0.4200 BTC; the band is ±0.3000 BTC)");
  });

  test("a delta that rounds to nothing never reads −0.0000", () => {
    expect(hedgeSentence(fillEv({ options_delta: 0.00001, perp_delta: -0.00002 }), "BTC")).toContain("it was 0.0000 BTC");
  });

  test("older lines without the figures still read, without them", () => {
    const text = hedgeSentence(hedgeEv(1, { change: "hedge_fill", filled_usd: 300, side: "sell" }), "BTC");
    expect(text).toBe("Sold 300 USD of the perpetual to bring the book's coin delta back inside its band.");
  });
});

describe("Scenario: hedge unavailable", () => {
  test("the row says the hedge cannot trade and the GEX shed is back, under Protection", () => {
    const ev = hedgeEv(3, { change: "hedge_unavailable", ...LINE_OF.hedge_unavailable });
    render(<ActivityView bot="btc" events={[ev]} now={NOW} />);
    const row = screen.getByTestId("activity-entry");
    expect(within(row).getByText("The futures hedge cannot trade, so the GEX shed is back on")).toHaveClass("font-semibold");
    expect(row).toHaveTextContent("It buys the threatened side back as usual until the hedge can trade again.");
    expect(activityFromEvent(ev, "BTC").groups.protection).toBe(true);
    expect(isHedgeRisk(ev)).toBe(true);
  });
});

describe("the other lines", () => {
  test("a close shows its result with its sign, a loss as a gain", () => {
    const loss = hedgeEv(4, { change: "hedge_closed", ...LINE_OF.hedge_closed });
    render(<ActivityView bot="btc" events={[loss]} now={NOW} />);
    const row = screen.getByTestId("activity-entry");
    expect(row).toHaveTextContent("Bought 32,310 USD of the perpetual to close the hedge");
    expect(row).toHaveTextContent("The experiment was switched off");
    expect(row).toHaveTextContent("Result −0.00120 BTC");
    expect(within(row).getByText("−0.00120 BTC")).toHaveClass("text-loss");
    const gain = activityFromEvent(hedgeEv(4, { change: "hedge_closed", ...LINE_OF.hedge_closed, realised_coin: 0.002 }), "BTC");
    expect(gain.amount).toEqual({ label: "Result", value: 0.002 });
  });

  test("the kill switch's close names the kill switch; the result is a 0 when the journal omits it", () => {
    const d = { change: "hedge_closed", ...LINE_OF.hedge_closed, realised_coin: undefined, detail: "kill switch: closing the bot's perpetual" };
    expect(hedgeSentence(hedgeEv(4, d), "BTC")).toContain("the kill switch closes what the bot holds");
    expect(hedgeResult(d)).toBe(0);
  });

  test("a close that filled nothing, a fill and a mode switch show no result", () => {
    expect(hedgeResult({ ...LINE_OF.hedge_closed, change: "hedge_closed", filled_usd: 0 })).toBeNull();
    expect(activityFromEvent(fillEv(), "BTC").amount).toBeNull();
    expect(activityFromEvent(hedgeEv(5, { change: "hedge_mode_changed", ...LINE_OF.hedge_mode_changed, realised_coin: 0.5 }), "BTC").amount).toBeNull();
  });

  test("a person's switch names them and counts as their action; on and off differ", () => {
    const on = activityFromEvent(hedgeEv(5, { change: "hedge_mode_changed", ...LINE_OF.hedge_mode_changed }), "BTC");
    expect(on.headline).toBe("bob switched the futures hedge experiment on from the monitor");
    expect(on.groups.person).toBe(true);
    expect(on.icon).toBe("person");
    expect(hedgeSentence(hedgeEv(5, { change: "hedge_mode_changed", enabled: false }), "BTC")).toContain("A person switched the futures hedge experiment off");
    expect(hedgeSentence(hedgeEv(5, { change: "hedge_mode_changed" }), "BTC")).toBe("A person switched the futures hedge experiment from the monitor.");
  });

  test("waits name the order and the delta; a line without a size still reads", () => {
    expect(hedgeSentence(hedgeEv(6, { change: "hedge_rate_limited", ...LINE_OF.hedge_rate_limited }), "BTC")).toBe(
      "A purchase of 300 USD of the perpetual waits: the hourly limit of hedge orders was reached (it was −0.4200 BTC; the band is ±0.3000 BTC). It is tried again next cycle.",
    );
    expect(hedgeSentence(hedgeEv(6, { change: "hedge_price_away", ...LINE_OF.hedge_price_away }), "BTC")).toContain("A sale of 300 USD of the perpetual waits: its price is too far from the index");
    expect(hedgeSentence(hedgeEv(6, { change: "hedge_failed" }), "BTC")).toContain("A trade of the perpetual did not go through");
    expect(hedgeSentence(hedgeEv(6, { change: "perp_left_open" }), "BTC")).toContain("left part of the perpetual open");
  });

  test("a kept leg names the option and its size, in place of the skip's generic sentence", () => {
    expect(activityFromEvent(SHED, "BTC").headline).toBe("Kept 1.5 × BTC-27NOV26-60000-P");
    expect(activityFromEvent(SHED, "BTC").why).toContain("The GEX shed would have bought it back");
    const bare = { ...SHED, data: { skip_reason: "shed_hedged" } };
    expect(hedgeSentence(bare, "BTC")).toContain("Kept an option:");
  });

  test.each([
    ["a fill", "hedge_fill", 300, { money: true, protection: true, waiting: false, person: false }],
    ["a close", "hedge_closed", 300, { money: true, protection: false, waiting: false, person: false }],
    ["a close that filled nothing", "hedge_closed", 0, { money: false, protection: false, waiting: true, person: false }],
    ["rate limited", "hedge_rate_limited", undefined, { money: false, protection: false, waiting: true, person: false }],
    ["price away", "hedge_price_away", undefined, { money: false, protection: false, waiting: true, person: false }],
    ["failed", "hedge_failed", undefined, { money: false, protection: false, waiting: true, person: false }],
    ["halted", "hedge_halted", undefined, { money: false, protection: true, waiting: false, person: false }],
    ["left open", "perp_left_open", undefined, { money: false, protection: true, waiting: false, person: false }],
    ["resumed", "hedge_resumed", undefined, { money: false, protection: false, waiting: false, person: false }],
  ])("filters: %s", (_, change, filledUSD, want) => {
    expect(activityGroups({ event: "hedge", change, filledUSD })).toEqual(want);
  });

  test("a risk change's name on another event changes nothing, and other events stay untouched", () => {
    expect(activityGroups({ event: "risk_limit", change: "hedge_halted" }).protection).toBe(false);
    expect(hedgeSentence({ event: "closed", data: {} }, "BTC")).toBeNull();
    expect(isHedgeEvent({ event: "skipped", data: { skip_reason: "no_expiry" } })).toBe(false);
    expect(isHedgeRisk(SHED)).toBe(false);
    const entries = buildActivity([fillEv(), { seq: 9, event: "skipped", at: AT, data: { skip_reason: "no_expiry" }, plain: "No new strangle." }], [], "BTC");
    expect(filterActivity(entries, "money").map((e) => e.seq)).toEqual([1]);
  });
});

describe("Scenario: phone width", () => {
  test("a long sentence wraps: nothing of a fixed width or kept on one line", () => {
    render(<ActivityView bot="btc" events={[SHED, fillEv()]} now={NOW} />);
    for (const row of screen.getAllByTestId("activity-entry")) {
      expect(row.querySelector("div")).toHaveClass("min-w-0", "flex-1");
      for (const p of row.querySelectorAll("p")) expect(p).toHaveClass("break-words");
      expect(row.outerHTML).not.toMatch(/whitespace-nowrap|truncate|w-\[\d+px\]/);
    }
  });
});

describe("useMonitor's hedgeEvents", () => {
  test("keeps each bot's hedge lines from the polls, and a restarted journal starts them over", async () => {
    jest.useFakeTimers();
    fetchMonitorConfig.mockResolvedValue({ names: ["btc"], pollMs: 1000 });
    fetchBotTrades.mockResolvedValue({ trades: [] });
    fetchBotState.mockResolvedValue({ status: { underlying: "BTC", event_counts: { filled: 5 } } });
    fetchBotEvents.mockResolvedValueOnce([fillEv(), { seq: 2, event: "skipped", at: AT, data: { skip_reason: "no_expiry" } }, { ...SHED, seq: 3 }]).mockResolvedValueOnce([hedgeEv(4, { change: "hedge_resumed" })]);
    fetchBotEvents.mockResolvedValue([]);
    const { result, unmount } = renderHook(() => useMonitor());
    const held = () => result.current.hedgeEvents.btc?.map((e) => e.seq);
    const after = (ms) => act(() => jest.advanceTimersByTimeAsync(ms));

    await after(0);
    expect(held()).toEqual([1, 3]);
    await after(1000);
    expect(held()).toEqual([1, 3, 4]);

    fetchBotState.mockResolvedValue({ status: { underlying: "BTC", event_counts: { filled: 1 } } });
    fetchBotEvents.mockResolvedValueOnce([hedgeEv(1, { change: "hedge_resumed" })]);
    await after(1000);
    expect(held()).toEqual([1]);
    unmount();
    jest.useRealTimers();
  });
});
