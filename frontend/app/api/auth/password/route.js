import { clientIp, getAuth, sameOrigin } from "@/lib/auth/server";
import { ACCESS_COOKIE, accessCookie, cookieSecure, readCookie, refreshCookie, sessionRequired } from "@/lib/auth/tokens";
import { authLog } from "@/lib/auth/log";

export const dynamic = "force-dynamic";

/**
 * Change your own password (#212): POST {"current", "next"} with a live
 * session → 200 with new access and refresh cookies for this browser; every
 * other session of the person ends (their next refresh fails). 400 for a weak
 * new password, 401 "Current password is wrong" (counted like a failed
 * sign-in: 5 lock the username), 429 while locked, 401 without a session.
 * Same-origin JSON only; the service token never reaches it (proxy.js: GET
 * of /api/bots only).
 */
export async function POST(request) {
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  let body;
  try {
    body = await request.json();
  } catch {
    body = {};
  }

  let auth;
  try {
    auth = await getAuth();
  } catch (err) {
    authLog("ERROR", "auth unavailable", { error: err.message });
    return Response.json({ error: "sign-in is unavailable: see the monitor's logs" }, { status: 503 });
  }

  const result = await auth.changePassword({
    accessToken: readCookie(request, ACCESS_COOKIE),
    current: body?.current,
    next: body?.next,
    ip: clientIp(request),
  });
  if (!result.ok) {
    if (result.session) return sessionRequired();
    const headers = result.retryAfter ? { "Retry-After": String(result.retryAfter) } : {};
    return Response.json({ error: result.error }, { status: result.status, headers });
  }
  const secure = cookieSecure(request);
  const headers = new Headers({ "Cache-Control": "no-store" });
  headers.append("Set-Cookie", accessCookie(result.access, secure));
  headers.append("Set-Cookie", refreshCookie(result.refresh, secure));
  return Response.json({ user: { username: result.user.username, role: result.user.role } }, { headers });
}
