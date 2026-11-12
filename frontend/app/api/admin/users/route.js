import { adminRequest, serviceAnswer } from "@/lib/auth/admin";
import { readAccounts } from "@/lib/auth/accountLinks";

export const dynamic = "force-dynamic";

/**
 * GET /api/admin/users → {"users": [{id, username, role, email, created_at,
 * last_sign_in_at, accounts}], "accounts": [{account_id, label, environment}]},
 * never a hash. `accounts` of a user are the account ids they are linked to
 * (#760); the top-level `accounts` are the registry's, the ones a link may
 * name ([] without a status file). Admins only (#306): the person is re-read
 * from the database, a manager or a user gets 403.
 */
export async function GET(request) {
  const { auth, response } = await adminRequest(request);
  if (response) return response;
  const accounts = readAccounts().map(({ account_id, label, environment }) => ({ account_id, label, environment }));
  return Response.json({ users: auth.listUsers(), accounts }, { headers: { "Cache-Control": "no-store" } });
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
