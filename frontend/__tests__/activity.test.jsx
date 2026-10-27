import { fireEvent, render, screen, within } from "@testing-library/react";
import { ActivityView } from "@/components/ActivityPage";
import {
  activityFromEvent,
  activityFromTrade,
  activityGroups,
  buildActivity,
  filterActivity,
  groupByDay,
  splitPlain,
} from "@/lib/activity";

// The Activity page (#385): /api/events and /api/trades as the bot serves
// them (internal/api: PlainEvent, PlainTrade), grouped by day in BRT.

const NOW = Date.parse("2026-10-27T15:00:00Z"); // 12:00 BRT
const TODAY = "2026-10-27T13:05:00Z"; // 10:05 BRT
const YESTERDAY = "2026-10-26T14:30:00Z"; // 11:30 BRT

const ev = (seq, event, at, data, plain) => ({ seq, event, at, data, plain });

const opened = ev(
  10,
  "filled",
  YESTERDAY,
  { instrument: "BTC-27NOV26-72000-P", option_type: "put", trigger_reason: "entry", qty: 1, fill_price: 0.0094, premium_received: 0.0094 },
  "Sold the put to open a new strangle and collect its premium (entry).",
);
const boughtBack = ev(
  20,
  "closed",
  TODAY,
  { instrument: "BTC-6NOV26-76000-P", option_type: "put", close_reason: "delta_exit", trigger_reason: "delta_exit", pnl: -0.0012 },
  "Bought back the put: the price moved close to its strike, so the risk grew too fast (delta exit). Like a stop-loss, it will not be sold again on that side until its cooldown ends.",
);
const waited = ev(
  21,
  "skipped",
  TODAY,
  { skip_reason: "gex_wait: no snapshot" },
  "No new risk yet: the market makers' positioning data (GEX) is missing or out of date, so the bot waits for a fresh reading.",
);
const frozen = ev(22, "risk_limit", TODAY, { change: "frozen", detail: "regime negative" }, "New risk is paused: a change waits for confirmation.");
const manualRebalance = ev(
  23,
  "risk_limit",
  TODAY,
  { change: "rebalance_manual", actor: { user: "bob", role: "manager" } },
  "bob asked for a rebalance from the monitor: the positions are resized toward the margin limit.",
);
const unknown = ev(24, "something_new", TODAY, {}, "The bot recorded a step that has no plain-words explanation yet (event=something_new).");
const takeProfit = ev(
  25,
  "closed",
  TODAY,
  { close_reason: "roi_target", pnl: 0.003 },
  "Bought back the call: most of its premium was already earned (take-profit). The call is sold again to complete the strangle.",
);
const submitted = ev(26, "submitted", TODAY, { trigger_reason: "entry" }, "Sent an order to sell the put.");

const ALL = [opened, boughtBack, waited, frozen, manualRebalance, unknown, takeProfit, submitted];

const renderView = (props = {}) => render(<ActivityView bot="btc" events={ALL} trades={[]} now={NOW} {...props} />);

const headlines = () => screen.queryAllByTestId("activity-entry").map((li) => li.querySelector("p").textContent);

