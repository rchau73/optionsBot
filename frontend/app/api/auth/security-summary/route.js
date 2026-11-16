import { requireRole } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

/**
 * GET /api/auth/security-summary → 200 {"previous_sign_in", "first_sign_in",
 * "failed_since", "notice_id", "locked_until"?} (#803): the signed-in
 * person's previous sign-in and the wrong passwords typed for their username
 * since. Any role, their own row only; never an IP, a browser, a token or
 * another person's data. The live session is re-read like
 * GET /api/auth/sessions, so an ended session hears 401.
 */
export async function GET(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  const summary = auth.signInSummary(user);
  if (!summary) return Response.json({ error: "sign in required" }, { status: 401 });
  return Response.json(summary, { headers: { "Cache-Control": "no-store" } });
}
