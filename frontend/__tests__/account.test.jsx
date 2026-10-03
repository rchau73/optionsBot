import { render, screen, within } from "@testing-library/react";
import AccountPanel from "@/components/AccountPanel";
import { marginLevel, marginModelLabel, pickAccount, worstMMPct } from "@/lib/account";

const now = Date.parse("2026-10-03T12:00:10Z");
const snapshot = (overrides = {}) => ({
  as_of: "2026-10-03T12:00:05Z",
  margin_model: "cross_pm",
  portfolio_margining: true,
  cross_collateral: true,
  totals: { equity_usd: 150000, margin_balance_usd: 150000, initial_margin_usd: 30000, maintenance_margin_usd: 21000, im_pct: 20, mm_pct: 14 },
  assets: [
    { currency: "BTC", balance: 1.2, equity: 1.25, margin_balance: 1.25, available_funds: 0.95, available_withdrawal_funds: 0.9,
      initial_margin: 0.3, maintenance_margin: 0.2, projected_initial_margin: 0.25, projected_maintenance_margin: 0.18, im_pct: 24, mm_pct: 16, spot_reserve: 0 },
    { currency: "USDC", balance: 25000, equity: 25000, margin_balance: 25000, available_funds: 25000, available_withdrawal_funds: 25000,
      initial_margin: 0, maintenance_margin: 0, projected_initial_margin: 0, projected_maintenance_margin: 0, im_pct: 0, mm_pct: 0, spot_reserve: 0 },
  ],
  ...overrides,
});

describe("account helpers", () => {
  test("margin levels follow the warning bands", () => {
    expect(marginLevel(10)).toBe("ok");
    expect(marginLevel(50)).toBe("warn");
    expect(marginLevel(80)).toBe("danger");
    expect(marginLevel(undefined)).toBe("unknown");
  });

  test("model labels", () => {
    expect(marginModelLabel("segregated_pm")).toBe("Segregated · Portfolio margin");
    expect(marginModelLabel("new_model")).toBe("new_model");
    expect(marginModelLabel("")).toBe("Unknown model");
  });

  test("picks the freshest snapshot across bots (one shared account)", () => {
    const older = snapshot({ as_of: "2026-10-03T12:00:00Z" });
    const newer = snapshot({ as_of: "2026-10-03T12:00:08Z" });
    const picked = pickAccount([{ name: "btc", account: { snapshot: older } }, { name: "eth", account: { snapshot: newer } }, { name: "x" }], now);
    expect(picked.bot).toBe("eth");
    expect(picked.ageSec).toBe(2);
    expect(pickAccount([{ name: "btc", account: { snapshot: null } }], now)).toBeNull();
  });

  test("worst maintenance margin across totals and assets", () => {
    expect(worstMMPct(snapshot())).toBe(16);
    expect(worstMMPct({ assets: [] })).toBeNull();
  });
});

describe("AccountPanel", () => {
  test("shows model, totals, margin bars and one row per asset", () => {
    render(<AccountPanel account={{ snapshot: snapshot(), bot: "btc", ageSec: 5, error: null }} />);
    expect(screen.getByText("Cross · Portfolio margin")).toBeInTheDocument();
    expect(screen.getByText("Cross collateral on")).toBeInTheDocument();
    expect(screen.getByText("Equity (all collateral)").nextSibling).toHaveTextContent("$150,000");
    expect(screen.getByRole("meter", { name: /MM used/ })).toHaveAttribute("aria-valuenow", "14");
    const btcRow = screen.getByText("BTC").closest("tr");
    expect(within(btcRow).getByText("24.0%")).toBeInTheDocument();
    expect(within(btcRow).getByText("0.9")).toBeInTheDocument(); // withdrawable
    expect(screen.getByText("USDC")).toBeInTheDocument();
    expect(screen.queryByText("STALE")).not.toBeInTheDocument();
  });

  test("without cross collateral there are no USD totals", () => {
    render(<AccountPanel account={{ snapshot: snapshot({ cross_collateral: false, totals: undefined }), bot: "btc", ageSec: 1 }} />);
    expect(screen.getByText("Cross collateral off")).toBeInTheDocument();
    expect(screen.queryByText("Equity (all collateral)")).not.toBeInTheDocument();
  });

  test("flags stale data, a failed poll and high margin", () => {
    const risky = snapshot();
    risky.assets[0] = { ...risky.assets[0], mm_pct: 85 };
    render(<AccountPanel account={{ snapshot: risky, bot: "btc", ageSec: 120, error: "timeout" }} />);
    expect(screen.getByText("STALE")).toBeInTheDocument();
    expect(screen.getByText("last poll failed")).toBeInTheDocument();
    expect(screen.getByText("MARGIN HIGH")).toBeInTheDocument();
    expect(screen.getByText("85.0%")).toHaveClass("text-loss");
  });

  test("explains when there is no account data", () => {
    render(<AccountPanel account={null} />);
    expect(screen.getByText(/No account data yet/)).toBeInTheDocument();
  });
});
