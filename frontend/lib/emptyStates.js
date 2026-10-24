// Plain-words empty states for every monitor panel, chart and list (#298).
// Content lives in content/empty-states.json: { panel: { empty, loading,
// stale, error, source } } — what will appear, when, and why it is not there
// now. Facts only, never a forecast or advice (the style of the bot's
// internal/api/explain.go). Content only: the screens read it once E11's
// prototype is approved.
import emptyStates from "@/content/empty-states.json";

export const STATES = ["empty", "loading", "stale", "error"];
export const MAX_TEXT = 140;

/**
 * Every panel key → what the monitor shows for it: the Panel title, or the
 * component for blocks rendered outside a Panel. The monitor.panel.* keys
 * are the collapsible panels' storageKey (and the analytics `panel` enum).
 */
export const PANELS = {
  "monitor.header": "<Header",
  "monitor.gex": "GEX regime and gamma flip (header chips)",
  "monitor.summary": "<InvestorSummary",
  "monitor.kpi": "<KpiStrip",
  "monitor.panel.account": "Account & collateral (shared by all bots)",
  "monitor.positions": "Open positions",
  "monitor.panel.held": "Missing legs: why repair holds them",
  "monitor.panel.others": "Not managed by the bot (still uses margin)",
  "monitor.panel.trades": "Trade history & profitability",
  "monitor.activity": "Activity",
  "monitor.pnl": "P&L (USD, all bots)",
  "monitor.pnl_coin": "P&L in coin (per bot)",
  "monitor.orders": "Working orders (placed, not filled yet)",
};

/** The sentence for a panel in a state (empty | loading | stale | error), or null. */
export function emptyStateFor(panel, state, entries = emptyStates) {
  if (!STATES.includes(state)) return null;
  return entries[panel]?.[state] ?? null;
}

export default emptyStates;
