import Badge from "./Badge";
import Panel from "./Panel";
import { activationView, minutesShort, pctText } from "@/lib/analytics/usageView";

// The admin Analytics page's activation card (#620, the cohort topic): new
// people who opened the Overview and then a worst-case view, a trade's reason
// or a tip or the assistant within a day of their first session, over the
// report's last 12 weeks. Counts and shares only; a week with no event is
// listed apart, never shown as 0; no alert is raised on it.

export const ACTIVATION_TITLE = "Q2, Q4 · Activation in the first day";

function Latest({ v }) {
  const w = v.latest;
  if (!w) return <p className="text-sm text-muted">No events in {v.latestWeek}: nothing to count this week.</p>;
  return (
    <>
      <div className="grid grid-cols-3 gap-3">
        <div>
          <div className="text-xs text-muted">Activated this week</div>
          <div className="text-lg font-semibold tabular-nums">{`${w.activated} of ${w.users}`}</div>
        </div>
        <div>
          <div className="text-xs text-muted">Share</div>
          <div className="text-lg font-semibold tabular-nums">{w.tooFew ? "—" : pctText(w.share)}</div>
        </div>
        <div>
          <div className="text-xs text-muted">Median time to the first answer</div>
          <div className="text-lg font-semibold tabular-nums">{w.tooFew ? "—" : minutesShort(w.median)}</div>
        </div>
      </div>
      {w.tooFew ? (
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <Badge tone="muted">too few to judge</Badge>
          {w.users} new {w.users === 1 ? "person" : "people"} this week, under {v.minUsers}: no share is given and no alert is raised.
        </p>
      ) : null}
    </>
  );
}

export default function ActivationCard({ report }) {
  const v = activationView(report);
  if (!v) {
    return (
      <Panel title={ACTIVATION_TITLE}>
        <p className="text-sm text-muted">Not in this report (written before #620).</p>
      </Panel>
    );
  }
  return (
    <Panel title={ACTIVATION_TITLE}>
      <div className="space-y-3">
        <p className="text-sm text-muted">
          New people who, within {v.windowHours} h of their first session, opened the Overview and then a worst-case view, a trade&apos;s reason, or a tip or
          the assistant.
        </p>
        <Latest v={v} />
        {v.rows.length ? (
          <div className="max-w-full overflow-x-auto">
            <table className="w-full text-sm">
              <caption className="text-left text-xs uppercase tracking-wide text-muted">Last 12 weeks, newest first</caption>
              <thead>
                <tr className="text-xs text-muted">
                  <th scope="col" className="text-left font-normal">
                    First week
                  </th>
                  <th scope="col" className="text-right font-normal">
                    New people
                  </th>
                  <th scope="col" className="text-right font-normal">
                    Opened the Overview
                  </th>
                  <th scope="col" className="text-right font-normal">
                    Activated
                  </th>
                  <th scope="col" className="text-right font-normal">
                    Share
                  </th>
                  <th scope="col" className="text-right font-normal">
                    Median
                  </th>
                </tr>
              </thead>
              <tbody>
                {v.rows.map((w) => (
                  <tr key={w.week} className="border-t border-line/60">
                    <th scope="row" className="whitespace-nowrap py-0.5 pr-2 text-left font-normal">
                      {w.week}
                    </th>
                    <td className="text-right tabular-nums">{w.users}</td>
                    <td className="text-right tabular-nums">{w.overview}</td>
                    <td className="text-right tabular-nums">{w.activated}</td>
                    <td className="whitespace-nowrap pl-2 text-right tabular-nums">{w.tooFew ? "too few" : pctText(w.share)}</td>
                    <td className="whitespace-nowrap pl-2 text-right tabular-nums">{w.tooFew ? "—" : minutesShort(w.median)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {v.empty.length ? <p className="text-xs text-muted">Weeks with no events, not counted as zero: {v.empty.join(", ")}</p> : null}
      </div>
    </Panel>
  );
}
