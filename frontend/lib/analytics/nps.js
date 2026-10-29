// The monthly NPS card in the assistant panel (#446,
// docs/product/analytics/surveys.md §1). Pure: when it is due (decided on the
// server from the user's row in auth.db), the losing-week rule (the page reads
// only the sign of /api/performance?range=7d) and the card's words and chips.
import { NPS_TO_IMPROVE, NPS_WHAT_WORKS } from "./surveys.js";

const DAY_MS = 24 * 3600 * 1000;

/** Never in the first 14 days after the account is created. */
export const NPS_MIN_ACCOUNT_DAYS = 14;
/** Only after this many sessions (sign-ins, as auth.db counts them). */
export const NPS_MIN_SESSIONS = 3;
/** At most once per 30 days, from the day it was shown (answered or not). */
export const NPS_EVERY_DAYS = 30;

/**
 * Why NPS is not due for this person now, or null when it is due. Everything
 * here is on the user's row: consent (true / false / null), createdAtSec,
 * sessions, lastShownSec (null = never shown) and optedOut. Results never
 * enter this rule (the losing-week check is the page's, losingWeek below).
 */
export function npsNotDueReason({ consent, createdAtSec, sessions, lastShownSec, optedOut }, nowMs) {
  if (consent !== true) return "no_consent";
  if (optedOut) return "opted_out";
  const reason = surveyNotEligibleReason({ consent, createdAtSec, sessions }, nowMs);
  if (reason) return reason;
  if (shownWithin(lastShownSec, NPS_EVERY_DAYS, nowMs)) return "shown_recently";
  return null;
}

/**
 * The rules every card of the team shares (surveys.md §1 and §2): consent
 * yes, an account at least 14 days old and at least 3 sign-ins. → why the
 * person may not be asked, or null. The four answers card (answers.js) uses it too.
 */
export function surveyNotEligibleReason({ consent, createdAtSec, sessions }, nowMs) {
  if (consent !== true) return "no_consent";
  if (!Number.isFinite(createdAtSec) || nowMs - createdAtSec * 1000 < NPS_MIN_ACCOUNT_DAYS * DAY_MS) return "account_too_new";
  if (!(sessions >= NPS_MIN_SESSIONS)) return "too_few_sessions";
  return null;
}

/** True when a card shown at shownSec (Unix seconds; null = never) was shown less than `days` days ago. */
export function shownWithin(shownSec, days, nowMs) {
  return shownSec != null && nowMs - shownSec * 1000 < days * DAY_MS;
}

/**
 * The losing-week rule: true when any bot's last 7 days (/api/performance
 * ?range=7d) was a net loss, or when a sign cannot be read (postponed rather
 * than guessed). Only the sign is used; it is never sent or stored.
 */
export function losingWeek(performances) {
  if (!Array.isArray(performances) || performances.length === 0) return true;
  return performances.some((p) => !p || p.net_pnl_known !== true || typeof p.net_pnl !== "number" || p.net_pnl < 0);
}

/** surveys.md §1 step 1. */
export const NPS_QUESTION = "From 0 to 10, how likely are you to recommend this monitor to a friend who invests?";

/** The card's mark: optional, from the team, once a month. */
export const NPS_MARK = "From the team · optional · once a month";

/** The Ask button's accessible name and hint while NPS is due. */
export const NPS_ASK_LABEL = "Ask (one optional question waiting)";
export const NPS_ASK_HINT = "One optional question from the team";

export const NPS_THANKS = "Thank you. Your answer helps us decide what to build next.";

/** surveys.md §4: each code's words. */
export const REASON_LABELS = {
  steady_income: "Steady income",
  easy_to_follow: "Easy to follow",
  saves_time: "Saves me time",
  clear_risk: "I see the risk clearly",
  explains_trades: "It explains why it traded",
  trust_numbers: "I trust the numbers",
  helpful_answers: "The assistant answers my questions",
  value_unclear: "I don't see the value",
  losses: "Risk or losses",
  too_complex: "Too hard to understand",
  price: "Price",
  trust: "I don't trust what I see",
  hard_to_find: "Hard to find what I need",
  missing_feature: "Something I need is missing",
  other: "Something else",
};

/** Step 2 by score: 9–10 asks what works, 0–8 what to improve. → { question, codes } */
export function npsStep2(score) {
  return Number(score) >= 9
    ? { question: "What do we do best?", codes: NPS_WHAT_WORKS }
    : { question: "What should we improve first?", codes: NPS_TO_IMPROVE };
}
