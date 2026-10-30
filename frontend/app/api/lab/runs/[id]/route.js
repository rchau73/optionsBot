import { requireRole } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { compareDir, validId } from "@/lib/compare";
import { readRun } from "@/lib/lab";

export const dynamic = "force-dynamic";

/**
 * GET /api/lab/runs/<id> (#539): one run — its list row (label, settings
 * changed, headline numbers), the report.json content as `report` and
 * `analysis`: "withheld pending legal review" until the L10 gate opens
 * (lib/lab.js). The id is a report folder's time (20261012T093000Z) under
 * data/compare/ and nothing else: any other id is 404 before the disk is
 * touched. Managers and admins; read-only, applies nothing.
 */
export async function GET(request, { params }) {
  const { user, response } = await requireRole(request, "manager");
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
  authLog("INFO", "lab run read", { component: "lab", username: user.username, id });
  return Response.json(r.run, { headers: { "Cache-Control": "no-store" } });
}
