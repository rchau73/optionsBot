import { adminRequest, serviceAnswer } from "@/lib/auth/admin";

export const dynamic = "force-dynamic";

/**
 * POST /api/admin/users/<id>/unlock (body {}) → {"unlocked": true|false}:
 * AuthService.unlockUser (#846) — the username's sign-in lock and recent
 * failures go; false when it was not locked (nothing changes, no audit line).
 * Admins only, same-origin JSON; sessions and the password are untouched.
 */
export async function POST(request, { params }) {
  const { auth, user, id, response } = await adminRequest(request, { write: true, params });
  if (response) return response;
  const result = auth.unlockUser(user, id);
  return serviceAnswer(result, { unlocked: result.unlocked });
}
