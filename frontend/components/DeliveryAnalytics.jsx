"use client";

import Link from "next/link";
import clsx from "clsx";
import { useEffect, useState } from "react";
import DeliveryTable from "./DeliveryTable";
import DeliveryTiles from "./DeliveryTiles";
import ReleaseTrain from "./ReleaseTrain";
import { fetchDeliveryReport } from "@/lib/api";
import { DELIVERY_ENVS, ENV_LABELS, deliveryHref, weeklySummary } from "@/lib/analytics/deliveryView";
import { formatDateTime } from "@/lib/format";

const TARGET =
  "inline-flex min-h-11 min-w-11 items-center justify-center rounded border border-line px-3 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** What the topic shows once cmd/dora has run: the empty state's list. */
export const WILL_APPEAR = [
  "Four numbers per environment: how often it deploys, how long a merged change takes to go live, how many deploys fail, how long a failure lasts.",
  "A release train of the last 8 days: each mvp deploy and each stable release.",
  "What went live: every merged PR with its merge time and when it went live on mvp and on stable.",
];

/**
 * The Analytics page's Delivery (DORA) topic (#473): the delivery report
 * cmd/dora writes (#470) for one environment — stable (production) by
 * default, mvp (test) — four DORA tiles, the release train and every PR that
 * went live. env comes from the URL; the switch is links.
 */
export default function DeliveryAnalytics({ env }) {
  const [state, setState] = useState({ data: null, error: null });

  useEffect(() => {
    const ctrl = new AbortController();
    fetchDeliveryReport(ctrl.signal).then(
      (data) => setState({ data, error: null }),
      (err) => {
        if (!ctrl.signal.aborted) setState({ data: null, error: err.message });
      },
    );
    return () => ctrl.abort();
  }, []);

  return <DeliveryView env={env} data={state.data} error={state.error} />;
}

/** The topic for a route answer `data` (null while loading) or an `error`. */
export function DeliveryView({ env, data, error = null }) {
  const [openPr, setOpenPr] = useState(null);
  const report = data?.report ?? null;
  const open = openPr === null ? null : (report?.prs ?? []).find((p) => p.number === openPr) ?? null;
  return (
    <div className="space-y-4">
      <Toolbar env={env} report={report} />
      {error ? (
        <Notice role="alert">The delivery report could not be loaded: {error}</Notice>
      ) : !data ? (
        <Notice>Loading the delivery report…</Notice>
      ) : !report ? (
        <NotRunning />
      ) : (
        <>
          <DeliveryTiles report={report} env={env} />
          <ReleaseTrain report={report} env={env} pr={open} />
          <DeliveryTable report={report} openPr={openPr} onOpen={(n) => setOpenPr((cur) => (cur === n ? null : n))} />
          <p className="text-xs text-muted">
            Times in UTC. From {data.file}, written {formatDateTime(data.written_at)} by cmd/dora (dora.sh, daily); nothing is recomputed here.
          </p>
        </>
      )}
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

function NotRunning() {
  return (
    <section aria-labelledby="dora-off" className="rounded border border-line bg-panel p-3 text-sm">
      <h2 id="dora-off" className="mb-2 font-semibold">
        No delivery report yet
      </h2>
      <p className="mb-2">
        The delivery collector has not written its report (dora.json) yet. It runs by itself once a day on the agents&apos; machine (dora.sh, #470): nothing to
        set up. Once it has run, this topic shows:
      </p>
      <ul className="list-disc space-y-1 pl-5">
        {WILL_APPEAR.map((s) => (
          <li key={s}>{s}</li>
        ))}
      </ul>
    </section>
  );
}

/** The environment switch, Copy link and Weekly summary. */
function Toolbar({ env, report }) {
  const [copied, setCopied] = useState(null);
  const [showSummary, setShowSummary] = useState(false);
  const summary = weeklySummary(report);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(`${window.location.origin}${deliveryHref(env)}`);
      setCopied("Link copied.");
    } catch {
      setCopied("Could not copy: use the address bar.");
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-3">
        <nav aria-label="Environment" className="flex flex-wrap gap-1">
          {DELIVERY_ENVS.map((e) => (
            <Link
              key={e}
              href={deliveryHref(e)}
              aria-current={e === env ? "page" : undefined}
              className={clsx(TARGET, e === env ? "bg-slate-700 font-semibold" : "hover:bg-line/40")}
            >
              {ENV_LABELS[e]}
            </Link>
          ))}
        </nav>
        <button type="button" onClick={copy} className={clsx(TARGET, "hover:bg-line/40")}>
          Copy link
        </button>
        {report ? (
          <button
            type="button"
            aria-expanded={showSummary}
            aria-controls="dora-summary"
            onClick={() => setShowSummary((v) => !v)}
            className={clsx(TARGET, "hover:bg-line/40")}
          >
            Weekly summary
          </button>
        ) : null}
        <span role="status" className="text-sm text-muted">
          {copied}
        </span>
      </div>
      {showSummary ? (
        <section id="dora-summary" aria-label="Weekly summary" className="rounded border border-line bg-panel p-3 text-sm">
          {summary ? (
            <>
              <p className="mb-2 text-muted">What dora.sh posts on the E6 epic on Monday, week {summary.week}:</p>
              <pre className="overflow-x-auto whitespace-pre-wrap font-mono text-xs">{summary.text}</pre>
            </>
          ) : (
            <p>No week has ended since recording began: the first summary is posted on the Monday after one.</p>
          )}
        </section>
      ) : null}
    </div>
  );
}
