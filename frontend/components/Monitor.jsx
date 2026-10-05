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
import AccountPanel, { AccountSummary } from "./AccountPanel";
import OtherPositions, { otherPositionsSummary } from "./OtherPositions";
import { useMonitor } from "@/hooks/useMonitor";
import { filterRows, groupRows, legRows, summarise } from "@/lib/monitor";
import { pickAccount, riskRows, unmanagedPositions } from "@/lib/account";
import { DISPLAY_TZ_LABEL } from "@/lib/time";

const NO_FILTERS = { bot: "", strategy: "", type: "", moneyness: "", groupBy: "strategy" };

/** The single monitor page: header, KPIs, positions, working orders, feed, P&L. */
export default function Monitor() {
  const { names, bots, feed, history, error, pollMs, polledAt } = useMonitor();
  const [filters, setFilters] = useState(NO_FILTERS);

  const rows = useMemo(() => legRows(bots), [bots]);
  const groups = useMemo(() => groupRows(filterRows(rows, filters), filters.groupBy), [rows, filters]);
  const options = useMemo(
    () => ({ bots: bots.map((b) => b.name), strategies: [...new Set(rows.map((r) => r.strategyId))] }),
    [bots, rows],
  );
  const kpi = summarise(bots);
  const account = pickAccount(bots, polledAt);
  const risk = riskRows(bots);
  const others = unmanagedPositions(account, bots);

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
        <Panel
          title="Account & collateral (shared by all bots)"
          collapsible
          storageKey="monitor.panel.account"
          summary={<AccountSummary account={account} risk={risk} />}
        >
          <AccountPanel account={account} risk={risk} />
        </Panel>
        <Panel title="Open positions" right={<Filters filters={filters} options={options} onChange={setFilters} />}>
          <PositionsTable groups={groups} />
        </Panel>
        <Panel
          title="Not managed by the bot (still uses margin)"
          collapsible
          storageKey="monitor.panel.others"
          summary={otherPositionsSummary(others)}
        >
          <OtherPositions data={others} />
        </Panel>
        <div className="grid gap-3 lg:grid-cols-3">
          <Panel title="Activity" className="lg:col-span-2">
            <ActivityFeed feed={feed} />
          </Panel>
          <div className="space-y-3">
            <Panel title="P&L (USD, all bots)">
              <PnlChart live={history} names={names} />
            </Panel>
            <Panel title="Working orders (placed, not filled yet)">
              <p className="mb-2 text-xs text-muted">
                Limit orders resting on Deribit. Filled orders move to Open positions; unfilled ones are cancelled after the
                fill timeout and retried on a later cycle.
              </p>
              <PendingOrders bots={bots} />
            </Panel>
          </div>
        </div>
        <p className="text-center text-xs text-muted">
          Read-only · refreshes every {pollMs / 1000}s · times in {DISPLAY_TZ_LABEL} (UTC−3) · counts are since each bot started · educational study, not investment advice
        </p>
      </main>
    </div>
  );
}
