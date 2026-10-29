import { authDbPath, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { validateEvent } from "@/lib/analytics/plan";
import { validateSurveyAnswer } from "@/lib/analytics/surveys";
import { analyticsSalt, appendEvent, buildEntry, eventsPath } from "@/lib/analytics/collector";

export const dynamic = "force-dynamic";

// 500 characters of text at up to 4 bytes each, plus the codes.
const MAX_BODY = 4096;

/**
 * POST /api/survey {"survey_id", "session_id", "codes": {…}, "text"?} (#363,
 * docs/product/analytics/surveys.md): one in-product survey answer. Signed-in,
 * same-origin JSON, and only for a person who said yes to analytics — §1 and
 * §2 show their cards only then, and §3 stores no exit answer without it
 * until counsel answers C3. The survey id, every code and the text are
 * checked against surveys.md (lib/analytics/surveys.js) and anything else is
 * refused whole (400). The codes and text go to auth.db beside the user; the
 * survey_answered event carries the codes only, through the collector's
 * allow-list. 204 when both are stored; nothing is kept when either fails.
 */
export async function POST(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }

  const salt = analyticsSalt();
  const file = eventsPath(authDbPath());
  if (!salt || !file) return Response.json({ error: "analytics is off: ANALYTICS_SALT is not set" }, { status: 503 });
  if (auth.store.analyticsConsent(user.id) !== true) {
    return Response.json({ error: "analytics is off for this user: no consent" }, { status: 403 });
  }

  const raw = await request.text();
  if (raw.length > MAX_BODY) return Response.json({ error: "answer too large" }, { status: 413 });
  let body;
  try {
    body = JSON.parse(raw);
  } catch {
    body = null;
  }
  const answer = validateSurveyAnswer(body);
  // The survey's event, codes only: the collector's allow-list checks it too.
  const valid = answer.ok
    ? validateEvent({ event: "survey_answered", session_id: answer.sessionId, props: { survey_id: answer.surveyId, ...answer.codes } })
    : answer;
  if (!valid.ok) {
    // The reason names the field, never its value (it may be the free text).
    authLog("WARN", "survey answer refused", { component: "analytics", username: user.username, reason: valid.error });
    return Response.json({ error: valid.error }, { status: 400 });
  }

  const nowMs = Date.now();
  let id;
  try {
    id = auth.store.addSurveyAnswer(user.id, { surveyId: answer.surveyId, codes: answer.codes, text: answer.text }, nowMs);
  } catch (err) {
    authLog("ERROR", "survey answer write failed", { component: "analytics", username: user.username, survey_id: answer.surveyId, error: err.message });
    return Response.json({ error: "answer not stored: see the monitor's logs" }, { status: 500 });
  }
  if (!appendEvent(file, buildEntry({ valid, user, salt, nowMs }))) {
    auth.store.deleteSurveyAnswer(id);
    return Response.json({ error: "answer not stored: see the monitor's logs" }, { status: 500 });
  }
  authLog("INFO", "survey answer stored", { component: "analytics", username: user.username, survey_id: answer.surveyId, with_text: answer.text !== null });
  // The survey only, never the codes or the text (#490).
  auth.recordAction(user, "survey_answered", { survey_id: answer.surveyId });
  return new Response(null, { status: 204, headers: { "Cache-Control": "no-store" } });
}
