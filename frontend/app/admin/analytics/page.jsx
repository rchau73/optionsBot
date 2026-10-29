import Link from "next/link";
import UsageAnalytics from "@/components/UsageAnalytics";
import { viewer } from "@/lib/auth/viewer";
import { parseAnalyticsQuery } from "@/lib/analytics/usageView";

export const metadata = { title: "Analytics · optionsBot monitor" };

/**
 * /admin/analytics?env=&week= (#445): admins read the weekly usage report.
 * GET /api/admin/analytics enforces the role (403 for anyone else); this only
 * picks the view.
 */
export default async function AnalyticsPage({ searchParams }) {
  const me = await viewer();
  const admin = me?.role === "admin";
  const q = parseAnalyticsQuery(await searchParams, process.env.MONITOR_DEPLOYMENT ?? null);
  return (
    <>
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
        <h1 className="mr-2 text-lg font-bold">optionsBot · Analytics</h1>
        <Link href="/" className="text-sm text-muted underline hover:text-inherit">
          ← Monitor
        </Link>
      </header>
      <main className="mx-auto max-w-6xl p-4">
        {!admin ? (
          <p role="alert" className="rounded border border-line bg-panel px-3 py-2">
            Only an admin reads the usage analytics.
          </p>
        ) : q.ok ? (
          <UsageAnalytics env={q.env} week={q.week} />
        ) : (
          <p role="alert" className="rounded border border-line bg-panel px-3 py-2">
            {q.error}
          </p>
        )}
      </main>
    </>
  );
}
