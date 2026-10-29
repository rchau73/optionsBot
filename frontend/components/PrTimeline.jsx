import clsx from "clsx";
import { activityHref, activityWindow, durationText, prHref, reportNow, timeParts, utcText } from "@/lib/analytics/deliveryView";

/** Each part's colour; the legend names every part with its time, so colour is never the only signal. */
const PART_TONE = { review: "bg-slate-500", mvp: "bg-testnet", stable: "bg-profit" };

/**
 * Where a PR's time went (#473): first commit → merge → mvp → stable as one
 * bar with a legend, the failures it caused, and its links — the Activity
 * page around its mvp deploy and the PR. The P&L chart with the PR marked
 * comes later (#475).
 */
export default function PrTimeline({ pr, report }) {
  const parts = timeParts(pr, reportNow(report));
  const total = parts.reduce((s, p) => s + p.hours, 0);
  const win = activityWindow(pr);
  const failures = (report?.failures ?? []).filter((f) => (pr.failures ?? []).includes(f.id));
  const label = `#${pr.number}, where the time went: ${parts.map((p) => `${p.label} ${p.text}`).join(", ") || "no times recorded"}`;
  return (
    <div className="space-y-2 rounded border border-line p-3">
      {parts.length ? (
        <div role="img" aria-label={label} className="flex h-3 w-full overflow-hidden rounded bg-line">
          {parts.map((p) => (
            <span
              key={p.key}
              className={clsx(PART_TONE[p.key], p.waiting && "opacity-50")}
              style={{ width: `${total > 0 ? Math.max(2, (p.hours / total) * 100) : 100 / parts.length}%` }}
            />
          ))}
        </div>
      ) : (
        <p className="text-sm text-muted">No live time recorded for #{pr.number} yet.</p>
      )}
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {parts.map((p) => (
          <li key={p.key} className="flex items-center gap-1">
            <span aria-hidden="true" className={clsx("inline-block h-2 w-3 rounded", PART_TONE[p.key], p.waiting && "opacity-50")} />
            {p.label}: <span className="tabular-nums">{p.text}</span>
          </li>
        ))}
      </ul>
      {failures.length ? (
        <ul className="text-sm">
          {failures.map((f) => (
            <li key={f.id}>
              Failure on {f.env} at {utcText(f.at)}: {f.detail};{" "}
              {f.restored_at ? `restored ${utcText(f.restored_at)} (time to restore ${durationText(f.restore_hours)})` : "not restored yet"}.
            </li>
          ))}
        </ul>
      ) : null}
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {win ? (
          <li>
            <a href={activityHref(win)} className="inline-flex min-h-11 items-center underline">
              Activity page: look from {utcText(win.from)} to {utcText(win.to)} UTC
            </a>
          </li>
        ) : null}
        <li>
          <a href={prHref(pr.number)} target="_blank" rel="noreferrer" className="inline-flex min-h-11 items-center underline">
            PR #{pr.number} on GitHub
          </a>
        </li>
        <li className="inline-flex min-h-11 items-center text-muted">The P&amp;L chart with this PR marked: later (#475).</li>
      </ul>
    </div>
  );
}
