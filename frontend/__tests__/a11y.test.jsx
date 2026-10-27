// Accessibility baseline (#310): every panel and dialog of the monitor and the
// sign-in form rendered with test data and checked by axe-core. Today's
// violations are kept in `a11y-baseline.json` (component → rule id → nodes);
// a new rule or a higher count fails, a lower count asks for the baseline to
// be lowered. `UPDATE_A11Y=1 npm test` rewrites the baseline from real axe
// output — never edit it by hand. Colour contrast is out of jsdom's reach
// (see test/a11y.js).
import fs from "node:fs";
import path from "node:path";
import { render, screen, waitFor } from "@testing-library/react";
import AccountPanel from "@/components/AccountPanel";
import ActivityFeed from "@/components/ActivityFeed";
import Header from "@/components/Header";
import HeldLegs from "@/components/HeldLegs";
import InvestorSummary from "@/components/InvestorSummary";
import KpiStrip from "@/components/KpiStrip";
import LoginForm from "@/components/LoginForm";
import { ConfirmCloseModal } from "@/components/ManualClose";
import OtherPositions from "@/components/OtherPositions";
import PendingOrders from "@/components/PendingOrders";
import PositionsTable from "@/components/PositionsTable";
import { RebalanceModal } from "@/components/Rebalance";
import TradeHistory from "@/components/TradeHistory";
import { consolidateRows, groupRows, legRows, summarise } from "@/lib/monitor";
import { axeCounts, compareToBaseline, UPDATE_COMMAND } from "@/test/a11y";
import { bot, positions, status } from "@/test/fixtures";

const BASELINE_FILE = path.join(__dirname, "a11y-baseline.json");
const UPDATE = process.env.UPDATE_A11Y === "1";
const baseline = fs.existsSync(BASELINE_FILE) ? JSON.parse(fs.readFileSync(BASELINE_FILE, "utf8")) : null;
const recorded = {};

jest.setTimeout(20_000); // axe walks the whole rendered tree

// --- test data, shaped like the bot API responses ---

// A leg the bot lets a person close (regime-side block on), so the Close
// button is active and its dialog can open.
const closeable = (() => {
  const [call, put] = positions.strangles[0].legs;
  const mc = { allowed: true, reason: "confirmed negative gamma regime in a bear trend: no new puts", after: "repair is held as after a stop-out", rebuilt: false };
  return bot({
    status: { ...status, trend: "bear", flip_buffer_pct: 2, market: { ...status.market, gamma_flip: 101000, spot_to_flip_pct: -1 }, risk: { status: { regime_negative: true, regime_known: true } } },
    positions: { ...positions, strangles: [{ ...positions.strangles[0], legs: [call, { ...put, bid: 0.012, ask: 0.014, manual_close: mc }] }] },
  });
})();

const held = bot({
  name: "eth",
  status: {
    ...status,
    held_legs: [
      {
        strangle_id: "st-1", slot: { dte: 30, delta: 0.16 }, missing: "put", open_leg: "ETH-30OCT26-2800-C", rule: "regime_side",
        reason: "confirmed negative gamma regime in a bear trend: no new puts", trend: "bear", confirmed_negative: true,
        live_regime: "NEGATIVE/ACCELERATION", spot: 2545, live_flip: 2476, buffer_pct: 2.6, above_flip_level: 2540.4,
      },
    ],
  },
});

