import { fireEvent, render, screen, within } from "@testing-library/react";
import ActivityFeed from "@/components/ActivityFeed";
import TradeHistory from "@/components/TradeHistory";
import { describeEvent } from "@/lib/monitor";
import { roleAtLeast } from "@/lib/auth/roles";

// #287: the Activity feed and the trade history lead with the bot's plain
// sentence (#201); the rule and its numbers fold under "details".

const STOP_PLAIN = "Bought back the put: it lost twice its premium (stop-loss)";
const GENERIC = "The bot recorded a step that has no plain-words explanation yet (odd_event).";

const closedEvent = (extra = {}) => ({
  seq: 7,
  at: "2026-10-22T12:00:00Z",
  event: "closed",
  data: {
    close_reason: "stop_loss", qty: 0.2, instrument: "BTC-27NOV26-70000-P", fill_price: 0.02, pnl: -0.004, pnl_usd: -320,
    delta: -0.31, detail: "stop_loss_200pct: mark 0.0200 ≥ 2 × 0.0100",
    market: { spot: 81000, dvol: 44.2 },
  },
  ...extra,
});

const feedOf = (...events) => events.map((e) => describeEvent("btc", e));
// What the monitor's Details for a role starts as (Monitor: roleAtLeast(role, "manager")).
const openFor = (role) => roleAtLeast(role, "manager");

