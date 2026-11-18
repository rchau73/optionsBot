import { getAuth } from "@/lib/auth/server";
import { accessCookie, clearedCookies, cookieSecure, readCookie, REFRESH_COOKIE, SESSION_HEADER } from "@/lib/auth/tokens";

export const dynamic = "force-dynamic";

/**
 * Silent refresh: a live refresh cookie → a new 15-minute access cookie. The
 * refresh token is reusable until logout (several tabs refresh with it); an
 * unknown or expired one clears both cookies and answers 401. One unused for
 * longer than its role's idle limit (#821) is ended: the 401 then carries
 * x-monitor-session: required and `idle_limit_hours`.
 */
export async function POST(request) {
  const secure = cookieSecure(request);
  let result = { ok: false };
  try {
    result = await (await getAuth()).refresh(readCookie(request, REFRESH_COOKIE));
  } catch {
    /* auth unavailable: same as no session */
  }
  const headers = new Headers({ "Cache-Control": "no-store" });
  if (!result.ok) {
    for (const c of clearedCookies(secure)) headers.append("Set-Cookie", c);
    if (result.idleLimitHours) {
      // Ended for inactivity (#821): the monitor's own 401, with the limit for the login page's message.
      headers.set(SESSION_HEADER, "required");
      return Response.json({ error: "session not used for too long: sign in again", idle_limit_hours: result.idleLimitHours }, { status: 401, headers });
    }
    return Response.json({ error: "session expired: sign in again" }, { status: 401, headers });
  }
  headers.append("Set-Cookie", accessCookie(result.access, secure));
  return Response.json({ user: { username: result.user.username, role: result.user.role } }, { headers });
}
