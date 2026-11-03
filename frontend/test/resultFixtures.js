// The result fixtures of the pages that show P&L (#582): a profitable month,
// a loss and nothing held, with the figures of the Overview's four answers
// (overview-four-answers.test.jsx, #489): the month ±0.00400 BTC (±$410) net
// of fees, one 25-day strangle, the worst case −0.05 BTC (−$4,935). Each page
// gets the shape of the API it reads: /api/summary and /api/status (phone
// overview, Worst case), /api/income (Income), /api/strangles/payoff
// (Positions) and the journal's trades (Details).

export const KINDS = ["profit", "loss", "none"];

const MONTH = "2026-10";
const sign = (kind) => (kind === "loss" ? -1 : 1);

const status = (kind) => ({
  as_of: new Date().toISOString(),
  underlying: "BTC",
  environment: "testnet",
  market: { spot: 95900, gex_regime: "POSITIVE/PINNING", gamma_flip: 90000, dvol: 45.2 },
  trend: "neutral",
  gex_as_of: new Date().toISOString(),
  risk: { status: { limit_im_pct: 20, max_mm_pct: 50, frozen: false, regime_known: true }, im_pct: 10, mm_pct: 5, as_of: new Date().toISOString() },
  held_legs: [],
  open_legs: kind === "none" ? 0 : 2,
  open_strangles: kind === "none" ? 0 : 1,
});

// The last trade: a buy-back that lost (the four answers' CLOSE).
const LAST_CLOSE = {
  at: "2026-10-28T09:12:00Z",
  kind: "close",
  instrument: "BTC-27NOV26-110000-C",
  qty: 1,
  rule: "take_profit",
  reason: "Bought back the call: its take-profit fired.",
  pnl_coin: -0.0012,
  pnl_usd: -115,
};

// Five one-day moves: the calm day earns, the big days cost.
const SCENARIOS = [
  { sigmas: -3, move_pct: -7.9, spot: 88324, result_coin: -0.05, result_usd: -4935, pct_of_equity: -4.2 },
  { sigmas: -1, move_pct: -2.6, spot: 93407, result_coin: -0.008, result_usd: -790, pct_of_equity: -0.7 },
  { sigmas: 0, move_pct: 0, spot: 95900, result_coin: 0.0003, result_usd: 29, pct_of_equity: 0.03 },
  { sigmas: 1, move_pct: 2.6, spot: 98393, result_coin: -0.006, result_usd: -592, pct_of_equity: -0.5 },
  { sigmas: 3, move_pct: 7.9, spot: 103476, result_coin: -0.03, result_usd: -2961, pct_of_equity: -2.5 },
];

/** /api/summary: the month's result, the open P&L, the worst case, the holding. */
export function summaryFor(kind) {
  const held = kind !== "none";
  const month = { month: MONTH, realised_coin: sign(kind) * 0.004, realised_usd: sign(kind) * 410, closes: 2 };
  return {
    as_of: "2026-10-28T15:00:00Z",
    underlying: "BTC",
    environment: "testnet",
    spot: 95900,
    stop_loss_multiple: 2,
    income: { coin: "BTC", months: [month], this_month: month, unrealised_coin: held ? sign(kind) * 0.001 : 0, unrealised_usd: held ? sign(kind) * 98.7 : 0 },
    worst_case: held
      ? { available: true, move_pct: 7.9, sigmas: 3, dvol: 45.2, scenarios: SCENARIOS, down: { loss_coin: 0.05, loss_usd: 4935, pct_of_equity: 4.2 }, up: { loss_coin: 0.03, loss_usd: 2961, pct_of_equity: 2.5 } }
      : { available: true, move_pct: 7.9, sigmas: 3, dvol: 45.2, scenarios: [], down: { loss_coin: 0, loss_usd: 0, pct_of_equity: 0 }, up: { loss_coin: 0, loss_usd: 0, pct_of_equity: 0 } },
    holding: held
      ? { strangles: 1, legs: 2, nearest: { expiry: "2026-11-27T08:00:00Z", put_strike: 85000, call_strike: 110000 } }
      : { strangles: 0, legs: 0 },
    last_action: LAST_CLOSE,
  };
}

