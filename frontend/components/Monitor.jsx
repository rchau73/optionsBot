"use client";

import clsx from "clsx";
import { useEffect, useMemo, useState } from "react";
import Header from "./Header";
import PhoneOverview from "./PhoneOverview";
import InvestorSummary from "./InvestorSummary";
import KpiStrip from "./KpiStrip";
import Panel from "./Panel";
import Filters from "./Filters";
import PositionsTable from "./PositionsTable";
import PendingOrders from "./PendingOrders";
import HeldLegs from "./HeldLegs";
import PayoffModal from "./PayoffModal";
import StrangleCards from "./StrangleCards";
import ActivityFeed from "./ActivityFeed";
import PnlChart from "./PnlChart";
import CoinPnlChart from "./CoinPnlChart";
import AccountPanel, { AccountSummary } from "./AccountPanel";
import OtherPositions, { otherPositionsSummary } from "./OtherPositions";
import TradeHistory, { tradeHistorySummary } from "./TradeHistory";
import { useMonitor } from "@/hooks/useMonitor";
import { heldRows } from "@/lib/held";
import { consolidateRows, filterRows, groupRows, legRows, summarise } from "@/lib/monitor";
import { pickAccount, riskRows, unmanagedPositions } from "@/lib/account";
import { DISPLAY_TZ_LABEL } from "@/lib/time";
import { roleAtLeast } from "@/lib/auth/roles";
import { activitySummary, ordersSummary, positionsSummary } from "@/lib/overview";

const NO_FILTERS = { bot: "", strategy: "", type: "", moneyness: "", groupBy: "strategy" };

/**
 * The single monitor page: header, the investor summary first, then KPIs, positions, working orders, feed, P&L.
 * role (the signed-in person's) only picks display defaults: technical details open for managers and admins,
 * and for a user the detail panels start folded (#288; a choice saved in this browser wins).
 * On a phone (< 640 px) the overview shows one bot (`bot`, ?bot=) with a coin switch (#320); its menu's
 * Details shows the full monitor below.
 * details (?view=details, the menu bar's "Details (pro)", #385) shows the detail panels, opened, and scrolls to them.
 */
