import { requireRole, sameOrigin } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };

/** What follows each answer, in plain words. */
const CONSENT_NOTES = {
  true: "Usage analytics is on for you: the monitor records which screens and panels you use, never amounts or positions.",
  false: "Usage analytics is off for you: nothing more is collected. Events already stored stay until you ask for their deletion.",
};

/**
 * GET /api/consent → {"consent": true | false | null} (null = never answered)
 * for the signed-in person, re-read from the user database (#363, event plan §5).
 */
export async function GET(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  return Response.json({ consent: auth.store.analyticsConsent(user.id) }, { headers: NO_STORE });
}

/**
 * POST /api/consent {"consent": true | false} → {"consent", "note"}: the
 * person's own answer to usage analytics. Signed-in, same-origin JSON only;
 * audited as analytics_consent_changed. No stops collection at once (POST
 * /api/events answers 403); events already stored stay until the person asks
 * for their deletion (scripts/analytics-user.mjs delete).
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
  if (keys.length !== 1 || keys[0] !== "consent" || typeof body.consent !== "boolean") {
    return Response.json({ error: 'body: {"consent": true} or {"consent": false}' }, { status: 400 });
  }
  auth.setAnalyticsConsent(user, body.consent);
  return Response.json({ consent: body.consent, note: CONSENT_NOTES[body.consent] }, { headers: NO_STORE });
}
