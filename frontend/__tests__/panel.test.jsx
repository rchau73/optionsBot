import { fireEvent, render, screen } from "@testing-library/react";
import Panel from "@/components/Panel";
import { AccountSummary } from "@/components/AccountPanel";
import { accountSummary } from "@/lib/account";

beforeEach(() => window.localStorage.clear());

describe("Panel", () => {
  test("a plain panel has no toggle", () => {
    render(<Panel title="Open positions">body</Panel>);
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("body")).toBeInTheDocument();
  });

  test("collapses and expands, showing the summary only while collapsed", () => {
    render(
      <Panel title="Account" collapsible storageKey="k" summary="IM 19%">
        body
      </Panel>,
    );
    const toggle = screen.getByRole("button", { name: /Account/ });
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.queryByText("IM 19%")).toBeNull();

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("body")).toBeNull();
    expect(screen.getByText("IM 19%")).toBeInTheDocument();

    fireEvent.click(toggle);
    expect(screen.getByText("body")).toBeInTheDocument();
  });

  test("remembers the choice in this browser", () => {
    const { unmount } = render(
      <Panel title="Account" collapsible storageKey="k">
        body
      </Panel>,
    );
    fireEvent.click(screen.getByRole("button", { name: /Account/ }));
    expect(window.localStorage.getItem("k")).toBe("0");
    unmount();

    render(
      <Panel title="Account" collapsible storageKey="k">
        body
      </Panel>,
    );
    expect(screen.getByRole("button", { name: /Account/ })).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("body")).toBeNull();
  });

  test("works when storage is blocked", () => {
    const spy = jest.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    jest.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    render(
      <Panel title="Account" collapsible storageKey="k">
        body
      </Panel>,
    );
    expect(screen.getByText("body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Account/ }));
    expect(screen.queryByText("body")).toBeNull();
    spy.mockRestore();
    jest.restoreAllMocks();
  });
});

const risk = [
  { bot: "btc", imPct: 19.24, limitIMPct: 20, frozen: false },
  { bot: "eth", imPct: null, limitIMPct: 20, frozen: true },
];
const account = (mm, ageSec = 5) => ({ snapshot: { totals: { mm_pct: mm }, assets: [] }, bot: "btc", ageSec });

describe("account summary (collapsed header)", () => {
  test("IM vs limit per bot, worst MM, attention flags", () => {
    expect(accountSummary(account(12.3), risk)).toEqual({
      text: "BTC IM 19.2% / 20% · ETH IM — / 20% · MM 12.3%",
      level: "ok",
      frozen: ["ETH"],
      missing: false,
    });
    expect(accountSummary(null, []).missing).toBe(true);
    expect(accountSummary(account(85), []).level).toBe("danger");
  });

  test("badges for high margin, frozen bots and stale data", () => {
    render(<AccountSummary account={account(85, 120)} risk={risk} />);
    expect(screen.getByText("MARGIN HIGH")).toBeInTheDocument();
    expect(screen.getByText("FROZEN ETH")).toBeInTheDocument();
    expect(screen.getByText("STALE")).toBeInTheDocument();
  });

  test("no account yet", () => {
    render(<AccountSummary account={null} risk={[]} />);
    expect(screen.getByText("MM —")).toBeInTheDocument();
    expect(screen.getByText("STALE")).toBeInTheDocument();
  });
});
