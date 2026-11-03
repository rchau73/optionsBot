import { requireRole } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

/**
 * GET /api/auth/sessions → 200 {"sessions": [{id, browser, created_at,
 * last_used_at, current}]} (#584): the signed-in person's own live sessions,
 * last used first, `current: true` on the one making the request. No token,
 * hash or address. Any role; the live session is re-read like a
 * state-changing route (#171), so a session ended elsewhere hears 401.
 */
export async function GET(request) {
  const { auth, user, sid, response } = await requireRole(request, "user");
  if (response) return response;
  return Response.json({ sessions: auth.listSessions(user, sid) }, { headers: { "Cache-Control": "no-store" } });
}