const summary = {
  as_of: "2026-10-22T12:00:00Z",
  underlying: "BTC",
  spot: 100000,
  holding: { strangles: 1, legs: 2, calls: 1, puts: 1, notional_coin: 0.2, notional_usd: 20000, nearest_expiry: "2026-12-27T08:00:00Z", nearest_dte: 44.5, coin: "BTC" },
  income: {
    coin: "BTC",
    months: [
      { month: "2026-09", realised_coin: -0.008, realised_usd: -800, closes: 2 },
      { month: "2026-10", realised_coin: 0.004, realised_usd: 410, closes: 1 },
    ],
    this_month: { month: "2026-10", realised_coin: 0.004, realised_usd: 410, closes: 1 },
    unrealised_coin: 0.0005,
    unrealised_usd: 50,
  },
  worst_case: {
    model: "model: Black–Scholes on each leg's current IV, spot moved ±3 daily σ over one day",
    available: true, sigmas: 3, dvol: 50, move_pct: 7.9, coin: "BTC",
    down: { spot: 92100, loss_coin: 0.05, loss_usd: 4605, pct_of_equity: 4.2 },
    up: { spot: 107900, loss_coin: 0.03, loss_usd: 3237, pct_of_equity: 2.9 },
  },
  last_action: {
    at: "2026-10-21T15:00:00Z", kind: "close", instrument: "BTC-30OCT26-90000-P", option_type: "put", qty: 0.5, rule: "stop_loss",
    reason: "Bought back the put: it lost twice its premium (stop-loss)", detail: "mark 0.02 ≥ 2 × premium 0.01", pnl_coin: -0.008, pnl_usd: -800,
  },
};

const account = {
  snapshot: {
    as_of: "2026-10-03T12:00:05Z", margin_model: "cross_pm", portfolio_margining: true, cross_collateral: true,
    totals: { equity_usd: 150000, margin_balance_usd: 150000, initial_margin_usd: 30000, maintenance_margin_usd: 21000, im_pct: 20, mm_pct: 14 },
    assets: [
      { currency: "BTC", balance: 1.2, equity: 1.25, margin_balance: 1.25, available_funds: 0.95, available_withdrawal_funds: 0.9,
        initial_margin: 0.3, maintenance_margin: 0.2, projected_initial_margin: 0.25, projected_maintenance_margin: 0.18, im_pct: 24, mm_pct: 16, spot_reserve: 0 },
    ],
  },
  bot: "btc", ageSec: 5, error: null,
};

const longPut = {
  currency: "BTC", instrument: "BTC-25DEC26-60000-P", kind: "option", direction: "buy", size: 1.7,
  average_price: 0.067, mark_price: 0.0045, total_pnl: -0.106, delta: -0.13, initial_margin: 0, maintenance_margin: 0,
};

const trade = (seq, kind, extra = {}) => ({
  seq, kind, bot: "btc", unit: "BTC", at: `2026-10-0${seq}T12:00:00Z`, instrument: `BTC-27NOV26-9000${seq}-P`,
  slot: { dte: 25, delta: 0.16 }, qty: 1, price: 0.01, reason: kind === "open" ? "entry" : "roi_target", spot: 85000, dvol: 36, ...extra,
});
const trades = [trade(1, "open"), trade(2, "close", { pnl: 0.004, pnl_usd: 340, roi_pct: 30, hold_days: 5 })];

const feed = [
  { id: "btc:2", at: "2026-10-02T12:00:05Z", bot: "btc", event: "closed", label: "Take-profit", tone: "good", text: "Bought back", context: ["DVOL 55.0"] },
  { id: "btc:1", at: "2026-10-02T12:00:00Z", bot: "btc", event: "skipped", label: "Skipped", tone: "muted", text: "not entered", context: [] },
];

const rebalanceReport = {
  dry_run: true, done: true, limit_im_pct: 20, im_pct: 6.5, share_per_slot: 91.6, margin_balance: 916.04, unit: "ETH",
  actions: [
    { strangle_id: "s1", slot: "50d/0.16Δ", action: "top_up", call: "ETH-27NOV26-3050-C", put: "ETH-27NOV26-1900-P", qty: 100, target_qty: 300, detail: "sell 100 of each leg" },
    { strangle_id: "s2", slot: "30d/0.16Δ", action: "skipped", qty: 0, target_qty: 0, detail: "inside the rollout window" },
  ],
};

const reply = (code, body) => ({ ok: code < 300, status: code, json: async () => body });

// --- one case per panel or dialog: render it and settle anything it loads ---

const positionsTable = () => render(<PositionsTable groups={groupRows(consolidateRows(legRows([closeable])), "slot")} statuses={{ btc: closeable.status }} />);

