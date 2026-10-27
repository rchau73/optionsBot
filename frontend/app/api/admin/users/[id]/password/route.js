import { adminRequest, serviceAnswer } from "@/lib/auth/admin";

export const dynamic = "force-dynamic";

/**
 * POST /api/admin/users/<id>/password {"password"} → {}: an admin sets a new
 * password (≥ 12 characters) and every session of that user ends — each of
 * their browsers must sign in again. Not for your own account (409: the menu
 * bar's "<name> ▾" → Change password asks for the current one). Audited as password_reset,
 * never with the password. Admins only, same-origin JSON.
 */
export async function POST(request, { params }) {
  const { auth, user, id, body, response } = await adminRequest(request, { write: true, params });
  if (response) return response;
  return serviceAnswer(await auth.resetPassword(user, id, body.password));
}
