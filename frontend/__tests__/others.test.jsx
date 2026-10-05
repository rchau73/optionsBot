import { render, screen } from "@testing-library/react";
import OtherPositions, { otherPositionsSummary } from "@/components/OtherPositions";
import { sumByCurrency, unmanagedPositions } from "@/lib/account";

const longPut = {
  currency: "BTC", instrument: "BTC-25DEC26-60000-P", kind: "option", direction: "buy", size: 1.7,
  average_price: 0.067, mark_price: 0.0045, total_pnl: -0.106, delta: -0.13, initial_margin: 0, maintenance_margin: 0,
};
const shortCall = { ...longPut, instrument: "BTC-27NOV26-98000-C", direction: "sell", size: -0.8, total_pnl: 0.0016 };
const account = (positions, extra = {}) => ({ snapshot: { positions, ...extra } });
const bots = [{ name: "btc", positions: { strangles: [{ legs: [{ instrument: "BTC-27NOV26-98000-C" }] }] } }];

describe("unmanagedPositions", () => {
  test("account positions minus the legs the bots manage", () => {
    const r = unmanagedPositions(account([longPut, shortCall]), bots);
    expect(r.reported).toBe(true);
    expect(r.rows.map((p) => p.instrument)).toEqual(["BTC-25DEC26-60000-P"]);
  });

  test("unknown is never shown as none", () => {
    expect(unmanagedPositions(null, bots)).toEqual({ rows: [], error: null, reported: false });
    expect(unmanagedPositions(account(undefined), bots).reported).toBe(false); // older bot
    expect(unmanagedPositions(account(null, { positions_error: "timeout" }), bots)).toEqual({ rows: [], error: "timeout", reported: true });
  });

  test("P&L summed per currency", () => {
    expect(sumByCurrency([longPut, { ...longPut, currency: "ETH", total_pnl: 2 }, longPut], "total_pnl")).toEqual({ BTC: -0.212, ETH: 2 });
  });
});

describe("OtherPositions", () => {
  test("lists an unmanaged long put with its P&L and margin", () => {
    render(<OtherPositions data={{ rows: [longPut], error: null, reported: true }} />);
    expect(screen.getByText("BTC-25DEC26-60000-P")).toBeInTheDocument();
    expect(screen.getByText("LONG")).toBeInTheDocument();
    expect(screen.getByText("−0.1060 BTC")).toBeInTheDocument();
  });

  test("empty, unavailable and not-reported states", () => {
    const { rerender } = render(<OtherPositions data={{ rows: [], error: null, reported: true }} />);
    expect(screen.getByText(/Every position on the account is managed/)).toBeInTheDocument();
    rerender(<OtherPositions data={{ rows: [], error: "timeout", reported: true }} />);
    expect(screen.getByRole("alert")).toHaveTextContent("timeout");
    rerender(<OtherPositions data={{ rows: [], error: null, reported: false }} />);
    expect(screen.getByText(/does not report/)).toBeInTheDocument();
  });

  test("collapsed summary", () => {
    expect(otherPositionsSummary({ rows: [longPut], error: null, reported: true })).toBe("1 position · P&L −0.1060 BTC");
    expect(otherPositionsSummary({ rows: [], error: null, reported: true })).toBe("none");
    expect(otherPositionsSummary({ rows: [], error: "x", reported: true })).toBe("unavailable");
  });
});
