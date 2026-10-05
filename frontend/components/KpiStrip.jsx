import StatTile from "./StatTile";
import { formatDate, formatUSD } from "@/lib/format";

const tone = (v) => (v > 0 ? "good" : v < 0 ? "bad" : undefined);

/** Headline numbers across all bots, since the journals began (restored on restart). */
export default function KpiStrip({ kpi }) {
  const since = kpi.since ? `since ${formatDate(kpi.since)}` : "since start";
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-8">
      <StatTile label="Bots online" value={`${kpi.online}/${kpi.bots}`} tone={kpi.online < kpi.bots ? "bad" : undefined} />
      <StatTile label="Open legs" value={kpi.openLegs} />
      <StatTile label="Pending orders" value={kpi.pending} />
      <StatTile label="Orders sent" value={kpi.submitted} sub={`${kpi.filled} filled · ${since}`} />
      <StatTile label="Closes" value={kpi.closed} sub={`${kpi.skipped} skipped entries · ${since}`} />
      <StatTile label="Realized P&L" value={formatUSD(kpi.realisedUsd)} tone={tone(kpi.realisedUsd)} sub={`closed legs · ${since}`} />
      <StatTile label="Unrealized P&L" value={formatUSD(kpi.unrealisedUsd)} tone={tone(kpi.unrealisedUsd)} sub="open legs at live mark" />
      <StatTile label="Total P&L" value={formatUSD(kpi.totalUsd)} tone={tone(kpi.totalUsd)} sub={`${since}, USD at spot`} />
    </div>
  );
}
