import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import Panel from "@/components/Panel";
import ActivityFeed from "@/components/ActivityFeed";
import PnlChart from "@/components/PnlChart";
import CoinPnlChart from "@/components/CoinPnlChart";
import { RebalanceModal } from "@/components/Rebalance";

// jsdom has no layout engine, so these tests pin the CSS contract (#109): the
// classes that make each panel follow the window instead of a fixed height.

const feed = [
  { id: "btc:2", at: "2026-10-02T12:00:05Z", bot: "btc", event: "closed", label: "Take-profit", tone: "good", text: "Bought back", context: [] },
  { id: "btc:1", at: "2026-10-02T12:00:00Z", bot: "btc", event: "skipped", label: "Skipped", tone: "muted", text: "not entered", context: [] },
];

const classesOf = (el) => el.className.split(/\s+/);

const renderActivity = () =>
  render(
    <Panel title="Activity" className="lg:col-span-2" fill>
      <ActivityFeed feed={feed} />
    </Panel>,
  );

describe("Scenario: Activity uses the height of its row", () => {
  test("the panel and its body stretch, and the list fills the rest of the panel", () => {
    renderActivity();
    const section = screen.getByRole("heading", { name: "Activity" }).closest("section");
    expect(classesOf(section)).toEqual(expect.arrayContaining(["flex", "flex-col", "lg:col-span-2"]));
    const body = section.querySelector("header + div");
    expect(classesOf(body)).toEqual(expect.arrayContaining(["flex", "flex-1", "min-h-0", "flex-col"]));

    // On lg the list is absolute inside a flex-1 box: it takes the row's height
    // and scrolls inside it, and its events never make the row taller.
    expect(classesOf(screen.getByTestId("activity-box"))).toEqual(expect.arrayContaining(["relative", "flex-1"]));
    expect(classesOf(screen.getByTestId("activity-list"))).toEqual(
      expect.arrayContaining(["overflow-y-auto", "lg:absolute", "lg:inset-0", "lg:max-h-none"]),
    );
  });

  test("a plain panel does not stretch", () => {
    render(<Panel title="Open positions">body</Panel>);
    const section = screen.getByRole("heading", { name: "Open positions" }).closest("section");
    expect(classesOf(section)).not.toContain("flex");
    expect(classesOf(screen.getByText("body"))).toEqual(["p-3"]);
  });
});

describe("Scenario: Resizing the window resizes the panels", () => {
  const realFetch = global.fetch;
  afterEach(() => {
    global.fetch = realFetch;
  });

  test("no fixed pixel height limits the Activity list; its minimum follows the window", () => {
    renderActivity();
    const list = screen.getByTestId("activity-list");
    expect(list.className).not.toMatch(/\d+px/);
    expect(classesOf(screen.getByTestId("activity-box"))).toContain("lg:min-h-[max(20rem,70vh)]");
  });

  test("the USD P&L chart follows the window within a minimum and maximum", () => {
    global.fetch = jest.fn().mockResolvedValue({ ok: true, json: async () => ({ points: [] }) });
    const live = [
      { t: Date.parse("2026-10-08T12:00:00Z"), totalUsd: 10, realisedUsd: 0, bots: {} },
      { t: Date.parse("2026-10-08T12:00:05Z"), totalUsd: 12, realisedUsd: 0, bots: {} },
    ];
    window.localStorage.setItem("monitor.pnl.range", "live");
    render(<PnlChart live={live} names={null} />);
    expect(classesOf(screen.getByLabelText("P&L, range live"))).toContain("h-[clamp(14rem,30vh,28rem)]");
    window.localStorage.removeItem("monitor.pnl.range");
  });

  test("the coin P&L chart follows the window within a minimum and maximum", async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        points: [
          { t: "2026-10-08T12:00:00Z", realised: 0.01, total: 0.02 },
          { t: "2026-10-08T13:00:00Z", realised: 0.01, total: 0.0125 },
        ],
      }),
    });
    render(<CoinPnlChart names={["btc"]} units={{ btc: "BTC" }} />);
    const chart = await screen.findByLabelText("P&L in BTC, range 1w");
    expect(classesOf(chart)).toContain("h-[clamp(10rem,20vh,20rem)]");
  });

  test("the rebalance actions list is capped relative to the window", async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ dry_run: true, done: true, unit: "ETH", actions: [] }),
    });
    render(<RebalanceModal row={{ bot: "eth", frozen: false, freezeReason: null, error: null }} onClose={jest.fn()} />);
    const list = await screen.findByTestId("rebalance-actions");
    expect(classesOf(list)).toContain("max-h-[max(16rem,40vh)]");
    expect(classesOf(list)).not.toContain("max-h-64");
  });
});

describe("Scenario: Small screens stay usable", () => {
  test("below lg the Activity list has a bounded height with its own scroll bar", () => {
    renderActivity();
    const list = classesOf(screen.getByTestId("activity-list"));
    expect(list).toEqual(expect.arrayContaining(["max-h-[max(20rem,60vh)]", "overflow-y-auto"]));
    expect(list).not.toContain("absolute"); // absolute only from lg up
  });
});

describe("Scenario: Nothing else changes", () => {
  test("the stretched Activity panel keeps its filters", () => {
    renderActivity();
    expect(screen.getByText("Take-profit")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Skipped" }));
    expect(screen.queryByText("Take-profit")).not.toBeInTheDocument();
    expect(screen.getByText("not entered")).toBeInTheDocument();
  });

  test("a stretched panel still collapses", async () => {
    render(
      <Panel title="Activity" fill collapsible storageKey="layout.k">
        body
      </Panel>,
    );
    fireEvent.click(screen.getByRole("button", { name: /Activity/ }));
    await waitFor(() => expect(screen.queryByText("body")).toBeNull());
    window.localStorage.clear();
  });
});
