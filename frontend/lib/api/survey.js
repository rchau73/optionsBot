// The person's analytics consent and surveys: the monthly NPS card, the four answers card and survey answers.

import { losingWeek } from "../analytics/nps.js";
import { fetchBotPerformance, fetchMonitorConfig } from "./bots.js";
import { apiFetch, jsonOrNull, sendJSON } from "./http.js";

/**
 * The signed-in person's stored analytics answer (#363, #424): true, false or
 * null (never answered). Anything but a 200 with a boolean — signed out, an
 * error, no network — is null, so nothing is sent on a guess. Never throws.
 */
export async function fetchConsent(signal) {
  try {
    const res = await apiFetch("/api/consent", { cache: "no-store", signal });
    const body = res.ok ? await jsonOrNull(res) : null;
    return typeof body?.consent === "boolean" ? body.consent : null;
  } catch {
    return null;
  }
}

/**
 * Whether the monthly NPS card is due for the signed-in person (#446), with
 * the page's losing-week rule applied: { due, optedOut }. The server decides
 * from the user's row; the page reads only the sign of each bot's last 7 days
 * (/api/performance?range=7d, never sent or stored). Never throws: any
 * failure is not due.
 */
export async function fetchNpsDue(signal) {
  try {
    const state = await fetchNpsState(signal);
    if (!state) return { due: false, optedOut: null };
    const optedOut = state.opted_out === true;
    if (state.due !== true) return { due: false, optedOut };
    const { names } = await fetchMonitorConfig(signal);
    const performances = await Promise.all(names.map((n) => fetchBotPerformance(n, "7d", signal).catch(() => null)));
    return { due: !losingWeek(performances), optedOut };
  } catch {
    return { due: false, optedOut: null };
  }
}

/** The server's NPS view for the signed-in person (#446): { due, reason, opted_out }, or null when it cannot be read. Never throws. */
export async function fetchNpsState(signal) {
  try {
    const res = await apiFetch("/api/survey/nps", { cache: "no-store", signal });
    return res.ok ? await jsonOrNull(res) : null;
  } catch {
    return null;
  }
}

/** The NPS card's state changes (#446): "shown" | "opt_out" | "opt_in" → the server's view; throws with its reason. */
export async function sendNpsAction(action) {
  return sendJSON("/api/survey/nps", "POST", { action });
}

/** Whether the four answers card is due for the signed-in person (#498): true only on the server's yes; any failure is not due. Never throws. */
export async function fetchAnswersDue(signal) {
  try {
    const res = await apiFetch("/api/survey/answers", { cache: "no-store", signal });
    const body = res.ok ? await jsonOrNull(res) : null;
    return body?.due === true;
  } catch {
    return false;
  }
}

/** The four answers card was shown (#498): the server keeps the day; throws with its reason. */
export async function sendAnswersShown() {
  return sendJSON("/api/survey/answers", "POST", { action: "shown" });
}

/** One survey answer (#363): { survey_id, session_id, codes, text? }; throws with the server's reason. */
export async function sendSurveyAnswer(answer) {
  await sendJSON("/api/survey", "POST", answer);
}
