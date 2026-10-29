// The bots' read-only views through the monitor's proxy (/api/bots/...).

import { getJSON } from "./http.js";

const botUrl = (name, path) => `/api/bots/${encodeURIComponent(name)}${path}`;

/** Bot names and the configured poll interval (ms). */
export async function fetchMonitorConfig(signal) {
  const { bots, pollMs } = await getJSON("/api/bots", signal);
  return { names: bots.map((b) => b.name), pollMs: pollMs ?? 1000 };
}

/**
 * Status, positions, pending orders, account and investor summary of one bot,
 * fetched together. A failed summary is kept apart (summaryError) so the rest
 * of the bot still shows; the investor view then says it is unavailable.
 */
export async function fetchBotState(name, signal) {
  const base = botUrl(name, "");
  const [status, positions, orders, account, summary, payoff] = await Promise.all([
    getJSON(`${base}/status`, signal),
    getJSON(`${base}/positions`, signal),
    getJSON(`${base}/orders`, signal),
    // Older bots have no /account: treat as "no account data", not an outage.
    getJSON(`${base}/account`, signal).catch(() => null),
    getJSON(`${base}/summary`, signal).then(
      (data) => ({ data, error: null }),
      (e) => ({ data: null, error: e.message }),
    ),
    // The payoff charts (#391); a failure leaves the rest of the bot shown.
    getJSON(`${base}/strangles/payoff`, signal).then(
      (data) => ({ data, error: null }),
      (e) => ({ data: null, error: e.message }),
    ),
  ]);
  return {
    status,
    positions,
    orders,
    account,
    summary: summary.data,
    summaryError: summary.error,
    payoff: payoff.data,
    payoffError: payoff.error,
  };
}

/** The payoff chart of each open strangle of one bot (/api/strangles/payoff, #391). */
export async function fetchBotPayoff(name, signal) {
  return getJSON(botUrl(name, "/strangles/payoff"), signal);
}

/** The investor summary of one bot (/api/summary): holding, income, worst case, last action. */
export async function fetchBotSummary(name, signal) {
  return getJSON(botUrl(name, "/summary"), signal);
}

/** Journal events after `since` (sequence number), oldest first. */
export async function fetchBotEvents(name, since, signal, limit = 200) {
  const { events } = await getJSON(botUrl(name, `/events?since=${since}&limit=${limit}`), signal);
  return events;
}

/** Opens and closes after journal sequence `after`, oldest first: { trades, history_since }. */
export async function fetchBotTrades(name, after, signal) {
  return getJSON(botUrl(name, `/trades?after=${after}&limit=1000`), signal);
}

/** One bot's income month by month with its summary (/api/income, #319). */
export async function fetchBotIncome(name, signal) {
  return getJSON(botUrl(name, "/income"), signal);
}

/** One bot's performance over a range (/api/performance?range=7d|30d|all, #232). */
export async function fetchBotPerformance(name, range, signal) {
  return getJSON(botUrl(name, `/performance?range=${encodeURIComponent(range)}`), signal);
}

/** Bucketed P&L history of one bot for a range (15m, 1h, 6h, 1d, 1w, 1m, all). */
export async function fetchPnlHistory(name, range, signal) {
  return getJSON(botUrl(name, `/pnl/history?range=${encodeURIComponent(range)}`), signal);
}
