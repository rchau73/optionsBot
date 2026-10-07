// Profitability from the bots' journals: every open and close, restored on
// restart, so these figures describe the strategy since its history began.

const CLOSE_LABELS = {
  roi_target: "Take-profit",
  delta_drift: "Roll (delta drift)",
  dte_rollout: "Roll (DTE)",
  delta_exit: "Delta exit",
  stop_loss: "Stop-loss",
  gamma_regime: "GEX shed",
  kill_switch: "Kill switch",
  rebalance_legs: "Balance legs",
  rebalance_downsize: "Downsize (IM limit)",
  margin_mm_limit: "MM limit cut",
};

export const closeLabel = (reason) => CLOSE_LABELS[reason] ?? reason ?? "—";

/**
 * The short position's delta at that moment: the journal keeps the option's
 * delta as Deribit quotes it (long holder), and the bot is short, so the
 * position carries −Δ × qty (a short call is negative, a short put positive).
 */
export function positionDelta(t) {
  return typeof t.delta === "number" && typeof t.qty === "number" ? -t.delta * t.qty : null;
}

/** Merges a page of trades into a bot's list (by seq); a lower seq means the journal was reset. */
export function mergeTrades(prev, page) {
  if (!page.length) return prev;
  const last = prev.length ? prev[prev.length - 1].seq : 0;
  return [...prev, ...page.filter((t) => t.seq > last)];
}

const slotKey = (s) => (s ? `${s.dte}d · Δ${s.delta}` : "unknown");

/**
 * Realized P&L in USD (each close at its own spot) and per coin, win rate,
 * average win vs average loss (USD), and P&L by exit reason and by slot.
 * trades carry `bot` and `unit` (BTC/ETH).
 */
export function summarizeTrades(trades) {
  const closes = trades.filter((t) => t.kind === "close");
  const opens = trades.filter((t) => t.kind === "open");
  const wins = closes.filter((t) => (t.pnl ?? 0) > 0);
  const losses = closes.filter((t) => (t.pnl ?? 0) < 0);
  const sum = (ts, f) => ts.reduce((a, t) => a + (f(t) ?? 0), 0);
  const group = (key) => {
    const m = new Map();
    for (const t of closes) {
      const k = key(t);
      const g = m.get(k) ?? { key: k, closes: 0, wins: 0, pnlUsd: 0 };
      g.closes++;
      g.wins += (t.pnl ?? 0) > 0 ? 1 : 0;
      g.pnlUsd += t.pnl_usd ?? 0;
      m.set(k, g);
    }
    return [...m.values()].sort((a, b) => a.pnlUsd - b.pnlUsd);
  };
  const byCoin = {};
  for (const t of closes) byCoin[t.unit] = (byCoin[t.unit] ?? 0) + (t.pnl ?? 0);
  return {
    opens: opens.length,
    closes: closes.length,
    realisedUsd: sum(closes, (t) => t.pnl_usd),
    realisedByCoin: byCoin,
    wins: wins.length,
    losses: losses.length,
    winRate: closes.length ? (wins.length / closes.length) * 100 : null,
    avgWinUsd: wins.length ? sum(wins, (t) => t.pnl_usd) / wins.length : null,
    avgLossUsd: losses.length ? sum(losses, (t) => t.pnl_usd) / losses.length : null,
    byReason: group((t) => closeLabel(t.reason)),
    bySlot: group((t) => `${t.bot.toUpperCase()} ${slotKey(t.slot)}`),
  };
}

/** Trades matching the filters, newest first. */
export function filterTrades(trades, { bot = "", kind = "", reason = "" } = {}) {
  return trades
    .filter((t) => (!bot || t.bot === bot) && (!kind || t.kind === kind) && (!reason || closeLabel(t.reason) === reason))
    .sort((a, b) => Date.parse(b.at) - Date.parse(a.at) || b.seq - a.seq);
}
