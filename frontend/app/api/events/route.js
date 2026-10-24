import { authDbPath, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { validateEvent } from "@/lib/analytics/plan";
import { analyticsSalt, appendEvent, buildEntry, eventsPath } from "@/lib/analytics/collector";

export const dynamic = "force-dynamic";

const MAX_BODY = 2048;

/**
 * Analytics collector (#193, docs/product/analytics/event-plan.md): POST
 * /api/events {"event", "session_id", "props"} from lib/track.js. Stores the
 * event in data/monitor/events.jsonl only when the person is signed in (re-read
 * from the user database), the request is same-origin JSON, they said yes to
 * analytics, and the event and every property are in the plan's allow-list —
 * anything else is refused whole (400) and nothing is written. 204 when stored.
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

  const text = await request.text();
  if (text.length > MAX_BODY) return Response.json({ error: "event too large" }, { status: 413 });
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    body = null;
  }
  const valid = validateEvent(body);
  if (!valid.ok) {
    // The reason names the field, never its value (it may be what the plan keeps out).
    authLog("WARN", "analytics event refused", { component: "analytics", username: user.username, reason: valid.error });
    return Response.json({ error: valid.error }, { status: 400 });
  }

  if (!appendEvent(file, buildEntry({ valid, user, salt, nowMs: Date.now() }))) {
    return Response.json({ error: "event not stored: see the monitor's logs" }, { status: 500 });
  }
  return new Response(null, { status: 204, headers: { "Cache-Control": "no-store" } });
}
