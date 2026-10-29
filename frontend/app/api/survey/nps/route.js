import { requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { npsNotDueReason } from "@/lib/analytics/nps";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };
const ACTIONS = ["shown", "opt_out", "opt_in"];

/** { due, reason (null when due), opted_out } for the signed-in person, from their row in auth.db. */
function npsView(auth, user, nowMs) {
  const state = auth.store.npsState(user.id);
  const reason = state ? npsNotDueReason(state, nowMs) : "unknown_user";
  return { due: reason === null, reason, opted_out: Boolean(state?.optedOut) };
}

/**
 * GET /api/survey/nps → {"due", "reason", "opted_out"} (#446, surveys.md §1):
 * whether the monthly NPS card is due for the signed-in person. Decided here,
 * from their row in auth.db (consent, account age, sign-ins, the day it was
 * last shown, opt-out), so another device never shows it again. The page adds
 * the two rules only it can see: the losing week and an open dialog.
 */
export async function GET(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  return Response.json(npsView(auth, user, Date.now()), { headers: NO_STORE });
}

/**
 * POST /api/survey/nps {"action": "shown" | "opt_out" | "opt_in"} → the view
 * above. "shown": the panel opened with the card in it — recorded only while
 * it is due, so the 30-day cap starts once whatever the devices do; 409 when
 * it is not due. "opt_out": "Don't ask me again". "opt_in": turned back on in
 * Account › Privacy. Signed-in, same-origin JSON only.
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
  if (keys.length !== 1 || keys[0] !== "action" || !ACTIONS.includes(body.action)) {
    return Response.json({ error: `body: {"action": one of ${ACTIONS.join(", ")}}` }, { status: 400 });
  }
  const nowMs = Date.now();
  if (body.action === "shown") {
    const view = npsView(auth, user, nowMs);
    if (!view.due) return Response.json({ ...view, error: "not due" }, { status: 409, headers: NO_STORE });
    auth.store.markNpsShown(user.id, nowMs);
  } else {
    auth.store.setNpsOptOut(user.id, body.action === "opt_out", nowMs);
  }
  authLog("INFO", "nps survey state", { component: "analytics", username: user.username, action: body.action });
  auth.recordAction(user, "nps_survey_changed", { change: body.action });
  return Response.json(npsView(auth, user, nowMs), { headers: NO_STORE });
}