describe("Scenario: activity grouped by day, plain words first", () => {
  test("Today lists the buy-back with its why and its signed result; Yesterday the opening with what was paid", () => {
    renderView();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("What the bot did, and why · BTC");
    const days = screen.getAllByRole("region").map((r) => r.getAttribute("aria-label"));
    expect(days).toEqual(["Today", "Yesterday"]);

    const today = screen.getByRole("region", { name: "Today" });
    const buyBack = within(today)
      .getAllByTestId("activity-entry")
      .find((li) => li.textContent.includes("Bought back the put"));
    expect(within(buyBack).getByText("Bought back the put")).toHaveClass("font-semibold");
    expect(buyBack).toHaveTextContent("The price moved close to its strike");
    expect(buyBack).toHaveTextContent("Result −0.00120 BTC");
    expect(within(buyBack).getByText("−0.00120 BTC")).toHaveClass("text-loss");
    expect(buyBack).toHaveTextContent("10:05");
    expect(within(buyBack).getByRole("img", { name: "Bought back" })).toBeInTheDocument();

    const yesterday = screen.getByRole("region", { name: "Yesterday" });
    const open = within(yesterday).getByTestId("activity-entry");
    expect(open).toHaveTextContent("Sold the put to open a new strangle and collect its premium (entry)");
    expect(open).toHaveTextContent("Paid to you +0.00940 BTC");
    expect(within(open).getByText("+0.00940 BTC")).toHaveClass("text-profit");
    expect(within(open).getByRole("img", { name: "Money in" })).toBeInTheDocument();
  });

  test("order mechanics (submitted, amended) stay under Details; the footer says where the raw entries are", () => {
    renderView();
    expect(headlines()).not.toContain("Sent an order to sell the put");
    expect(screen.getByText(/Every line comes from the bot's journal\. The raw entries, with all their numbers, are under Details \(pro\)\./)).toBeInTheDocument();
  });

  test("a person's action carries the person icon and who asked", () => {
    renderView();
    const li = screen.getAllByTestId("activity-entry").find((l) => l.textContent.includes("bob asked"));
    expect(within(li).getByRole("img", { name: "A person's action" })).toBeInTheDocument();
    expect(li).toHaveTextContent("By bob (manager).");
  });
});

describe("Scenario: filters", () => {
  test("Protection keeps only stops, delta exits, sheds, cuts, freezes and manual actions", () => {
    renderView();
    const protection = screen.getByRole("button", { name: "Protection" });
    expect(protection).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(protection);
    expect(protection).toHaveAttribute("aria-pressed", "true");
    // Newest first: same minute, later journal entries first.
    expect(headlines()).toEqual(["bob asked for a rebalance from the monitor", "New risk is paused", "Bought back the put"]);
  });

  test("an event the mapping does not know still appears under Everything, and only there", () => {
    renderView();
    expect(screen.getByRole("button", { name: "Everything" })).toHaveAttribute("aria-pressed", "true");
    expect(headlines()).toContain("The bot recorded a step that has no plain-words explanation yet (event=something_new)");
    for (const name of ["Money in or out", "Protection", "Waiting"]) {
      fireEvent.click(screen.getByRole("button", { name }));
      expect(headlines().some((h) => h.includes("something_new"))).toBe(false);
    }
  });

  test("Money in or out keeps opens and closes; Waiting keeps skips and holds", () => {
    renderView();
    fireEvent.click(screen.getByRole("button", { name: "Money in or out" }));
    expect(headlines()).toEqual(["Bought back the call", "Bought back the put", "Sold the put to open a new strangle and collect its premium (entry)"]);
    fireEvent.click(screen.getByRole("button", { name: "Waiting" }));
    expect(headlines()).toEqual(["No new risk yet"]);
  });
});

describe("Scenario: phone", () => {
  test("filters wrap and are 44 px targets; entries stack (time, icon, text) without a fixed width", () => {
    renderView();
    const group = screen.getByRole("group", { name: "Show" });
    expect(group).toHaveClass("flex-wrap");
    for (const b of within(group).getAllByRole("button")) expect(b).toHaveClass("min-h-11");
    const text = screen.getAllByTestId("activity-entry")[0].querySelector("div");
    expect(text).toHaveClass("min-w-0", "flex-1");
  });
});

describe("Activity page states", () => {
  test("no bot chosen, loading, unavailable and empty each say so, never 0", () => {
    const { rerender } = render(<ActivityView bot={null} />);
    expect(screen.getByRole("alert")).toHaveTextContent("No bot chosen");
    rerender(<ActivityView bot="btc" loading />);
    expect(screen.getByText(/Not yet: loading/)).toBeInTheDocument();
    rerender(<ActivityView bot="btc" error="HTTP 502" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Unknown: HTTP 502");
    rerender(<ActivityView bot="btc" />);
    expect(screen.getByText(/Not yet: the bot has journaled nothing/)).toBeInTheDocument();
  });

  test("a failed refresh keeps the last entries and says so", () => {
    renderView({ error: "timeout" });
    expect(screen.getByRole("status")).toHaveTextContent("Not updating: timeout");
    expect(screen.getAllByTestId("activity-entry").length).toBeGreaterThan(0);
  });

  test("Show earlier days adds older days", () => {
    const old = [1, 2, 3, 4, 5].map((d) => ev(d, "skipped", `2026-10-${String(10 + d).padStart(2, "0")}T15:00:00Z`, { skip_reason: "no_expiry" }, "No new strangle in this slot: x."));
    render(<ActivityView bot="btc" events={old} now={NOW} />);
    expect(screen.getAllByRole("region")).toHaveLength(3);
    fireEvent.click(screen.getByRole("button", { name: "Show earlier days" }));
    expect(screen.getAllByRole("region")).toHaveLength(5);
    expect(screen.queryByRole("button", { name: "Show earlier days" })).toBeNull();
  });
});

describe("lib/activity", () => {
  test("splitPlain cuts at the first colon or full stop", () => {
    expect(splitPlain("Bought back the put: it lost twice. More.")).toEqual({ headline: "Bought back the put", why: "It lost twice. More." });
    expect(splitPlain("No new strangle. It waits.")).toEqual({ headline: "No new strangle", why: "It waits." });
    expect(splitPlain("Sold the put (entry).")).toEqual({ headline: "Sold the put (entry)", why: "" });
    expect(splitPlain("")).toEqual({ headline: "", why: "" });
  });

  test.each([
    ["stop-loss close", { event: "closed", closeReason: "stop_loss" }, { money: true, protection: true, waiting: false, person: false }],
    ["GEX shed", { event: "closed", closeReason: "gamma_regime" }, { money: true, protection: true, waiting: false, person: false }],
    ["MM cut", { event: "closed", closeReason: "margin_mm_limit" }, { money: true, protection: true, waiting: false, person: false }],
    ["kill switch", { event: "closed", closeReason: "kill_switch" }, { money: true, protection: true, waiting: false, person: false }],
    ["manual close", { event: "closed", closeReason: "manual_close" }, { money: true, protection: true, waiting: false, person: true }],
    ["take-profit", { event: "closed", closeReason: "roi_target" }, { money: true, protection: false, waiting: false, person: false }],
    ["entry fill", { event: "filled", trigger: "entry" }, { money: true, protection: false, waiting: false, person: false }],
    ["cancel on freeze", { event: "cancelled", trigger: "risk_frozen" }, { money: false, protection: true, waiting: false, person: false }],
    ["cancel on timeout", { event: "cancelled", trigger: "order_timeout" }, { money: false, protection: false, waiting: false, person: false }],
    ["freeze", { event: "risk_limit", change: "frozen" }, { money: false, protection: true, waiting: false, person: false }],
    ["MM breach", { event: "risk_limit", change: "mm_breach" }, { money: false, protection: true, waiting: false, person: false }],
    ["manual rebalance", { event: "risk_limit", change: "rebalance_manual" }, { money: false, protection: true, waiting: false, person: true }],
    ["repair held", { event: "skipped", skipReason: "repair_held: top-up held" }, { money: false, protection: false, waiting: true, person: false }],
    ["stop deferred", { event: "skipped", skipReason: "stop_deferred" }, { money: false, protection: false, waiting: true, person: false }],
    ["unknown event", { event: "brand_new" }, { money: false, protection: false, waiting: false, person: false }],
  ])("%s", (_, fields, want) => {
    expect(activityGroups(fields)).toEqual(want);
  });

  test("an open or close in both the events and the trades is shown once; older trades fill in", () => {
    const trades = [
      { seq: 3, at: "2026-10-01T10:00:00Z", kind: "open", reason: "entry", premium: 0.01, plain: "Sold the call to open a new strangle (entry)." },
      { seq: 20, at: TODAY, kind: "close", reason: "delta_exit", pnl: -0.0012, plain: boughtBack.plain },
    ];
    const entries = buildActivity([boughtBack], trades, "BTC");
    expect(entries.map((e) => e.seq)).toEqual([20, 3]);
    expect(entries[1].amount).toEqual({ label: "Paid to you", value: 0.01 });
  });

  test("a close without pnl is a 0 result (the journal omits an exact 0); an open without a premium is unknown", () => {
    expect(activityFromTrade({ seq: 1, at: TODAY, kind: "close", reason: "roi_target" }).amount).toEqual({ label: "Result", value: 0 });
    expect(activityFromEvent(ev(2, "filled", TODAY, {}, "Sold."), "BTC").amount).toEqual({ label: "Paid to you", value: null });
  });

  test("an event from an older bot without plain words falls back to its technical line", () => {
    const e = activityFromEvent(ev(5, "skipped", TODAY, { skip_reason: "no_expiry" }), "BTC");
    expect(e.headline).toContain("not entered");
  });

  test("groupByDay titles Today, Yesterday, then the date in BRT", () => {
    const groups = groupByDay(
      [{ at: TODAY }, { at: "2026-10-27T02:00:00Z" /* 26 Oct 23:00 BRT */ }, { at: "2026-10-24T15:00:00Z" }],
      NOW,
    );
    expect(groups.map((g) => g.title)).toEqual(["Today", "Yesterday", "Sat, 24 Oct 2026"]);
  });

  test("filterActivity keeps every entry under Everything", () => {
    const entries = buildActivity(ALL, [], "BTC");
    expect(filterActivity(entries, "all")).toBe(entries);
  });
});
