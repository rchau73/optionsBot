import { requireRole } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { authKey } from "@/lib/auth/tokens";
import { compareDir, validId } from "@/lib/compare";
import { readRun } from "@/lib/lab";
import { buildPreview, confirmToken, PREVIEW_AUDIT_ACTION } from "@/lib/labpreview";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };

/**
 * GET /api/lab/runs/<id>/preview (#682, epic E9): what confirming this run
 * would change — each setting with its tip, the account's value and the
 * proposed one, the account, the person, the numbers labelled as backtested,
 * the fragility flags and the audit line's text. `confirm_token` is null when
 * the run needs the product owner, is stale or has no numbers (lib/labpreview.js).
 * Managers and admins. It changes no config and applies nothing; the only
 * write is its audit line (parameter_change_previewed), with both values.
 */
export async function GET(request, { params }) {
  const { auth, user, response } = await requireRole(request, "manager");
  if (response) return response;
  const { id } = await params;
  if (!validId(id)) return Response.json({ error: "no such run" }, { status: 404 });
  const r = readRun(compareDir(), id);
  if (r.missing) return Response.json({ error: "no such run" }, { status: 404 });
  if (r.error) {
    // The reason is the id only, never the file's content.
    authLog("ERROR", "lab run unreadable", { component: "lab", username: user.username, id });
    return Response.json({ error: "the run cannot be read: see the monitor's logs" }, { status: 500 });
  }
  const preview = buildPreview(r.run, user.username, Date.now());
  if (!preview) return Response.json({ error: "this run changed no setting: there is nothing to confirm" }, { status: 409, headers: NO_STORE });
  if (preview.confirmable) preview.confirm_token = confirmToken(preview, authKey());
  authLog("INFO", "lab apply previewed", {
    component: "lab",
    username: user.username,
    id,
    stale: preview.stale,
    needs_product_owner: preview.needs_product_owner,
    confirmable: preview.confirmable,
  });
  auth.recordAction(user, PREVIEW_AUDIT_ACTION, {
    run_id: id,
    underlying: preview.account.underlying,
    config: preview.account.config,
    changes: preview.changes.map((c) => ({ key: c.key, current: c.current, proposed: c.proposed })),
    needs_product_owner: preview.needs_product_owner,
    stale: preview.stale,
    line: preview.audit_line,
  });
  return Response.json(preview, { headers: NO_STORE });
}
