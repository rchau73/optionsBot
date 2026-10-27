import { requireRole } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { compareDir, readReport, validId } from "@/lib/compare";

export const dynamic = "force-dynamic";

/**
 * GET /api/compare/<id> (#302): one report.json, served as stored (its
 * disclaimer included). The id must be a report folder's time
 * (20261012T093000Z); anything else is refused before the disk is touched.
 */
export async function GET(request, { params }) {
  const { user, response } = await requireRole(request, "manager");
  if (response) return response;
  const { id } = await params;
  if (!validId(id)) return Response.json({ error: "not a report id" }, { status: 400 });
  const r = readReport(compareDir(), id);
  if (r.missing) return Response.json({ error: "no such report" }, { status: 404 });
  if (r.error) {
    // The reason is the id only, never the file's content.
    authLog("ERROR", "comparison report unreadable", { component: "compare", username: user.username, id });
    return Response.json({ error: "the report cannot be read: see the monitor's logs" }, { status: 500 });
  }
  authLog("INFO", "comparison report read", { component: "compare", username: user.username, id });
  return new Response(r.text, { headers: { "Content-Type": "application/json", "Cache-Control": "no-store" } });
}
