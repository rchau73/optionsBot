import { adminRequest } from "@/lib/auth/admin";
import { auditPath } from "@/lib/auth/audit";
import { auditCsvStream, planAuditExport } from "@/lib/auth/auditexport";
import { authDbPath } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { parseAuditQuery } from "@/lib/audit";

export const dynamic = "force-dynamic";

/**
 * GET /api/admin/audit/export?user=&action=&from=&to= (#702) → the lines of
 * these filters (those of GET /api/admin/audit) as a CSV file, newest first,
 * the newest 50,000 at most. X-Audit-Lines says how many, X-Audit-Cut whether
 * the cap cut it, X-Audit-Skipped how many damaged lines were left out. An
 * in-memory user database has no trail: 409. Reads the trail, changes none of
 * its lines; when the file ends (or the download stops) one audit_exported
 * line records the filters and the count, never a line. Admins only: a manager
 * or a user gets 403 before any file is opened.
 */
export async function GET(request) {
  const { auth, user, response } = await adminRequest(request);
  if (response) return response;

  const parsed = parseAuditQuery(new URL(request.url).searchParams);
  if (!parsed.ok) return Response.json({ error: parsed.error }, { status: 400 });

  const file = auditPath(authDbPath());
  if (!file) return Response.json({ error: "the trail is off: this monitor keeps its users in memory" }, { status: 409 });

  const { query } = parsed;
  const filters = { user: query.user, action: query.action, from: query.from, to: query.to };
  let plan;
  try {
    plan = await planAuditExport(file, query);
  } catch (err) {
    authLog("ERROR", "audit export failed", { username: user.username, file, error: err.message });
    return Response.json({ error: "the audit trail could not be read: see the monitor's logs" }, { status: 500 });
  }

  const onDone = ({ lines, complete, error }) => {
    auth.recordAction(user, "audit_exported", { filters, lines, cut: plan.cut, skipped: plan.skipped, complete });
    if (error) authLog("ERROR", "audit export failed", { username: user.username, file, lines, error: error.message });
    else authLog("INFO", "audit exported", { username: user.username, filters, lines, cut: plan.cut, complete });
  };
  const day = new Date().toISOString().slice(0, 10);
  return new Response(auditCsvStream(file, query, plan, onDone), {
    headers: {
      "Content-Type": "text/csv; charset=utf-8",
      "Content-Disposition": `attachment; filename="audit-trail-${day}.csv"`,
      "Cache-Control": "no-store",
      "X-Audit-Lines": String(plan.lines),
      "X-Audit-Cut": String(plan.cut),
      "X-Audit-Skipped": String(plan.skipped),
    },
  });
}
