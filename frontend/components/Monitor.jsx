"use client";

import Link from "next/link";
import { useState } from "react";
import Header from "./Header";
import PhoneOverview from "./PhoneOverview";
import BotDashboards from "./BotDashboard";
import OverviewPnl from "./OverviewPnl";
import OverviewStatus from "./OverviewStatus";
import OverviewTiles from "./OverviewTiles";
import PayoffModal from "./PayoffModal";
import { useMonitor } from "@/hooks/useMonitor";
import { summarise } from "@/lib/monitor";
import { DISPLAY_TZ_LABEL } from "@/lib/time";

/**
 * The overview, answer first (#404): the status line and the amber "held back" lines, the four tiles (coin
 * first, ≈ USD on the footer), the P&L chart (Coin per bot | USD all bots), then one band per bot — price chart
 * with the sold-strike corridor and its strangle rows, a row opening the payoff modal. The detail panels —
 * positions, missing legs, trades, working orders, not managed, the USD P&L over every range — are their own
 * page, Details (pro) at /details (#405); Activity and Account too (/activity, /account).
 * On a phone (< 640 px) the overview shows one bot (`bot`, ?bot=) with a coin switch (#320); its menu links to
 * Details (pro).
 */
export default function Monitor({ bot = null }) {
  const { names, bots, error, pollMs } = useMonitor();
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
          <OverviewStatus bots={bots} />
          <OverviewTiles bots={bots} loading={!bots.length && !error} />
          <OverviewPnl names={names} units={units} />
          <BotDashboards bots={bots} onOpen={openChart} />
          <p>
            <Link
              href="/details"
              className="inline-flex min-h-11 items-center text-sm text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2"
            >
              Details (pro) →
            </Link>
          </p>
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
