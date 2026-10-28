"use client";

import { useState } from "react";
import Header from "./Header";
import PhoneOverview from "./PhoneOverview";
import BotDashboards from "./BotDashboard";
import Panel from "./Panel";
import PayoffModal from "./PayoffModal";
import PnlChart from "./PnlChart";
import CoinPnlChart from "./CoinPnlChart";
import { useMonitor } from "@/hooks/useMonitor";
import { summarise } from "@/lib/monitor";
import { DISPLAY_TZ_LABEL } from "@/lib/time";

/**
 * The overview as a dashboard (#394): the header's chips and warnings, then per bot its regime chart, strangle
 * payoff cards and four stat tiles, then the two P&L charts (one week by default). The detail panels — positions,
 * missing legs, trades, working orders, not managed — are their own page, Details (pro) at /details (#405);
 * Activity and Account too (/activity, /account).
 * On a phone (< 640 px) the overview shows one bot (`bot`, ?bot=) with a coin switch (#320); its menu links to
 * Details (pro).
 */
export default function Monitor({ bot = null }) {
  const { names, bots, history, error, pollMs } = useMonitor();
  // The payoff modal (#391): { bot, strangleId, opener } — opener gets the focus back on close.
  const [chart, setChart] = useState(null);
  const openChart = (botName, strangleId, opener) => setChart({ bot: botName, strangleId, opener });
  const chartBot = chart ? bots.find((b) => b.name === chart.bot) : null;
  const [phoneBot, setPhoneBot] = useState(bot);

  const selectPhoneBot = (name) => {
    setPhoneBot(name);
    const url = new URL(window.location.href);
    url.searchParams.set("bot", name);
    window.history.replaceState(null, "", url);
  };

  const kpi = summarise(bots);
  const units = Object.fromEntries(bots.map((b) => [b.name, b.status?.underlying ?? b.name.toUpperCase()]));

  return (
    <div className="min-h-screen">
      <div className="sm:hidden">
        <PhoneOverview bots={bots} selected={phoneBot} onSelect={selectPhoneBot} />
      </div>
      <div className="hidden sm:block" data-testid="desktop-monitor">
        <Header bots={bots} error={error} />
        <main className="space-y-3 p-3">
          {kpi.halted ? (
            <div role="alert" className="rounded border border-loss bg-loss/10 px-3 py-2 text-loss">
              Kill switch fired — trading is halted on at least one bot.
            </div>
          ) : null}
          <BotDashboards bots={bots} onOpen={openChart} />
          <div className="grid gap-3 xl:grid-cols-2">
            <Panel title="P&L (USD, all bots)" collapsible storageKey="monitor.panel.pnl" summary="chart, all bots">
              <PnlChart live={history} names={names} />
            </Panel>
            <Panel title="P&L in coin (per bot)" collapsible storageKey="monitor.panel.pnl_coin" summary="one chart per bot">
              <CoinPnlChart names={names} units={units} />
            </Panel>
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
