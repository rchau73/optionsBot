import { requireRole, sameOrigin } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

/**
 * DELETE /api/auth/sessions/<id> (body {}) → 200 {} (#584): the signed-in
 * person ends one of their own other sessions; its next refresh fails, so
 * that browser goes to the sign-in page within 15 minutes (the life of the
 * access token it holds). Someone else's or an unknown id → 404 and nothing
 * changes; the session in use → 409 "use Sign out". Any role; live session
 * re-read like every state-changing route, same-origin JSON only; audited as
 * session_ended with the short id.
 */
export async function DELETE(request, { params }) {
  const { auth, user, sid, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  const { id } = await params;
  const result = auth.endSession(user, sid, id);
  if (!result.ok) return Response.json({ error: result.error }, { status: result.status });
  return Response.json({}, { headers: { "Cache-Control": "no-store" } });
}