describe("Activity feed", () => {
  test("Scenario: an event with a plain sentence — first line is the sentence, details folded for a user", () => {
    render(<ActivityFeed feed={feedOf(closedEvent({ plain: STOP_PLAIN }))} detailsOpen={openFor("user")} />);
    const item = screen.getAllByRole("listitem")[0];
    const firstLine = item.firstChild;
    expect(within(firstLine).getByText(STOP_PLAIN)).toBeInTheDocument();
    expect(within(firstLine).getByText("btc")).toBeInTheDocument(); // bot, with the time beside it
    expect(within(firstLine).getByText(/BRT$/)).toBeInTheDocument();

    const toggle = within(item).getByRole("button", { name: "details" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    const detail = within(item).getByText(/stop_loss_200pct: mark 0\.0200/);
    expect(detail).not.toBeVisible();
    expect(within(item).getByText(/spot \$81,000/)).not.toBeVisible();
    expect(document.getElementById(toggle.getAttribute("aria-controls"))).toContainElement(detail);

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(toggle).toHaveTextContent("hide details");
    expect(detail).toBeVisible();
  });

  test.each(["manager", "admin"])("Scenario: a manager sees details open — %s", (role) => {
    render(<ActivityFeed feed={feedOf(closedEvent({ plain: STOP_PLAIN }))} detailsOpen={openFor(role)} />);
    expect(screen.getByRole("button", { name: "hide details" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText(/stop_loss_200pct: mark 0\.0200/)).toBeVisible();
  });

  test("an unknown or missing role folds the details", () => {
    expect(openFor(null)).toBe(false);
    expect(openFor("guest")).toBe(false);
  });

  test("Scenario: an older bot without plain — today's text and no empty line", () => {
    render(<ActivityFeed feed={feedOf(closedEvent())} detailsOpen />);
    const item = screen.getAllByRole("listitem")[0];
    expect(within(item).getByText(/Bought back 0\.2 BTC-27NOV26-70000-P/)).toBeVisible();
    expect(within(item).queryByRole("button")).toBeNull();
    // the first line and the market context, nothing else
    expect(item.children).toHaveLength(2);
    expect(item.children[1]).toHaveTextContent("spot $81,000 · DVOL 44.2");
  });

  test("an empty plain counts as missing", () => {
    expect(describeEvent("btc", closedEvent({ plain: "  " })).plain).toBeNull();
    expect(describeEvent("btc", closedEvent({ plain: STOP_PLAIN })).plain).toBe(STOP_PLAIN);
  });

  test("the generic fallback is shown as it is", () => {
    render(<ActivityFeed feed={feedOf(closedEvent({ plain: GENERIC }))} />);
    expect(screen.getByText(GENERIC)).toBeVisible();
  });

  test("the kind filter still reads the event, not the sentence", () => {
    const skipped = { seq: 8, at: "2026-10-22T12:01:00Z", event: "skipped", plain: "Did not open the slot: closed", data: { skip_reason: "gex_wait" } };
    render(<ActivityFeed feed={feedOf(skipped, closedEvent({ plain: STOP_PLAIN }))} />);
    fireEvent.click(screen.getByRole("tab", { name: "Skipped" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("Did not open the slot: closed")).toBeInTheDocument();
  });
});

const trade = (seq, kind, extra = {}) => ({
  seq, kind, bot: "btc", unit: "BTC", at: `2026-10-0${seq}T12:00:00Z`, instrument: `BTC-X-${seq}`,
  slot: { dte: 25, delta: 0.16 }, qty: 1, price: 0.01, reason: kind === "open" ? "entry" : "roi_target", spot: 85000, dvol: 36, ...extra,
});

describe("Trade history", () => {
  const trades = [
    trade(1, "open", { plain: "Sold a call to open the 25-day slot" }),
    trade(2, "close", { reason: "stop_loss", pnl: -0.002, pnl_usd: -170, detail: "stop_loss_200pct: mark 0.02 ≥ 2 × 0.01", plain: STOP_PLAIN }),
    trade(3, "close", { pnl: 0.004, pnl_usd: 340, detail: "roi 52% ≥ 50%" }), // an older bot: no plain
  ];

  test("Scenario: trade history row — reason cell shows plain; expanding shows the rule and its numbers", () => {
    render(<TradeHistory trades={trades} detailsOpen={false} />);
    const row = screen.getByText(STOP_PLAIN).closest("tr");
    const toggle = within(row).getByRole("button", { name: "details" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    const rule = within(row).getByText("stop_loss_200pct: mark 0.02 ≥ 2 × 0.01");
    expect(rule).not.toBeVisible();
    expect(within(row).getByText("Stop-loss")).not.toBeVisible();
    fireEvent.click(toggle);
    expect(rule).toBeVisible();
    expect(within(row).getByText("Stop-loss")).toBeVisible();
  });

  test("details open by default for a manager", () => {
    render(<TradeHistory trades={trades} detailsOpen={openFor("manager")} />);
    const row = screen.getByText(STOP_PLAIN).closest("tr");
    expect(within(row).getByRole("button", { name: "hide details" })).toHaveAttribute("aria-expanded", "true");
    expect(within(row).getByText("stop_loss_200pct: mark 0.02 ≥ 2 × 0.01")).toBeVisible();
  });

  test("a row without plain shows today's reason and detail, no toggle", () => {
    render(<TradeHistory trades={trades} />);
    const row = screen.getByText("BTC-X-3").closest("tr");
    expect(within(row).getByText("Take-profit")).toBeVisible();
    expect(within(row).getByText("roi 52% ≥ 50%")).toBeVisible();
    expect(within(row).queryByRole("button")).toBeNull();
  });

  test("the generic fallback is shown as it is", () => {
    render(<TradeHistory trades={[trade(1, "close", { reason: "odd", plain: GENERIC })]} />);
    expect(screen.getByText(GENERIC)).toBeVisible();
  });

  test("Scenario: filters unchanged — exit reason stop-loss lists only stop-loss closes", () => {
    render(<TradeHistory trades={trades} />);
    fireEvent.change(screen.getByLabelText(/Exit reason/), { target: { value: "Stop-loss" } });
    expect(screen.getByText("1 rows")).toBeInTheDocument();
    expect(screen.getByText(STOP_PLAIN)).toBeInTheDocument();
    expect(screen.queryByText("BTC-X-3")).toBeNull();
  });

  test("losses and gains share size and weight in the P&L cells", () => {
    render(<TradeHistory trades={trades} />);
    const loss = screen.getByText(STOP_PLAIN).closest("tr").querySelector(".text-loss");
    const gain = screen.getByText("BTC-X-3").closest("tr").querySelector(".text-profit");
    const sizeWeight = (el) => el.className.split(/\s+/).filter((c) => /^(text-(xs|sm|base|lg)|font-)/.test(c));
    expect(sizeWeight(loss)).toEqual(sizeWeight(gain));
  });
});
