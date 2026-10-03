"use client";

import { useMemo, useState } from "react";
import Header from "./Header";
import KpiStrip from "./KpiStrip";
import Panel from "./Panel";
import Filters from "./Filters";
import PositionsTable from "./PositionsTable";
import PendingOrders from "./PendingOrders";
import ActivityFeed from "./ActivityFeed";
import PnlChart from "./PnlChart";
import { useMonitor } from "@/hooks/useMonitor";
import { filterRows, groupRows, legRows, summarise } from "@/lib/monitor";

const POLL_MS = 1000;
const NO_FILTERS = { bot: "", strategy: "", type: "", moneyness: "", groupBy: "strategy" };

/** The single monitor page: header, KPIs, positions, working orders, feed, P&L. */
export default function Monitor() {
  const { bots, feed, history, error } = useMonitor(POLL_MS);
  const [filters, setFilters] = useState(NO_FILTERS);

  const rows = useMemo(() => legRows(bots), [bots]);
  const groups = useMemo(() => groupRows(filterRows(rows, filters), filters.groupBy), [rows, filters]);
  const options = useMemo(
    () => ({ bots: bots.map((b) => b.name), strategies: [...new Set(rows.map((r) => r.strategyId))] }),
    [bots, rows],
  );
  const kpi = summarise(bots);

  return (
    <div className="min-h-screen">
      <Header bots={bots} error={error} />
      <main className="space-y-3 p-3">
        {kpi.halted ? (
          <div role="alert" className="rounded border border-loss bg-loss/10 px-3 py-2 text-loss">
            Kill switch fired — trading is halted on at least one bot.
          </div>
        ) : null}
        <KpiStrip kpi={kpi} />
        <Panel title="Open positions" right={<Filters filters={filters} options={options} onChange={setFilters} />}>
          <PositionsTable groups={groups} />
        </Panel>
        <div className="grid gap-3 lg:grid-cols-3">
          <Panel title="Activity" className="lg:col-span-2">
            <ActivityFeed feed={feed} />
          </Panel>
          <div className="space-y-3">
            <Panel title="P&L this session (USD)">
              <PnlChart history={history} />
            </Panel>
            <Panel title="Working orders">
              <PendingOrders bots={bots} />
            </Panel>
          </div>
        </div>
        <p className="text-center text-xs text-muted">
          Read-only · refreshes every second · counts are since each bot started · educational study, not investment advice
        </p>
      </main>
    </div>
  );
}
