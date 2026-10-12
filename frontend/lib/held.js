// Missing legs of one-legged strangles and what holds their repair
// (status.held_legs, built by the bot from the same rules repair applies).
import { isNumber } from "./format";

/** Short label and tone per hold rule (the bot's Hold* constants). */
export const HOLD_RULES = {
  "": { label: "repairing", tone: "good" },
  regime_side: { label: "regime side", tone: "warn" },
  stopped: { label: "stop hold", tone: "warn" },
  gex_shed: { label: "GEX anchor", tone: "warn" },
  gex_shedding: { label: "GEX shedding", tone: "bad" },
  mm_breach: { label: "MM limit", tone: "bad" },
  stop_deferred: { label: "stop waiting", tone: "bad" },
  gex_wait: { label: "waiting GEX", tone: "muted" },
  repair_working: { label: "order working", tone: "good" },
  unconfirmed_order: { label: "unknown order", tone: "muted" },
  churn_paused: { label: "churn pause", tone: "muted" },
  rollout_window: { label: "rollout window", tone: "muted" },
};

export function holdRule(rule) {
  return HOLD_RULES[rule ?? ""] ?? { label: rule, tone: "neutral" };
}

/** % spot must still rise to reach level (negative = already above), or null. */
export function distanceToLevel(spot, level) {
  if (!isNumber(spot) || !isNumber(level) || level <= 0 || spot <= 0) return null;
  return (level / spot - 1) * 100;
}

/**
 * The reference script's direction label in a negative regime (infer_bias,
 * without its wall checks): spot above the flip → acceleration bias up,
 * below → down. The bot itself trades on its own SMA trend, not on this.
 */
export function scriptBias(liveRegime, spot, flip) {
  if (!String(liveRegime ?? "").startsWith("NEGATIVE") || !isNumber(spot) || !isNumber(flip) || flip <= 0) return null;
  if (spot > flip) return "up";
  if (spot < flip) return "down";
  return null;
}

/** One row per missing leg across bots. */
export function heldRows(bots) {
  return (bots ?? []).flatMap((b) =>
    (b.status?.held_legs ?? []).map((h) => ({
      key: `${b.name}:${h.strangle_id}:${h.missing}`,
      bot: b.name,
      ...h,
      toAboveFlipPct: distanceToLevel(h.spot, h.above_flip_level),
      toAnchorPct: h.anchor ? distanceToLevel(h.spot, h.anchor.level) : null,
      bias: scriptBias(h.live_regime, h.spot, h.live_flip),
    })),
  );
}
