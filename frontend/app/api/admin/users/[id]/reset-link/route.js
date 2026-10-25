import { adminRequest, serviceAnswer } from "@/lib/auth/admin";

export const dynamic = "force-dynamic";

/**
 * POST /api/admin/users/<id>/reset-link {} → {"url", "expires_at"} (#307):
 * a single-use link, valid 30 minutes, with which the person sets a new
 * password at /reset. Shown once: only the token's SHA-256 is stored, and a
 * new link voids the older ones. Not for your own account (409). Audited as
 * reset_link_issued, never with the token. Admins only, same-origin JSON.
 */
export async function POST(request, { params }) {
  const { auth, user, id, response } = await adminRequest(request, { write: true, params });
  if (response) return response;
  const result = auth.issueResetLink(user, id);
  if (!result.ok) return serviceAnswer(result);
  // adminRequest checked that Origin is this host, so the link points back here.
  const url = `${request.headers.get("origin")}/reset?token=${result.token}`;
  return serviceAnswer(result, { url, expires_at: result.expires_at });
}
