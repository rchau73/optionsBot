// The in-product surveys' answer rules (docs/product/analytics/surveys.md,
// #363): which codes each survey asks and where free text is allowed. Pure —
// POST /api/survey checks every answer here before anything is stored. The
// codes are values of the event plan's allow-list (plan.js PROPERTIES), so the
// survey's event passes the collector's own check; __tests__/survey-route.test.js
// keeps the two equal.
import { PROPERTIES } from "./plan.js";

/** The longest free text kept (surveys.md §1 and §3: ≤ 500 characters). */
export const MAX_SURVEY_TEXT = 500;

// surveys.md §4: the reason codes each survey asks.
const NPS_REASONS = ["value_unclear", "losses", "too_complex", "price", "trust", "steady_income", "easy_to_follow", "saves_time", "other"];
const EXIT_REASONS = ["value_unclear", "losses", "too_complex", "price", "trust", "switched", "other"];

/**
 * survey id → { codes: field → allowed values, required: fields that must be
 * answered, text: whether free text may come with it — true, or the
 * reason_code that opens the text box }.
 */
export const SURVEYS = {
  // §1: a score (required), one coded why and an optional text.
  nps: { codes: { score: PROPERTIES.score.values, reason_code: NPS_REASONS }, required: ["score"], text: true },
  // §2: two optional choice questions, no text.
  answers: { codes: { came_for: PROPERTIES.came_for.values, hardest_to_find: PROPERTIES.hardest_to_find.values }, required: [], text: false },
  // §3: one optional exit reason; the text box opens only for "other".
  exit: { codes: { reason_code: EXIT_REASONS }, required: [], text: "other" },
};

// The browser's random per-session id (lib/track.js), as the collector checks it.
const SESSION_ID = /^[0-9a-f]{32}$/;

/** Why a free text is refused, or null. */
function textProblem(rule, codes, text) {
  if (typeof text !== "string") return "text: a string";
  if (rule === false) return "text: this survey takes no text";
  if (typeof rule === "string" && codes.reason_code !== rule) return `text: only with reason_code ${rule}`;
  if ([...text].length > MAX_SURVEY_TEXT) return `text: at most ${MAX_SURVEY_TEXT} characters`;
  return null;
}

/**
 * Checks a survey answer against surveys.md: a known survey, a session id,
 * only that survey's codes with allowed values, at least one answer, and free
 * text only where the survey has a text box and within its length. Anything
 * else is refused whole. The reason names the field, never the value.
 * → { ok: true, surveyId, sessionId, codes, text (null when none) } | { ok: false, error }
 */
export function validateSurveyAnswer(body) {
  if (!body || typeof body !== "object" || Array.isArray(body)) return { ok: false, error: "a survey answer object required" };
  for (const key of Object.keys(body)) {
    if (!["survey_id", "session_id", "codes", "text"].includes(key)) return { ok: false, error: `field ${key} is not accepted` };
  }
  const { survey_id: surveyId, session_id: sessionId, codes = {}, text = null } = body;
  if (typeof surveyId !== "string" || !Object.hasOwn(SURVEYS, surveyId)) return { ok: false, error: "survey_id: not in surveys.md" };
  if (typeof sessionId !== "string" || !SESSION_ID.test(sessionId)) return { ok: false, error: "session_id: 32 hex characters" };
  if (!codes || typeof codes !== "object" || Array.isArray(codes)) return { ok: false, error: "codes: an object" };

  const survey = SURVEYS[surveyId];
  for (const [name, value] of Object.entries(codes)) {
    if (!Object.hasOwn(survey.codes, name)) return { ok: false, error: `code ${name} is not asked in the ${surveyId} survey` };
    if (!survey.codes[name].includes(value)) return { ok: false, error: `code ${name}: one of ${survey.codes[name].join(", ")}` };
  }
  for (const name of survey.required) {
    if (!Object.hasOwn(codes, name)) return { ok: false, error: `code ${name} is required in the ${surveyId} survey` };
  }
  if (Object.keys(codes).length === 0) return { ok: false, error: "codes: at least one answer" };

  let kept = null;
  if (text !== null) {
    const problem = textProblem(survey.text, codes, text);
    if (problem) return { ok: false, error: problem };
    kept = text.trim() === "" ? null : text;
  }
  return { ok: true, surveyId, sessionId, codes: { ...codes }, text: kept };
}
