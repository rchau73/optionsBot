import { adminRequest, serviceAnswer } from "@/lib/auth/admin";

export const dynamic = "force-dynamic";

/**
 * POST /api/admin/users/<id>/signout (body {}) → {}: AuthService.signOutEverywhere —
 * every session of that user ends. Admins only, same-origin JSON.
 */
export async function POST(request, { params }) {
  const { auth, user, id, response } = await adminRequest(request, { write: true, params });
  if (response) return response;
  return serviceAnswer(auth.signOutEverywhere(user, id));
}
