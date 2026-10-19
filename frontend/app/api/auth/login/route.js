import { clientIp, getAuth, sameOrigin } from "@/lib/auth/server";
import { accessCookie, cookieSecure, refreshCookie } from "@/lib/auth/tokens";
import { authLog } from "@/lib/auth/log";

export const dynamic = "force-dynamic";

/**
 * Sign in: POST {"username", "password"} → 200 {"user": {username, role}} with
 * the access and refresh cookies, or 401 "invalid username or password" (wrong
 * user, wrong password or a locked username alike), 429 when the IP tries too
 * often. Same-origin JSON only.
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

  const result = await auth.login({ username: body?.username, password: body?.password, ip: clientIp(request) });
  if (!result.ok) {
    const headers = result.retryAfter ? { "Retry-After": String(result.retryAfter) } : {};
    return Response.json({ error: result.error }, { status: result.status, headers });
  }
  const secure = cookieSecure(request);
  const headers = new Headers({ "Cache-Control": "no-store" });
  headers.append("Set-Cookie", accessCookie(result.access, secure));
  headers.append("Set-Cookie", refreshCookie(result.refresh, secure));
  return Response.json({ user: { username: result.user.username, role: result.user.role } }, { headers });
}
