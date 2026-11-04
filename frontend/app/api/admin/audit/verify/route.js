import { adminRequest } from "@/lib/auth/admin";
import { auditPath } from "@/lib/auth/audit";
import { verifyAudit } from "@/lib/auth/auditread";
import { authDbPath } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";

export const dynamic = "force-dynamic";

/**
 * GET /api/admin/audit/verify (#619) → {"off", "ok", "checked", "legacy",
 * "legacy_before", "first_break"}: the hash chain of the whole audit trail
 * (archives oldest first, then the current file) checked line by line,
 * stopping at the first break ({file, archive, line, time, reason}). Legacy
 * lines (before the chain) are counted, not checked. An in-memory user
 * database has no trail (off: true). Read-only; one log line with the admin
 * and the result. Admins only: a manager or a user gets 403.
 */
export async function GET(request) {
  const { user, response } = await adminRequest(request);
  if (response) return response;

  const headers = { "Cache-Control": "no-store" };
  const file = auditPath(authDbPath());
  if (!file) return Response.json({ off: true, ok: null, checked: 0, legacy: 0, legacy_before: null, first_break: null }, { headers });
  try {
    const result = await verifyAudit(file);
    authLog(result.ok ? "INFO" : "WARN", "audit chain verified", {
      username: user.username,
      ok: result.ok,
      checked: result.checked,
      legacy: result.legacy,
      first_break: result.first_break,
    });
    return Response.json({ off: false, ...result }, { headers });
  } catch (err) {
    authLog("ERROR", "audit verify failed", { username: user.username, file, error: err.message });
    return Response.json({ error: "the audit trail could not be checked: see the monitor's logs" }, { status: 500 });
  }
}
