import StatTile from "./StatTile";
import { formatUSD } from "@/lib/format";

const tone = (v) => (v > 0 ? "good" : v < 0 ? "bad" : undefined);

/** Headline numbers across all bots (counts are since each bot started). */
export default function KpiStrip({ kpi }) {
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-8">
      <StatTile label="Bots online" value={`${kpi.online}/${kpi.bots}`} tone={kpi.online < kpi.bots ? "bad" : undefined} />
      <StatTile label="Open legs" value={kpi.openLegs} />
      <StatTile label="Pending orders" value={kpi.pending} />
      <StatTile label="Orders sent" value={kpi.submitted} sub={`${kpi.filled} filled`} />
      <StatTile label="Closes" value={kpi.closed} sub={`${kpi.skipped} skipped entries`} />
      <StatTile label="Realised P&L" value={formatUSD(kpi.realisedUsd)} tone={tone(kpi.realisedUsd)} />
      <StatTile label="Open P&L" value={formatUSD(kpi.unrealisedUsd)} tone={tone(kpi.unrealisedUsd)} />
      <StatTile label="Total P&L" value={formatUSD(kpi.totalUsd)} tone={tone(kpi.totalUsd)} sub="since start, USD at spot" />
    </div>
  );
}
