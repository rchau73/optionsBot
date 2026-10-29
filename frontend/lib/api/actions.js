// The two requests that change a bot's trading state, through the monitor's
// proxy, which adds the bot's admin token and checks the person's role.

import { sendJSON } from "./http.js";

/**
 * Asks a bot to buy back positions at market (manual close; the bot allows it
 * only under the regime-side block). Resolves to its per-position results.
 */
export async function closePositions(name, positionIds) {
  const body = await sendJSON(`/api/bots/${encodeURIComponent(name)}/close`, "POST", { position_ids: positionIds });
  return body?.results ?? [];
}

/**
 * Manual rebalance of one bot. dryRun: only report what the pass would do.
 * Returns the bot's report; throws with the bot's reason when it refuses
 * (frozen, cooldown, no margin data...).
 */
export async function rebalanceBot(name, dryRun) {
  return sendJSON(`/api/bots/${encodeURIComponent(name)}/rebalance`, "POST", { dry_run: dryRun });
}
