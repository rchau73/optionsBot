import { adminRequest, serviceAnswer } from "@/lib/auth/admin";

export const dynamic = "force-dynamic";

/**
 * GET /api/admin/users → {"users": [{id, username, role, email, created_at,
 * last_sign_in_at}]}, never a hash. Admins only (#306): the person is re-read
 * from the database, a manager or a user gets 403.
 */
export async function GET(request) {
  const { auth, response } = await adminRequest(request);
  if (response) return response;
  return Response.json({ users: auth.listUsers() }, { headers: { "Cache-Control": "no-store" } });
}

/**
 * POST /api/admin/users {"username", "password", "role", "email"?} → 201
 * {"user"}. The sign-in username rule and the ≥ 12-character password rule;
 * 400 with the reason, 409 when the username is taken. Same-origin JSON only;
 * audited as user_created, never with the password.
 */
export async function POST(request) {
  const { auth, user, body, response } = await adminRequest(request, { write: true });
  if (response) return response;
  const result = await auth.createUser(user, body);
  if (!result.ok) return serviceAnswer(result);
  const created = auth.listUsers().find((u) => u.id === result.user.id);
  return Response.json({ user: created }, { status: 201, headers: { "Cache-Control": "no-store" } });
}
