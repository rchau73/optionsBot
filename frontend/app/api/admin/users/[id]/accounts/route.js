import { adminRequest, serviceAnswer } from "@/lib/auth/admin";
import { readAccounts } from "@/lib/auth/accountLinks";

export const dynamic = "force-dynamic";

/**
 * PUT /api/admin/users/<id>/accounts {"accounts": [account ids]} →
 * {"accounts", "added", "removed"} (#760): AuthService.setAccountLinks — the
 * manager's links become this list; [] removes them all, so the manager acts
 * on every account again. A new id must be in the account registry's status
 * file. Admins only, same-origin JSON; one audit line per account added
 * (account_link_added) or removed (account_link_removed).
 */
export async function PUT(request, { params }) {
  const { auth, user, id, body, response } = await adminRequest(request, { write: true, params });
  if (response) return response;
  const result = auth.setAccountLinks(user, id, body.accounts, readAccounts());
  const { ok, ...rest } = result;
  return serviceAnswer(result, ok ? rest : {});
}
