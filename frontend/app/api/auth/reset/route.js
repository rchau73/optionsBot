import { clientIp, getAuth, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";

export const dynamic = "force-dynamic";

/**
 * Set a new password with a reset link (#307): POST {"token", "next"} → 200 {}.
 * Public like sign-in (proxy.js lets it through) and under the same per-IP
 * limit (429 with Retry-After). 400 with the reason for a password under 12
 * characters; 400 "link expired or already used" for an unknown, used,
 * expired or replaced link. Success ends every session of the person; they
 * sign in afterwards. Same-origin JSON only.
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

  const result = await auth.resetWithLink({ token: body?.token, next: body?.next, ip: clientIp(request) });
  if (!result.ok) {
    const headers = result.retryAfter ? { "Retry-After": String(result.retryAfter) } : {};
    return Response.json({ error: result.error }, { status: result.status, headers });
  }
  return Response.json({}, { headers: { "Cache-Control": "no-store" } });
}
