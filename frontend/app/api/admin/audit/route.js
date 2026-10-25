import { adminRequest } from "@/lib/auth/admin";
import { auditPath } from "@/lib/auth/audit";
import { readAudit } from "@/lib/auth/auditread";
import { authDbPath } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { parseAuditQuery } from "@/lib/audit";

export const dynamic = "force-dynamic";

/**
 * GET /api/admin/audit?user=&action=&from=&to=&before=&limit= (#308, #249) →
 * {"off", "entries", "skipped", "next_before"}: the audit trail newest first,
 * each line as stored. user = who acted, action = one of the known actions,
 * from / to = UTC days, before = the previous answer's next_before, limit
 * 1–1000 (default 100). A malformed line is skipped and counted; a missing
 * file is an empty list; an in-memory user database has no trail (off: true).
 * Read-only. Admins only: a manager or a user gets 403.
 */
export async function GET(request) {
  const { response } = await adminRequest(request);
  if (response) return response;

  const parsed = parseAuditQuery(new URL(request.url).searchParams);
  if (!parsed.ok) return Response.json({ error: parsed.error }, { status: 400 });

  const headers = { "Cache-Control": "no-store" };
  const file = auditPath(authDbPath());
  if (!file) return Response.json({ off: true, entries: [], skipped: 0, next_before: null }, { headers });
  try {
    return Response.json({ off: false, ...(await readAudit(file, parsed.query)) }, { headers });
  } catch (err) {
    authLog("ERROR", "audit read failed", { file, error: err.message });
    return Response.json({ error: "the audit trail could not be read: see the monitor's logs" }, { status: 500 });
  }
}
