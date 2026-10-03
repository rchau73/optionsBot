import { render, screen, within } from "@testing-library/react";
import AccountPanel from "@/components/AccountPanel";
import { pendingLabel, riskRows } from "@/lib/account";
import { describeEvent } from "@/lib/monitor";

const riskStatus = (overrides = {}) => ({
  status: {
    limit_im_pct: 35,
    max_mm_pct: 35,
    reason: "DVOL band ≥30 (IM 35%) (confirmed)",
    frozen: true,
    freeze_reason: "DVOL moved to band ≥70 (IM 50%) (IV percentile 81), not yet confirmed",
    pending: [{ rule: "dvol_band", to: "band ≥70 (IM 50%)", days: 1, need: 2 }],
    ...overrides,
  },
  im_pct: 12.3,
  mm_pct: 8.4,
  unit: "USD",
});

const bots = [
  { name: "btc", status: { risk: riskStatus() } },
  { name: "eth", status: { risk: { ...riskStatus({ frozen: false, freeze_reason: "", pending: [], limit_im_pct: 20, reason: "negative gamma regime (confirmed)" }), error: "timeout" } } },
  { name: "old", status: {} }, // a bot built before the margin policy
];

describe("margin policy helpers", () => {
  test("pending change label shows the countdown", () => {
    expect(pendingLabel({ rule: "gamma_regime", to: "negative", days: 1, need: 2 })).toBe("Gamma regime → negative: 1 of 2 daily closes");
  });

  test("one row per bot that reports a policy", () => {
    const rows = riskRows(bots);
    expect(rows.map((r) => r.bot)).toEqual(["btc", "eth"]);
    expect(rows[0]).toMatchObject({ limitIMPct: 35, frozen: true, imPct: 12.3, pending: ["DVOL → band ≥70 (IM 50%): 1 of 2 daily closes"] });
    expect(rows[1]).toMatchObject({ imPct: null, mmPct: null, error: "timeout" }); // margin unknown → "—", never 0
  });
});

describe("AccountPanel margin policy", () => {
  test("shows each bot's limit, reason, freeze and countdown", () => {
    render(<AccountPanel account={null} risk={riskRows(bots)} />);
    const btc = screen.getByText("BTC").closest("tr");
    expect(within(btc).getByText("12.3% / 35%")).toBeInTheDocument();
    expect(within(btc).getByText("FROZEN")).toBeInTheDocument();
    expect(within(btc).getByText(/pending: DVOL → band ≥70/)).toBeInTheDocument();
    const eth = screen.getByText("ETH").closest("tr");
    expect(within(eth).getByText("no margin data")).toBeInTheDocument();
    expect(within(eth).getByText("— / 20%")).toBeInTheDocument();
  });

  test("draws the limits as ticks on the usage bars", () => {
    const account = {
      snapshot: {
        as_of: "2026-10-03T12:00:00Z", margin_model: "cross_pm", cross_collateral: true,
        totals: { equity_usd: 1, margin_balance_usd: 1, initial_margin_usd: 0, maintenance_margin_usd: 0, im_pct: 12, mm_pct: 8 },
        assets: [],
      },
      bot: "btc", ageSec: 1,
    };
    render(<AccountPanel account={account} risk={riskRows(bots)} />);
    const markers = screen.getAllByTestId("limit-marker");
    expect(markers.map((m) => m.getAttribute("title"))).toEqual(["BTC IM limit 35%", "ETH IM limit 20%", "MM limit 35%"]);
    expect(markers[0]).toHaveStyle({ left: "35%" });
  });
});

describe("margin policy in the activity feed", () => {
  const ev = (data) => ({ seq: 9, at: "2026-10-03T12:00:00Z", event: "risk_limit", data });

  test("a freeze is a warning with DVOL and regime context", () => {
    const line = describeEvent("btc", ev({ change: "frozen", detail: "DVOL moved to band ≥70", im_pct: 12.34, mm_pct: 8, unit: "USD", dvol: 61.2, iv_percentile: 81.4, regime: "POSITIVE/PINNING" }));
    expect(line).toMatchObject({ label: "Entries frozen", tone: "warn", text: "DVOL moved to band ≥70 · IM 12.3% / MM 8.0%" });
    expect(line.context).toEqual(["DVOL 61.2 (p81)", "POSITIVE/PINNING"]);
  });

  test("a confirmed limit change names the new limit; an MM breach is bad", () => {
    expect(describeEvent("btc", ev({ change: "limit_changed", limit_im_pct: 20, detail: "x" })).label).toBe("IM limit 20%");
    expect(describeEvent("btc", ev({ change: "mm_breach", detail: "x" })).tone).toBe("bad");
  });

  test("MM-limit closes are labelled and shown in red", () => {
    const line = describeEvent("btc", { seq: 1, at: "2026-10-03T12:00:00Z", event: "closed", data: { close_reason: "margin_mm_limit", qty: 0.2, instrument: "BTC-X-C", fill_price: 0.01, pnl: 0.001 } });
    expect(line).toMatchObject({ label: "MM limit", tone: "bad" });
  });
});
