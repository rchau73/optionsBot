import { adminRequest } from "@/lib/auth/admin";
import { authLog } from "@/lib/auth/log";
import { doraDir, readDora } from "@/lib/analytics/delivery";

export const dynamic = "force-dynamic";

/**
 * GET /api/admin/analytics/delivery (#473) → the delivery report cmd/dora
 * wrote, as written: { report, file, written_at }, both environments in it
 * (the page's switch picks one). No dora.json yet → report null (200): the
 * page shows what will appear. Read-only, never recomputed. Admins only: a
 * manager or a user gets 403, as the Usage topic.
 */
export async function GET(request) {
  const { response } = await adminRequest(request);
  if (response) return response;
  const headers = { "Cache-Control": "no-store" };
  try {
    const found = await readDora(doraDir());
    if (!found) return Response.json({ report: null, file: null, written_at: null }, { headers });
    return Response.json({ report: found.report, file: "dora.json", written_at: found.writtenAt }, { headers });
  } catch (err) {
    authLog("ERROR", "delivery report read failed", { error: err.message });
    return Response.json({ error: "the delivery report could not be read: see the monitor's logs" }, { status: 500 });
  }
}
