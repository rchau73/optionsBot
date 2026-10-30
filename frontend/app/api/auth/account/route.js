import { requireRole, sameOrigin } from "@/lib/auth/server";
import { clearedCookies, cookieSecure } from "@/lib/auth/tokens";

export const dynamic = "force-dynamic";

/**
 * DELETE /api/auth/account (body {}) → 200 {"events_removed" or
 * "events_error", "survey_answers_removed" or "survey_answers_error"} and
 * both cookies cleared (#543): the signed-in person deletes their own
 * account — sessions, consent, analytics events, survey answers
 * (AuthService.deleteOwnAccount). Any role, except the last admin (409 with
 * the reason). Live session re-read like every state-changing route,
 * same-origin JSON only; audited as account_deleted. Exchange accounts and
 * funds are not touched.
 */
export async function DELETE(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  const { ok, status, error, ...rest } = auth.deleteOwnAccount(user);
  if (!ok) return Response.json({ error }, { status });
  const headers = new Headers({ "Cache-Control": "no-store" });
  for (const c of clearedCookies(cookieSecure(request))) headers.append("Set-Cookie", c);
  return Response.json(rest, { headers });
}
