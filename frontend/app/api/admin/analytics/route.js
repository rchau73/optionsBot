import path from "node:path";
import { adminRequest } from "@/lib/auth/admin";
import { authLog } from "@/lib/auth/log";
import { analyticsSalt } from "@/lib/analytics/config";
import { listWeeks, readReport, reportsDir } from "@/lib/analytics/reports";
import { REPORT_ENVS, isoWeekOf, parseAnalyticsQuery } from "@/lib/analytics/usageView";

export const dynamic = "force-dynamic";

/**
 * GET /api/admin/analytics?env=stable|mvp&week=YYYY-Www (#445) → the weekly
 * usage report of an environment, as cmd/usage wrote it (counts only):
 * { off, env, envs, current_week, weeks (oldest first), week, report, file,
 * written_at, consent }. No week → the latest report; none at all → week and
 * report null. A week without a report → 404 with the same fields (report
 * null), so the page can still move to another week. ANALYTICS_SALT unset →
 * { off: true } and no figure. consent ({ yes, no, unasked } users) only for
 * this monitor's own environment: the other one's users are not here.
 * Read-only. Admins only: a manager or a user gets 403.
 */
export async function GET(request) {
  const { auth, response } = await adminRequest(request);
  if (response) return response;

  const own = process.env.MONITOR_DEPLOYMENT ?? null;
  const q = parseAnalyticsQuery(new URL(request.url).searchParams, own);
  if (!q.ok) return Response.json({ error: q.error }, { status: 400 });

  const headers = { "Cache-Control": "no-store" };
  const base = { env: q.env, envs: REPORT_ENVS, current_week: isoWeekOf(new Date()) };
  if (!analyticsSalt()) return Response.json({ ...base, off: true }, { headers });

  const dir = path.join(reportsDir(), q.env);
  try {
    const weeks = await listWeeks(dir);
    const week = q.week ?? weeks.at(-1) ?? null;
    const consent = q.env === own ? auth.store.consentCounts() : null;
    const body = { ...base, off: false, weeks, week, report: null, file: null, written_at: null, consent };
    if (!week) return Response.json(body, { headers });
    const found = await readReport(dir, week);
    if (!found) return Response.json({ ...body, error: `no report for ${week}` }, { status: 404, headers });
    return Response.json({ ...body, report: found.report, file: `${week}.json`, written_at: found.writtenAt }, { headers });
  } catch (err) {
    authLog("ERROR", "usage report read failed", { env: q.env, week: q.week, error: err.message });
    return Response.json({ error: "the usage report could not be read: see the monitor's logs" }, { status: 500 });
  }
}
