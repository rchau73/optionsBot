import { adminRequest, serviceAnswer } from "@/lib/auth/admin";

export const dynamic = "force-dynamic";

/**
 * PATCH /api/admin/users/<id> {"role"} → {"user"}: AuthService.changeRole —
 * never zero admins (409 with the reason), every session of that user ends.
 * Admins only, same-origin JSON.
 */
export async function PATCH(request, { params }) {
  const { auth, user, id, body, response } = await adminRequest(request, { write: true, params });
  if (response) return response;
  const result = auth.changeRole(user, id, typeof body.role === "string" ? body.role : "");
  return serviceAnswer(result, result.ok ? { user: auth.listUsers().find((u) => u.id === id) } : {});
}

/**
 * DELETE /api/admin/users/<id> (body {}) → {"events_removed"} or
 * {"events_error"}: AuthService.deleteUser — never yourself, never the last
 * admin (409). Admins only, same-origin JSON.
 */
export async function DELETE(request, { params }) {
  const { auth, user, id, response } = await adminRequest(request, { write: true, params });
  if (response) return response;
  const result = auth.deleteUser(user, id);
  const { ok, ...rest } = result;
  return serviceAnswer(result, ok ? rest : {});
}
