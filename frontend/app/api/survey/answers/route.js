import { requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { answersNotDueReason } from "@/lib/analytics/answers";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };

/** { due, reason (null when due) } for the signed-in person, from their row in auth.db. */
function answersView(auth, user, nowMs) {
  const state = auth.store.answersState(user.id);
  const reason = state ? answersNotDueReason(state, nowMs) : "unknown_user";
  return { due: reason === null, reason };
}

/**
 * GET /api/survey/answers → {"due", "reason"} (#498, surveys.md §2): whether
 * the four answers card is due for the signed-in person. Decided here, from
 * their row in auth.db (consent, account age, sign-ins, the day this card and
 * the NPS card were last shown), so another device never shows it again.
 */
export async function GET(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  return Response.json(answersView(auth, user, Date.now()), { headers: NO_STORE });
}

/**
 * POST /api/survey/answers {"action": "shown"} → the view above: the panel
 * opened with the card in it. Recorded only while it is due, so the 30-day
 * cap starts once whatever the tabs and devices do; 409 when it is not due.
 * The answers themselves go through POST /api/survey. Signed-in, same-origin
 * JSON only.
 */
export async function POST(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  let body;
  try {
    body = await request.json();
  } catch {
    body = null;
  }
  const keys = body && typeof body === "object" && !Array.isArray(body) ? Object.keys(body) : [];
  if (keys.length !== 1 || body.action !== "shown") {
    return Response.json({ error: 'body: {"action": "shown"}' }, { status: 400 });
  }
  const nowMs = Date.now();
  const view = answersView(auth, user, nowMs);
  if (!view.due) return Response.json({ ...view, error: "not due" }, { status: 409, headers: NO_STORE });
  auth.store.markAnswersShown(user.id, nowMs);
  authLog("INFO", "answers survey shown", { component: "analytics", username: user.username });
  auth.recordAction(user, "answers_survey_shown", {});
  return Response.json(answersView(auth, user, nowMs), { headers: NO_STORE });
}