export default function Monitor({ role = null, bot = null, details = false }) {
  const { names, bots, feed, history, trades, error, pollMs, polledAt } = useMonitor();
  const detailsOpen = roleAtLeast(role, "manager");
  const panelsOpen = role !== "user";
  const fold = { defaultOpen: panelsOpen, forceOpen: details };
  const allTrades = useMemo(() => Object.values(trades ?? {}).flat(), [trades]);
  const [filters, setFilters] = useState(NO_FILTERS);
  // The payoff modal (#391): { bot, strangleId, opener } — opener gets the focus back on close.
  const [chart, setChart] = useState(null);
  const openChart = (botName, strangleId, opener) => setChart({ bot: botName, strangleId, opener });
  const chartBot = chart ? bots.find((b) => b.name === chart.bot) : null;
  const [phoneBot, setPhoneBot] = useState(bot);
  const [phoneDetails, setPhoneDetails] = useState(details);
  // A later "Details (pro)" on this page (client navigation) opens them too.
  const [seenDetails, setSeenDetails] = useState(details);
  if (details !== seenDetails) {
    setSeenDetails(details);
    if (details) setPhoneDetails(true);
  }
  useEffect(() => {
    if (details) requestAnimationFrame(() => document.getElementById("details")?.scrollIntoView?.());
  }, [details]);

  const selectPhoneBot = (name) => {
    setPhoneBot(name);
    const url = new URL(window.location.href);
    url.searchParams.set("bot", name);
    window.history.replaceState(null, "", url);
  };
  const showPhoneDetails = (show, anchor) => {
    setPhoneDetails(show);
    if (show && anchor) requestAnimationFrame(() => document.getElementById(anchor)?.scrollIntoView?.());
  };

  const rows = useMemo(() => consolidateRows(legRows(bots)), [bots]);
  const groups = useMemo(() => groupRows(filterRows(rows, filters), filters.groupBy), [rows, filters]);
  const options = useMemo(
    () => ({ bots: bots.map((b) => b.name), strategies: [...new Set(rows.map((r) => r.strategyId))] }),
    [bots, rows],
  );
  const kpi = summarise(bots);
  const account = pickAccount(bots, polledAt);
  const risk = riskRows(bots);
  const others = unmanagedPositions(account, bots);
  const statuses = Object.fromEntries(bots.map((b) => [b.name, b.status]));
  const units = Object.fromEntries(bots.map((b) => [b.name, b.status?.underlying ?? b.name.toUpperCase()]));

  return (
    <div className="min-h-screen">
      <div className="sm:hidden">
        <PhoneOverview bots={bots} selected={phoneBot} onSelect={selectPhoneBot} details={phoneDetails} onDetails={showPhoneDetails} />
      </div>
      <div className={clsx(!phoneDetails && "hidden", "sm:block")} data-testid="desktop-monitor">
        <Header bots={bots} error={error} role={role} />
        <main className="space-y-3 p-3">
          {kpi.halted ? (
            <div role="alert" className="rounded border border-loss bg-loss/10 px-3 py-2 text-loss">
              Kill switch fired — trading is halted on at least one bot.
            </div>
          ) : null}
          <InvestorSummary bots={bots} />
          <StrangleCards bots={bots} onOpen={openChart} />
          <div id="details" className="scroll-mt-3">
            <KpiStrip kpi={kpi} />
          </div>
          <Panel
            title="Account & collateral (shared by all bots)"
            collapsible
            {...fold}
            storageKey="monitor.panel.account"
            summary={<AccountSummary account={account} risk={risk} />}
          >
            <AccountPanel account={account} risk={risk} />
          </Panel>
          <Panel
            title="Open positions"
            collapsible
            {...fold}
            storageKey="monitor.panel.positions"
            summary={positionsSummary(bots)}
            right={<Filters filters={filters} options={options} onChange={setFilters} />}
          >
            <PositionsTable groups={groups} statuses={statuses} onOpenChart={openChart} />
          </Panel>
          <Panel
            title="Missing legs: why repair holds them"
            collapsible
            {...fold}
            storageKey="monitor.panel.held"
            summary={heldSummary(bots)}
          >
            <HeldLegs bots={bots} onOpenChart={openChart} />
          </Panel>
          <Panel
            title="Not managed by the bot (still uses margin)"
            collapsible
            {...fold}
            storageKey="monitor.panel.others"
            summary={otherPositionsSummary(others)}
          >
            <OtherPositions data={others} />
          </Panel>
          <Panel
            title="Trade history & profitability"
            collapsible
            {...fold}
            storageKey="monitor.panel.trades"
            summary={tradeHistorySummary(allTrades)}
          >
            <TradeHistory trades={allTrades} detailsOpen={detailsOpen} />
          </Panel>
          <div className="grid gap-3 lg:grid-cols-3">
            <Panel
              id="activity"
              title="Activity"
              className="lg:col-span-2"
              fill
              collapsible
              {...fold}
              storageKey="monitor.panel.activity"
              summary={activitySummary(feed)}
            >
              <ActivityFeed feed={feed} detailsOpen={detailsOpen} />
            </Panel>
            <div className="space-y-3">
              <Panel title="P&L (USD, all bots)" collapsible {...fold} storageKey="monitor.panel.pnl" summary="chart, all bots">
                <PnlChart live={history} names={names} />
              </Panel>
              <Panel title="P&L in coin (per bot)" collapsible {...fold} storageKey="monitor.panel.pnl_coin" summary="one chart per bot">
                <CoinPnlChart names={names} units={units} />
              </Panel>
              <Panel
                title="Working orders (placed, not filled yet)"
                collapsible
                {...fold}
                storageKey="monitor.panel.orders"
                summary={ordersSummary(bots)}
              >
                <p className="mb-2 text-xs text-muted">
                  Limit orders resting on Deribit. Filled orders move to Open positions; unfilled ones are cancelled after the
                  fill timeout and retried on a later cycle.
                </p>
                <PendingOrders bots={bots} />
              </Panel>
            </div>
          </div>
          {chart ? (
            <PayoffModal
              report={chartBot?.payoff ?? null}
              error={chartBot?.payoffError ?? chartBot?.error ?? null}
              strangleId={chart.strangleId}
              opener={chart.opener}
              onClose={() => setChart(null)}
            />
          ) : null}
          <p className="text-center text-xs text-muted">
            Read-only · refreshes every {pollMs / 1000}s · times in {DISPLAY_TZ_LABEL} (UTC−3) · history and counts restored from each bot&apos;s journal · educational study, not investment advice
          </p>
        </main>
      </div>
    </div>
  );
}

/** "2 held: btc 2" / "none" for the collapsed panel. */
function heldSummary(bots) {
  const rows = heldRows(bots);
  if (!rows.length) return "none";
  const per = {};
  for (const r of rows) per[r.bot] = (per[r.bot] ?? 0) + 1;
  return `${rows.length} held: ${Object.entries(per)
    .map(([b, n]) => `${b} ${n}`)
    .join(" · ")}`;
}
