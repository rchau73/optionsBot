"use client";

import Link from "next/link";
import clsx from "clsx";
import { useEffect, useState } from "react";
import Panel from "./Panel";
import { Attention, DataBehind, Lapsed, QuestionCards } from "./UsageCards";
import { fetchUsageReport } from "@/lib/api/admin";
import { REPORT_ENVS, SMALL_SAMPLE, analyticsHref, isSmallSample, lapsedView, neighbourWeeks } from "@/lib/analytics/usageView";

const TARGET =
  "inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-line px-3 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** The page's promise, under its title. */
export const COUNTS_ONLY =
  "Counts of people and actions only: no names, no ids, no amounts. Only people who accepted analytics are counted.";

/** The setup steps while ANALYTICS_SALT is unset: never its value. */
export const SETUP_STEPS = [
  "Add ANALYTICS_SALT (at least 16 random characters) to this environment's .env. Keep it there only: never paste it in a ticket, a chat or a commit.",
  "Restart the monitor: scripts/envctl.sh <stable|mvp> restart monitor.",
  "People say yes to analytics on the consent notice; nothing is collected from anyone else.",
  "On Monday the weekly job (usage.sh) writes the report of the week before; it shows here.",
];

/**
 * The admin Analytics page (#445): the weekly usage report of one
 * environment, attention first, then the Q1–Q8 cards and the data behind
 * them. env and week come from the URL; the switch and the arrows are links.
 */
export default function UsageAnalytics({ env, week = null }) {
  const [state, setState] = useState({ key: null, data: null, error: null });
  const key = `${env}/${week ?? ""}`;

  useEffect(() => {
    const ctrl = new AbortController();
    fetchUsageReport(env, week, ctrl.signal).then(
      (data) => setState({ key, data, error: null }),
      (err) => {
        if (!ctrl.signal.aborted) setState({ key, data: null, error: err.message });
      },
    );
    return () => ctrl.abort();
  }, [env, week, key]);

  const current = state.key === key ? state : { data: null, error: null };
  return <AnalyticsView env={env} data={current.data} error={current.error} />;
}

/** The page for a route answer `data` (null while loading) or an `error`. */
export function AnalyticsView({ env, data, error = null }) {
  return (
    <div className="space-y-4">
      <Header env={env} data={data} />
      <Body data={data} error={error} />
    </div>
  );
}

function Header({ env, data }) {
  const weeks = data?.weeks ?? [];
  const { older, newer } = neighbourWeeks(weeks, data?.week ?? null);
  const arrow = (to, label, text) =>
    to ? (
      <Link href={analyticsHref(env, to)} aria-label={`${label}: ${to}`} className={clsx(TARGET, "hover:bg-line/40")}>
        {text}
      </Link>
    ) : (
      <span aria-disabled="true" aria-label={`${label}: none`} className={clsx(TARGET, "opacity-40")}>
        {text}
      </span>
    );
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-3">
        <nav aria-label="Environment" className="flex gap-1">
          {REPORT_ENVS.map((e) => (
            <Link
              key={e}
              href={analyticsHref(e)}
              aria-current={e === env ? "page" : undefined}
              className={clsx(TARGET, e === env ? "bg-slate-700 font-semibold" : "hover:bg-line/40")}
            >
              {e}
            </Link>
          ))}
        </nav>
        {data && !data.off ? (
          <div className="flex items-center gap-1">
            {arrow(older, "Older week", "←")}
            <span className="px-2 font-semibold tabular-nums">{data.week ?? "no report"}</span>
            {arrow(newer, "Newer week", "→")}
          </div>
        ) : null}
      </div>
      <p className="text-sm text-muted">{COUNTS_ONLY}</p>
    </div>
  );
}

function Notice({ children, role = "status" }) {
  return (
    <p role={role} className="rounded border border-line bg-panel px-3 py-2 text-sm">
      {children}
    </p>
  );
}

function Body({ data, error }) {
  if (error) return <Notice role="alert">The usage report could not be loaded: {error}</Notice>;
  if (!data) return <Notice>Loading the usage report…</Notice>;
  if (data.off) {
    return (
      <Panel title="Analytics is off">
        <p className="mb-2 text-sm">ANALYTICS_SALT is not set in this monitor, so nothing is collected and no figure is shown. To turn it on:</p>
        <ol className="list-decimal space-y-1 pl-5 text-sm">
          {SETUP_STEPS.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
      </Panel>
    );
  }
  if (data.missing) {
    return (
      <>
        <Notice>No report for {data.week}. Reports are written on Monday for the week before; pick another week with the arrows.</Notice>
        <DataBehind data={data} />
      </>
    );
  }
  if (!data.report) {
    return (
      <>
        <Notice>No usage report in {data.env} yet. The first one is written on the Monday after people accept analytics.</Notice>
        <DataBehind data={data} />
      </>
    );
  }
  const r = data.report;
  if (!r.events_in_week) {
    return (
      <>
        <Notice>No events in {r.week}. {r.no_events}</Notice>
        <DataBehind data={data} />
      </>
    );
  }
  return (
    <>
      {isSmallSample(r) ? (
        <Notice>
          Small sample: {r.active.week.users} active {r.active.week.users === 1 ? "person" : "people"} this week, under {SMALL_SAMPLE}. Read
          shares as anecdotes; the drop check does not judge a base this small.
        </Notice>
      ) : null}
      <Attention report={r} />
      {lapsedView(r)?.rising ? <Lapsed report={r} /> : null}
      <QuestionCards report={r} />
      <DataBehind data={data} />
    </>
  );
}