/** One strangle of /api/strangles/payoff; on the loss fixture it is losing too. */
function strangle(kind) {
  return {
    strangle_id: "btc-a",
    slot: { dte: 25, delta: 0.16 },
    expiry: "2026-11-27T08:00:00Z",
    dte: 29.4,
    spot: 95900,
    legs: [
      { position_id: "p", instrument: "BTC-27NOV26-85000-P", option_type: "put", strike: 85000, qty: 1 },
      { position_id: "c", instrument: "BTC-27NOV26-110000-C", option_type: "call", strike: 110000, qty: 1 },
    ],
    missing: [],
    premium_coin: 0.01,
    unrealised_coin: sign(kind) * 0.0062,
    unrealised_usd: sign(kind) * 594.6,
    earned_pct: sign(kind) * 62,
    break_even_low: 84000,
    break_even_high: 111000,
  };
}

/** /api/strangles/payoff. */
export function payoffFor(kind) {
  return { as_of: new Date().toISOString(), underlying: "BTC", spot: 95900, model: "Black-76", strangles: kind === "none" ? [] : [strangle(kind)] };
}

/** One bot as useMonitor serves it. */
export function botFor(kind) {
  return { name: "btc", stale: false, status: status(kind), summary: summaryFor(kind), payoff: payoffFor(kind) };
}

/**
 * /api/income: the month's parts add up to the result (paid to you − paid to
 * buy back − fees). Nothing held is a bot that has closed nothing yet.
 */
export function incomeFor(kind) {
  if (kind === "none") {
    return {
      coin: "BTC",
      months: [{ month: MONTH, closes: 0, premium_coin: 0, buyback_coin: 0, fees_coin: 0, realised_coin: 0, premium_usd: 0, buyback_usd: 0, fees_usd: 0, realised_usd: 0, hold_usd: null, hold_reason: "no spot recorded in the P&L history this month" }],
      unrealised_coin: 0,
      unrealised_usd: 0,
      summary: {
        from: "2026-10-01T00:00:00Z",
        to: "2026-10-28T15:00:00Z",
        label: "testnet",
        total_6m: { coin: 0, usd: 0 },
        return_pct: null,
        average_month: null,
        active_months: 0,
        up_months: 0,
        down_months: 0,
        worst_month: null,
        worst_week: { known: false, coin: 0, pct: null, start: null, end: null },
        missing_days: [],
      },
    };
  }
  const buyback = kind === "loss" ? 0.013 : 0.005;
  const result = sign(kind) * 0.004;
  return {
    coin: "BTC",
    months: [
      {
        month: MONTH,
        closes: 2,
        premium_coin: 0.01,
        buyback_coin: buyback,
        fees_coin: 0.001,
        realised_coin: result,
        premium_usd: 1025,
        buyback_usd: buyback * 102500,
        fees_usd: 102.5,
        realised_usd: sign(kind) * 410,
        hold_usd: 0,
      },
    ],
    unrealised_coin: sign(kind) * 0.001,
    unrealised_usd: sign(kind) * 98.7,
    summary: {
      from: "2026-10-01T00:00:00Z",
      to: "2026-10-28T15:00:00Z",
      label: "testnet",
      total_6m: { coin: result, usd: sign(kind) * 410 },
      return_pct: sign(kind) * 0.2,
      average_month: { coin: result, usd: sign(kind) * 410 },
      active_months: 1,
      up_months: kind === "loss" ? 0 : 1,
      down_months: kind === "loss" ? 1 : 0,
      worst_month: { month: MONTH, coin: result, usd: sign(kind) * 410, spot_move_pct: null },
      worst_week: { known: false, coin: 0, pct: null, start: null, end: null },
      missing_days: [],
    },
  };
}

/** The journal's trades (Details (pro)): the month's two closes add up to its result. */
export function tradesFor(kind) {
  if (kind === "none") return [];
  const close = (seq, pnl, pnlUSD) => ({
    seq,
    kind: "close",
    bot: "btc",
    unit: "BTC",
    at: `2026-10-${10 + seq}T12:00:00Z`,
    instrument: "BTC-27NOV26-110000-C",
    slot: { dte: 25, delta: 0.16 },
    qty: 1,
    price: 0.01,
    reason: "take_profit",
    spot: 95900,
    dvol: 45.2,
    pnl,
    pnl_usd: pnlUSD,
    roi_pct: pnl > 0 ? 40 : -40,
  });
  return [close(1, sign(kind) * 0.003, sign(kind) * 307), close(2, sign(kind) * 0.001, sign(kind) * 103)];
}
