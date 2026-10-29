// The four answers card in the assistant panel (#498,
// docs/product/analytics/surveys.md §2): what the person came to check and
// what was hardest to find, each one of E3's four investor answers or none.
// Pure: when it is due (decided on the server from the user's row in auth.db,
// with NPS's own consent, account-age and sign-in rules) and the card's words.
import { shownWithin, surveyNotEligibleReason } from "./nps.js";
import { PROPERTIES } from "./plan.js";

/** At most once per 30 days, from the day it was shown (answered or not). */
export const ANSWERS_EVERY_DAYS = 30;
/** Never within 7 days after the NPS card was shown. */
export const ANSWERS_AFTER_NPS_DAYS = 7;

/**
 * Why the four answers card is not due for this person now, or null when it
 * is due. From the user's row: consent, createdAtSec, sessions, lastShownSec
 * (this card's, null = never) and npsLastShownSec. The page adds the rule only
 * it can see: never in a tab that has shown (or is about to show) the NPS card.
 */
export function answersNotDueReason({ consent, createdAtSec, sessions, lastShownSec, npsLastShownSec }, nowMs) {
  const reason = surveyNotEligibleReason({ consent, createdAtSec, sessions }, nowMs);
  if (reason) return reason;
  if (shownWithin(lastShownSec, ANSWERS_EVERY_DAYS, nowMs)) return "shown_recently";
  if (shownWithin(npsLastShownSec, ANSWERS_AFTER_NPS_DAYS, nowMs)) return "nps_recently";
  return null;
}

/** surveys.md §2: the two questions, each optional. */
export const ANSWERS_QUESTIONS = [
  { field: "came_for", question: "What did you come to check today?" },
  { field: "hardest_to_find", question: "What was hardest to find?" },
];

/** The five options of both questions, in the order of surveys.md §2 (the event plan's enum). */
export const ANSWERS_CODES = PROPERTIES.came_for.values;

/** surveys.md §2: each option's words. */
export const ANSWERS_LABELS = {
  holding: "What it holds",
  income: "Making or losing",
  worst_case: "The worst case",
  last_action: "Why the last trade",
  none: "None of these",
};

/** The card's mark: optional, from the team, at most once a month. */
export const ANSWERS_MARK = "From the team · optional · two quick questions";

/** The Ask button's accessible name and hint while the card is due. */
export const ANSWERS_ASK_LABEL = "Ask (two optional questions waiting)";
export const ANSWERS_ASK_HINT = "Two optional questions from the team";

export const ANSWERS_THANKS = "Thank you. This tells us what the monitor should make easier to find.";
