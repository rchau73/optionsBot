import { getAuth, sameOrigin, sessionUser } from "@/lib/auth/server";
import { clearedCookies, cookieSecure, readCookie, REFRESH_COOKIE } from "@/lib/auth/tokens";

export const dynamic = "force-dynamic";

/** Sign out: ends this session (its refresh token stops working) and clears both cookies. */
export async function POST(request) {
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  const user = await sessionUser(request);
  (await getAuth()).logout(readCookie(request, REFRESH_COOKIE), user?.username ?? null);
  const headers = new Headers({ "Cache-Control": "no-store" });
  for (const c of clearedCookies(cookieSecure(request))) headers.append("Set-Cookie", c);
  return Response.json({ ok: true }, { headers });
}
