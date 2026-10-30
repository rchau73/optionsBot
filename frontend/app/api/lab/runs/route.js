import { requireRole } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { compareDir } from "@/lib/compare";
import { listRuns } from "@/lib/lab";

export const dynamic = "force-dynamic";

/**
 * GET /api/lab/runs (#539): the comparison runs cmd/compare wrote, newest
 * first (50 at most): time, the settings changed and the headline numbers in
 * percentages, each with the label "backtested, not a forecast" and the risk
 * beside the return. Managers and admins; read-only, applies nothing.
 */
export async function GET(request) {
  const { user, response } = await requireRole(request, "manager");
  if (response) return response;
  const { runs, skipped } = listRuns(compareDir());
  if (skipped.length) {
    authLog("WARN", "lab runs unreadable", { component: "lab", username: user.username, ids: skipped });
  }
  authLog("INFO", "lab runs listed", { component: "lab", username: user.username, count: runs.length });
  return Response.json({ runs }, { headers: { "Cache-Control": "no-store" } });
}
