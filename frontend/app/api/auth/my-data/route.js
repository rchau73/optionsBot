import { analyticsSalt, eventsPath } from "@/lib/analytics/config";
import { auditPath } from "@/lib/auth/audit";
import { authLog } from "@/lib/auth/log";
import { MY_DATA_DOWNLOADS } from "@/lib/auth/limits";
import { assembleMyData } from "@/lib/auth/mydata";
import { authDbPath, requireRole } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

/**
 * GET /api/auth/my-data (#750) → one JSON file of what the monitor holds about
 * the signed-in person (lib/auth/mydata.js): their account without the
 * password hash, consent, live sessions, survey answers, analytics events and
 * the audit lines where they acted. Any role; the live session is re-read like
 * a state-changing route (#171), never the read-only service token. At most 3
 * downloads an hour per person (429, nothing written). Each download writes
 * one my_data_exported audit line with the counts per section, never the content.
 */
export async function GET(request) {
  const { auth, user, sid, response } = await requireRole(request, "user");
  if (response) return response;

  const wait = auth.myDataLimiter.take(user.id, auth.now());
  if (wait > 0) {
    authLog("WARN", "my data download rate limited", { username: user.username, retry_after_sec: wait });
    const minutes = Math.ceil(wait / 60);
    return Response.json(
      { error: `You can download your data ${MY_DATA_DOWNLOADS} times an hour. Try again in ${minutes} ${minutes === 1 ? "minute" : "minutes"}.` },
      { status: 429, headers: { "Retry-After": String(wait), "Cache-Control": "no-store" } },
    );
  }

  const db = authDbPath();
  const nowMs = auth.now();
  const { file, counts } = await assembleMyData({
    store: auth.store,
    user,
    sessions: () => auth.listSessions(user, sid),
    eventsFile: eventsPath(db),
    salt: analyticsSalt(),
    auditFile: auditPath(db),
    nowMs,
  });
  auth.recordAction(user, "my_data_exported", { counts });
  authLog("INFO", "my data exported", { username: user.username, counts });

  const day = new Date(nowMs).toISOString().slice(0, 10);
  return new Response(`${JSON.stringify(file, null, 2)}\n`, {
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      "Content-Disposition": `attachment; filename="my-data-${user.username}-${day}.json"`,
      "Cache-Control": "no-store",
    },
  });
}
