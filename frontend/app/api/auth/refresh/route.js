import { getAuth } from "@/lib/auth/server";
import { accessCookie, clearedCookies, cookieSecure, readCookie, REFRESH_COOKIE } from "@/lib/auth/tokens";

export const dynamic = "force-dynamic";

/**
 * Silent refresh: a live refresh cookie → a new 15-minute access cookie. The
 * refresh token is reusable until logout (several tabs refresh with it); an
 * unknown or expired one clears both cookies and answers 401.
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
    return Response.json({ error: "session expired: sign in again" }, { status: 401, headers });
  }
  headers.append("Set-Cookie", accessCookie(result.access, secure));
  return Response.json({ user: { username: result.user.username, role: result.user.role } }, { headers });
}
