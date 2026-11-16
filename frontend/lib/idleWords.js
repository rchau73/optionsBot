// Why the bot is not trading, in plain words (#781, epic E3): one sentence
// per reason of /api/summary's `idle` (internal/api/idle.go) — every
// strategy.Skip* journal reason and "halted" — saying what the bot waits for
// and whether a person must do anything. The sentences state what holds the
// bot back now; none of them promises that trading resumes.
// __tests__/idle-words.test.jsx reads the Go constants and fails on a reason
// without a sentence here.

export const IDLE_FALLBACK = "The bot is waiting. Details in Activity.";

const NOTHING_TO_DO = "You need to do nothing.";

// reason → { text, act }: act is what a person must do, or null for nothing.
export const IDLE_WORDS = {
  bias_warmup: { text: "The bot holds back one side until it has a full hour of market data after a restart or a gap." },
  churn_paused: { text: "One slot is paused: it bought back and sold again too often within an hour. It waits for the pause to end." },
  entry_rejected: { text: "The last sale could not be sent, for example a premium too small or an exchange error. The bot tries again each cycle." },
  entry_wait_topup: { text: "The bot waits for a resize order to finish before it sells anything new, so the two do not add up too much risk." },
  gex_wait: { text: "The bot waits for fresh data on how market makers are positioned. Without it, it sells nothing new." },
  halted: {
    text: "The kill switch was used: the bot closed what it held and sells nothing more.",
    act: "A person must restart the bot for it to trade again.",
  },
  liquidity_cap: { text: "Too few contracts are open on the chosen prices, so the order would be under one contract. The bot waits for a deeper market." },
  manual_close_refused: { text: "A close asked from the monitor was refused, because its rule did not allow it. Nothing changed." },
  margin_limit: { text: "A new position would use more of the account than the bot's limit allows. It waits for room under the limit." },
  margin_unknown: { text: "The exchange's account figures are missing or out of date, so the bot adds no risk until they come back." },
  no_expiry: { text: "The exchange lists no expiry date in the range this slot uses. The bot waits for one." },
  no_free_expiry: { text: "Every expiry date in this slot's range is already used by another slot. The bot waits for a free one." },
  no_strike: { text: "No option price fits the slot's target with a real buyer and seller right now. The bot looks again each cycle." },
  rebalance_wait_entry: { text: "A resize waits for a sale that is still working, so the bot sizes one new position at a time." },
  regime_side: { text: "The market favours strong moves toward one side, so the bot sells only the other side for now." },
  repair_held: { text: "A missing side is not sold again yet: a rule holds it back, such as the wait after a stop-loss." },
  risk_frozen: { text: "New risk is paused: a change in market volatility or positioning waits to be confirmed on daily closes. Exits continue." },
  shed_hedged: { text: "The futures hedge experiment keeps an option the bot would otherwise buy back. Exits still apply." },
  stop_deferred: { text: "A stop-loss waits a few minutes for a normal price spread. Nothing new is sold meanwhile." },
};

/**
 * The idle line of a bot's summary: { text, act, blocked, known } or null
 * when the bot is not idle. A reason with no sentence (or none at all) gives
 * IDLE_FALLBACK, known false.
 */
export function idleLine(idle) {
  if (!idle) return null;
  const words = IDLE_WORDS[idle.reason];
  return {
    text: words?.text ?? IDLE_FALLBACK,
    act: words ? (words.act ?? NOTHING_TO_DO) : null,
    blocked: Boolean(idle.blocked) || idle.reason === "halted",
    known: Boolean(words),
  };
}
