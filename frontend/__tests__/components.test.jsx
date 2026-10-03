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
  });
});

describe("PositionsTable", () => {
  test("renders groups with strikes, DTE and greeks", () => {
    render(<PositionsTable groups={groupRows(legRows([bot()]), "slot")} />);
    expect(screen.getByText("BTC · 45d · 0.16Δ")).toBeInTheDocument();
    expect(screen.getByText("BTC-27DEC26-115000-C")).toBeInTheDocument();
    expect(screen.getByText("115,000")).toBeInTheDocument();
    expect(screen.getAllByText("44.5")).toHaveLength(2);
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
