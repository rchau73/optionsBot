import clsx from "clsx";
import { ENV_LABELS, deliveryTiles, headline } from "@/lib/analytics/deliveryView";

/** A band chip's tone: the word is always shown, colour only adds to it. */
const CHIP_TONE = {
  ELITE: "bg-profit/15 text-profit",
  HIGH: "bg-profit/15 text-profit",
  MEDIUM: "bg-warn/15 text-warn",
  LOW: "bg-loss/15 text-loss",
  "NO FAILURE": "bg-profit/15 text-profit",
  "NOT YET": "bg-slate-800 text-muted",
};

/** The headline line and the four DORA tiles of one environment, this week (#473). */
export default function DeliveryTiles({ report, env }) {
  const tiles = deliveryTiles(report, env);
  return (
    <section aria-labelledby="dora-tiles" className="space-y-3">
      <p className="text-base font-semibold">{headline(report)}</p>
      <h2 id="dora-tiles" className="text-sm text-muted">
        {ENV_LABELS[env]} · {tiles ? `week ${tiles.week} (so far)` : "no week in the report"}
      </h2>
      {tiles ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <DoraTile label="Deployment frequency" tile={tiles.frequency} />
          <DoraTile label="Lead time" tile={tiles.lead} />
          <DoraTile label="Change failure rate" tile={tiles.failure} />
          <DoraTile label="Time to restore" tile={tiles.restore} />
        </div>
      ) : null}
    </section>
  );
}

/** One tile: value, band chip, context, four weeks of bars and what it counts. */
export function DoraTile({ label, tile }) {
  return (
    <section aria-label={label} className="flex flex-col gap-2 rounded border border-line bg-panel p-3">
      <div className="flex items-start justify-between gap-2">
        <h3 className="text-sm text-muted">{label}</h3>
        <span className={clsx("rounded px-1.5 py-0.5 text-xs font-semibold", CHIP_TONE[tile.chip] ?? CHIP_TONE["NOT YET"])}>{tile.chip}</span>
      </div>
      <p className="text-2xl font-bold tabular-nums">{tile.value}</p>
      <p className="text-sm">{tile.context}</p>
      <WeekBars label={label} bars={tile.bars} />
      <p className="text-xs text-muted">What this counts: {tile.counts}</p>
    </section>
  );
}

/** Four weeks of bars, scaled to the largest; a week not recorded is dimmed and says n/r. */
function WeekBars({ label, bars }) {
  const max = Math.max(0, ...bars.map((b) => b.value ?? 0));
  const summary = `${label}, last ${bars.length} weeks: ${bars.map((b) => `${b.week} ${b.text}`).join(", ")}`;
  return (
    <div role="img" aria-label={summary} className="grid grid-cols-4 items-end gap-1">
      {bars.map((b) => (
        <div key={b.week} data-week={b.week} className={clsx("flex flex-col items-center gap-0.5", !b.recorded && "opacity-40")} aria-hidden="true">
          <div className="flex h-10 w-full items-end">
            <div
              className={clsx("w-full rounded-t", b.recorded ? "bg-testnet/60" : "border border-dashed border-muted")}
              style={{ height: b.recorded && max > 0 && b.value ? `${Math.max(6, (b.value / max) * 100)}%` : b.recorded ? "2px" : "100%" }}
            />
          </div>
          <span className="text-[11px] tabular-nums">{b.text}</span>
          <span className="text-[10px] text-muted">{b.week.slice(5)}</span>
        </div>
      ))}
    </div>
  );
}
