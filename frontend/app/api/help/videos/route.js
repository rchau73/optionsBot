import { requireRole } from "@/lib/auth/server";
import { allVideos } from "@/lib/videos";

export const dynamic = "force-dynamic";

/**
 * GET /api/help/videos (#611, epic E11): the how-to video manifest, read-only,
 * any signed-in role. `{videos: [...]}`, each with id, title, task,
 * duration_sec, captions, tips and status. Nothing personal is read or logged.
 */
export async function GET(request) {
  const { response } = await requireRole(request, "user");
  if (response) return response;
  return Response.json({ videos: allVideos() }, { headers: { "Cache-Control": "no-store" } });
}
