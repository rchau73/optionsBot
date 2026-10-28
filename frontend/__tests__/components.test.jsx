import { fireEvent, render, screen, within } from "@testing-library/react";
import Header from "@/components/Header";
import KpiStrip from "@/components/KpiStrip";
import PositionsTable from "@/components/PositionsTable";
import PendingOrders from "@/components/PendingOrders";
import ActivityFeed from "@/components/ActivityFeed";
import { groupRows, legRows, summarise } from "@/lib/monitor";
import { bot, status } from "@/test/fixtures";

describe("Header", () => {
  test("shows environment, freshness and market per bot", () => {
    render(<Header bots={[bot()]} />);
    const chip = screen.getByTestId("bot-btc");
    expect(within(chip).getByText("TESTNET")).toBeInTheDocument();
    expect(within(chip).getByText(/DVOL 55.2/)).toBeInTheDocument();
  });

  // #394: the regime, flip and SMA levels moved onto each bot's regime chart; the chip keeps warnings, spot and DVOL.
  test("keeps the regime text off the chip, and shows IM over limit as a warning", () => {
    const market = { ...status.market, spot: 95000, gamma_flip: 100000, spot_to_flip_pct: -5 };
    render(<Header bots={[bot({ status: { ...status, market, flip_buffer_pct: 2, risk: { im_over_limit: true } } })]} />);
    const chip = screen.getByTestId("bot-btc");
    expect(chip).not.toHaveTextContent(/Flip|POSITIVE|SMA/);
    expect(within(chip).getByText("IM over limit")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Account/ })).toBeNull(); // the person's menu is in the menu bar
  });

  test("names the environment and its release once", () => {
    render(<Header bots={[bot({ status: { ...status, deployment: "stable", version: "v0.1.0" } }), bot({ name: "eth" })]} />);
    expect(screen.getByTestId("deployment")).toHaveTextContent("STABLE · v0.1.0");
  });

  test("no deployment badge for bots started by hand", () => {
    render(<Header bots={[bot()]} />);
    expect(screen.queryByTestId("deployment")).not.toBeInTheDocument();
  });

  test("no flip, no band", () => {
    render(<Header bots={[bot()]} />);
    expect(screen.queryByTestId("flip-band")).not.toBeInTheDocument();
  });

  test("flags live, halted, stale and offline bots", () => {
    render(
      <Header
        bots={[
          bot({ status: { ...status, environment: "live", halted: true } }),
          bot({ name: "eth", stale: true, error: "bot eth is unreachable" }),
        ]}
      />,
    );
    expect(screen.getByText("LIVE")).toBeInTheDocument();
    expect(screen.getByText("HALTED")).toBeInTheDocument();
    expect(screen.getByText("OFFLINE")).toBeInTheDocument();
  });
});

describe("KpiStrip", () => {
  test("shows counts and P&L in USD with direction", () => {
    render(<KpiStrip kpi={summarise([bot()])} />);
    expect(screen.getByText("Open legs").nextSibling).toHaveTextContent("2");
    expect(screen.getByText("Total P&L").nextSibling).toHaveTextContent("$150");
    expect(screen.getByText("Total P&L").nextSibling).toHaveClass("text-profit");
    expect(screen.getByText("Unrealized P&L")).toBeInTheDocument();
    expect(screen.getByText("Realized P&L")).toBeInTheDocument();
  });
});

describe("PositionsTable", () => {
  test("renders groups with strikes (the full instrument in the title) and greeks", () => {
    render(<PositionsTable groups={groupRows(legRows([bot()]), "slot")} />);
    expect(screen.getByText("BTC · 45d · 0.16Δ")).toBeInTheDocument();
    expect(screen.getByText("115,000")).toHaveAttribute("title", "BTC-27DEC26-115000-C");
    expect(screen.getAllByText("1.0")).toHaveLength(2); // Γ ×10⁻⁵ of each leg
  });

  test("labels unrealized P&L, shows entry → mark and flags non-live marks", () => {
    render(<PositionsTable groups={groupRows(legRows([bot()]), "slot")} />);
    expect(screen.getByText("Unrealized P&L")).toBeInTheDocument();
    expect(screen.getByTestId("leg-btc:p-1")).toHaveTextContent("0.0120 → 0.0090");
    expect(screen.getAllByText("last cycle")).toHaveLength(1); // only the put has no live ticker
  });

  test("shows each leg's premium kept, signed, beside its bar", () => {
    render(<PositionsTable groups={groupRows(legRows([bot()]), "slot")} />);
    expect(screen.getByText("Premium kept")).toHaveAttribute("title", expect.stringContaining("% of the premium"));
    expect(screen.getByText("+25%")).toHaveClass("text-profit");
    expect(screen.getByTestId("leg-btc:p-1").querySelector('[data-testid="micro-bar"]')).toHaveAttribute("aria-hidden", "true");
  });

  test("a bought leg (e.g. an iron-condor wing) shows BUY, and a losing leg is red", () => {
    const b = bot();
    const leg = b.positions.strangles[0].legs[0];
    leg.side = "buy";
    leg.roi_pct = -40;
    render(<PositionsTable groups={groupRows(legRows([b]), "slot")} />);
    expect(screen.getByText("BUY")).toBeInTheDocument();
    expect(screen.getByText("-40%")).toHaveClass("text-loss");
  });

  test("explains an empty table", () => {
    render(<PositionsTable groups={[]} />);
    expect(screen.getByText(/No open positions/)).toBeInTheDocument();
  });
});

describe("PendingOrders", () => {
  test("lists working orders with fill progress", () => {
    render(<PendingOrders bots={[bot()]} />);
    expect(screen.getByText("BTC-30OCT26-110000-C")).toBeInTheDocument();
    expect(screen.getByText("0/0.1")).toBeInTheDocument();
    expect(screen.getByText(/^in 1m 1[45]s$/)).toBeInTheDocument(); // time left before the fill timeout
  });

  test("empty state", () => {
    render(<PendingOrders bots={[bot({ orders: { pending: [] } })]} />);
    expect(screen.getByText("No working orders.")).toBeInTheDocument();
  });
});

describe("ActivityFeed", () => {
  const feed = [
    { id: "btc:2", at: "2026-10-02T12:00:05Z", bot: "btc", event: "closed", label: "Take-profit", tone: "good", text: "Bought back", context: ["DVOL 55.0"] },
    { id: "btc:1", at: "2026-10-02T12:00:00Z", bot: "btc", event: "skipped", label: "Skipped", tone: "muted", text: "not entered", context: [] },
  ];

  test("filters by kind", () => {
    render(<ActivityFeed feed={feed} />);
    expect(screen.getByText("Take-profit")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Skipped" }));
    expect(screen.queryByText("Take-profit")).not.toBeInTheDocument();
    expect(screen.getByText("not entered")).toBeInTheDocument();
  });

  test("empty state", () => {
    render(<ActivityFeed feed={[]} />);
    expect(screen.getByText(/Nothing yet/)).toBeInTheDocument();
  });
});
