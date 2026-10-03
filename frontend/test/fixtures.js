// Shared test data shaped like the bot API responses.
export const status = {
  as_of: new Date().toISOString(),
  strategy_id: "short-strangle",
  underlying: "BTC",
  environment: "testnet",
  halted: false,
  market: { spot: 100000, dvol: 55.2, iv_percentile: 62, gex_regime: "POSITIVE/PINNING" },
  trend: "neutral",
  open_legs: 2,
  pending: 1,
  event_counts: { submitted: 4, filled: 2, closed: 1, skipped: 3 },
  pnl: [
    { slot: { dte: 45, delta: 0.16 }, realised: 0.001, unrealised: 0.0005, total: 0.0015 },
    { slot: null, realised: 0.001, unrealised: 0.0005, total: 0.0015 },
  ],
};

export const positions = {
  strategy_id: "short-strangle",
  underlying: "BTC",
  strangles: [
    {
      id: "st-1",
      slot: { dte: 45, delta: 0.16 },
      legs: [
        {
          position_id: "p-1", instrument: "BTC-27DEC26-115000-C", option_type: "call", strike: 115000, dte: 44.5,
          qty: 0.1, entry_price: 0.012, mark: 0.009, bid: 0.0085, ask: 0.0095, mark_source: "live", mark_as_of: new Date().toISOString(), unrealised_pnl: 0.0003, roi_pct: 25, loss_multiple: -0.25,
          stop_loss_mark: 0.036, moneyness: "OTM", distance_to_strike_pct: 15,
          greeks: { delta: 0.12, gamma: 0.00001, theta: -12, vega: 30 },
        },
        {
          position_id: "p-2", instrument: "BTC-27DEC26-88000-P", option_type: "put", strike: 88000, dte: 44.5,
          qty: 0.1, entry_price: 0.015, mark: 0.013, bid: 0, ask: 0, mark_source: "last_cycle", mark_as_of: new Date().toISOString(), unrealised_pnl: 0.0002, roi_pct: 13, loss_multiple: -0.13,
          stop_loss_mark: 0.045, moneyness: "OTM", distance_to_strike_pct: 12,
          greeks: { delta: -0.14, gamma: 0.00001, theta: -13, vega: 31 },
        },
      ],
    },
  ],
};

export const orders = {
  pending: [
    {
      id: "ps-1", slot: { dte: 25, delta: 0.16 }, repair: false, submitted_at: new Date().toISOString(), adjustments: 1,
      legs: [{ order_id: "o-7", instrument: "BTC-30OCT26-110000-C", option_type: "call", qty: 0.1, filled_qty: 0, limit_price: 0.011 }],
    },
  ],
};

export const bot = (overrides = {}) => ({ name: "btc", status, positions, orders, stale: false, ...overrides });
