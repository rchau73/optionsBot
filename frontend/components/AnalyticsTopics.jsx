import Link from "next/link";
import clsx from "clsx";
import { TOPICS, deliveryHref } from "@/lib/analytics/deliveryView";

const TARGET =
  "inline-flex min-h-11 items-center justify-center rounded border border-line px-3 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** Each topic's link: its own default environment (Usage: this monitor's, Delivery: stable). */
const HREF = { usage: "/admin/analytics", delivery: deliveryHref() };

/** The Analytics page's topic tabs (#473): Usage · Delivery (DORA), as links, the current one marked. */
export default function AnalyticsTopics({ topic }) {
  return (
    <nav aria-label="Topic" className="flex flex-wrap gap-1">
      {TOPICS.map((t) => (
        <Link
          key={t.key}
          href={HREF[t.key]}
          aria-current={t.key === topic ? "page" : undefined}
          className={clsx(TARGET, t.key === topic ? "bg-slate-700 font-semibold" : "hover:bg-line/40")}
        >
          {t.label}
        </Link>
      ))}
    </nav>
  );
}
