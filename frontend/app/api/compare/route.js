import { requireRole } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { compareDir, listReports } from "@/lib/compare";

export const dynamic = "force-dynamic";

/**
 * GET /api/compare (#302): the comparison reports cmd/compare wrote, newest
 * first (50 at most): id, underlying, base and proposed file names, from/to
 * and which of backtest, replay and stress ran. Managers and admins; read-only.
 */
export async function GET(request) {
  const { user, response } = await requireRole(request, "manager");
  if (response) return response;
  const { reports, skipped } = listReports(compareDir());
  if (skipped.length) {
    authLog("WARN", "comparison reports unreadable", { component: "compare", username: user.username, ids: skipped });
  }
  authLog("INFO", "comparison reports listed", { component: "compare", username: user.username, count: reports.length });
  return Response.json({ reports }, { headers: { "Cache-Control": "no-store" } });
}