const CASES = {
  InvestorSummary: () => render(<InvestorSummary bots={[{ ...bot(), summary }]} />),
  Header: () => render(<Header bots={[bot()]} />),
  KpiStrip: () => render(<KpiStrip kpi={summarise([bot()])} />),
  AccountPanel: () => render(<AccountPanel account={account} />),
  PositionsTable: positionsTable,
  HeldLegs: () => render(<HeldLegs bots={[held]} />),
  OtherPositions: () => render(<OtherPositions data={{ rows: [longPut], error: null, reported: true }} />),
  TradeHistory: () => render(<TradeHistory trades={trades} />),
  ActivityFeed: () => render(<ActivityFeed feed={feed} />),
  PendingOrders: () => render(<PendingOrders bots={[bot()]} />),
  ConfirmCloseModal: () => {
    const row = consolidateRows(legRows([closeable])).find((r) => r.manualClose?.allowed);
    return render(<ConfirmCloseModal row={row} status={closeable.status} onCancel={jest.fn()} onDone={jest.fn()} />);
  },
  RebalanceModal: async () => {
    global.fetch = jest.fn().mockResolvedValue(reply(200, rebalanceReport));
    const r = render(<RebalanceModal row={{ bot: "eth", frozen: false, freezeReason: null, error: null }} onClose={jest.fn()} />);
    await screen.findByTestId("rebalance-actions");
    return r;
  },
  LoginForm: async () => {
    global.fetch = jest.fn().mockResolvedValue({ ok: false, status: 401, json: async () => ({}) });
    const r = render(<LoginForm />);
    await waitFor(() => expect(global.fetch).toHaveBeenCalled()); // the refresh attempt on open
    return r;
  },
};

describe("accessibility baseline (axe, colour contrast excluded)", () => {
  const realFetch = global.fetch;
  afterEach(() => {
    global.fetch = realFetch;
  });
  afterAll(() => {
    if (UPDATE) fs.writeFileSync(BASELINE_FILE, `${JSON.stringify(recorded, null, 2)}\n`);
  });

  test.each(Object.keys(CASES))("%s has no violation beyond the baseline", async (name) => {
    const { container } = await CASES[name]();
    const counts = await axeCounts(container);
    recorded[name] = Object.fromEntries(Object.entries(counts).sort(([a], [b]) => a.localeCompare(b)));
    if (UPDATE) return;
    if (!baseline) throw new Error(`a11y-baseline.json is missing: run ${UPDATE_COMMAND} and commit it`);
    expect(compareToBaseline(name, counts, baseline[name])).toEqual([]);
  });
});

describe("Scenario: a new unlabeled control fails", () => {
  test("a button with no accessible name added to PositionsTable is named with its rule", async () => {
    const { container, unmount } = positionsTable();
    const today = await axeCounts(container);
    unmount();

    const added = positionsTable();
    added.container.querySelector("table").parentElement.appendChild(document.createElement("button"));
    const problems = compareToBaseline("PositionsTable", await axeCounts(added.container), today);
    expect(problems.some((p) => p.startsWith("PositionsTable: ") && p.includes("button-name"))).toBe(true);
  });
});

describe("comparison with the baseline", () => {
  test("Scenario: today passes — same counts, nothing to report", () => {
    expect(compareToBaseline("Header", { region: 2 }, { region: 2 })).toEqual([]);
    expect(compareToBaseline("Header", {}, undefined)).toEqual([]);
  });

  test("a new rule and a higher count fail, naming component and rule", () => {
    expect(compareToBaseline("KpiStrip", { "button-name": 1, region: 3 }, { region: 2 })).toEqual([
      "KpiStrip: new violation button-name (1 node)",
      "KpiStrip: region rose from 2 to 3",
    ]);
  });

  test("a lower count asks for the baseline to be lowered with the command", () => {
    expect(compareToBaseline("Header", { region: 1 }, { region: 2, "aria-allowed-role": 1 })).toEqual([
      `Header: aria-allowed-role fell from 1 to 0 — lower the baseline with ${UPDATE_COMMAND}`,
      `Header: region fell from 2 to 1 — lower the baseline with ${UPDATE_COMMAND}`,
    ]);
  });
});
