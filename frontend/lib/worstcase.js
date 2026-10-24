import { isNumber } from "./format";
import { localClock } from "./time";
// Pure helpers for the worst-case page (#318): five one-day moves of the book
// from /api/summary's worst_case.scenarios. Results keep their sign (+ gain,
// − loss). No row says how often such a day happens: a normal model
// understates crypto's tails and there is no measured figure.

const WORDS = { "-3": "very bad", "-2": "bad", 0: "quiet", 2: "strong rise", 3: "very strong rise" };

/** A move in whole percent with its sign: "−8 %", "+5 %", "0 %". */
export function formatMove(pct) {
  if (!isNumber(pct)) return "—";
  const r = Math.round(pct);
  if (r === 0) return "0 %";
  return `${r > 0 ? "+" : "−"}${Math.abs(r)} %`;
}

/** "3× a typical day's swing"; "no move" for 0σ. */
export function swingWords(sigmas) {
  const n = Math.abs(sigmas ?? 0);
  return n === 0 ? "no move" : `${n}× a typical day's swing`;
}

/** The scenario rows to show, worst fall first; [] when there is nothing to show. */
export function scenarioRows(summary) {
  if (whyNoScenarios(summary)) return [];
  return [...summary.worst_case.scenarios]
    .sort((a, b) => a.sigmas - b.sigmas)
    .map((s) => ({
      key: String(s.sigmas),
      sigmas: s.sigmas,
      words: WORDS[s.sigmas] ?? `${s.sigmas}σ`,
      move: formatMove(s.move_pct),
      swing: swingWords(s.sigmas),
      spot: s.spot,
      coin: s.result_coin,
      usd: s.result_usd,
      pct: isNumber(s.pct_of_equity) ? s.pct_of_equity : null,
    }));
}

/**
 * Why the page shows no results, in words — null when it can. Nothing held
 * and DVOL unknown both say so instead of a table of zeros.
 */
export function whyNoScenarios(summary) {
  if (!summary) return "Unknown: no answer from the bot yet.";
  const wc = summary.worst_case;
  if (!summary.holding?.legs) return "The bot holds nothing right now, so a bad day costs nothing. The table appears once it sells options.";
  if (!wc?.available) return `Unknown: ${wc?.reason || "the bot did not say why"}.`;
  if (!wc.scenarios?.length) return "Unknown: this bot does not report the five scenarios yet.";
  return null;
}

/**
 * The headline: the worse of the two 3σ days as a loss (positive = money
 * lost) in coin, USD and % of the account (null when the equity is unknown).
 */
export function headlineLoss(summary) {
  if (whyNoScenarios(summary)) return null;
  const { down, up } = summary.worst_case;
  const worse = (up?.loss_usd ?? -Infinity) > (down?.loss_usd ?? -Infinity) ? up : down;
  if (!worse) return null;
  return { coin: worse.loss_coin, usd: worse.loss_usd, pct: isNumber(worse.pct_of_equity) ? worse.pct_of_equity : null };
}

/** "twice", "3 times", "2.5 times" from stop_loss_multiple; null when unknown. */
export function stopWord(multiple) {
  if (!isNumber(multiple) || multiple <= 0) return null;
  if (multiple === 1) return "once";
  if (multiple === 2) return "twice";
  return `${multiple} times`;
}

/** "14:05" in the display zone, or null. */
export function clockHHMM(iso) {
  const c = iso ? localClock(iso) : null;
  return c ? c.slice(0, 5) : null;
}
