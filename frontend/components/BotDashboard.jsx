"use client";

import Badge from "./Badge";
import DashboardTiles from "./DashboardTiles";
import FirstDayPanel from "./FirstDayPanel";
import InfoTip from "./InfoTip";
import RegimeChart from "./RegimeChart";
import { BotStrangles } from "./StrangleCards";
import { formatUSD } from "@/lib/format";
import { coinOf, isFirstDay } from "@/lib/overview";
import { gexFreshness, regimeBadge, trendBadge } from "@/lib/regime";

// The overview as a dashboard (#394): one section per bot — a header line
// (coin, spot, regime, trend, GEX freshness), the regime chart, the strangle
// payoff cards (#391) and four stat tiles. Charts first, few words.

/** One dashboard section per bot. onOpen(bot, strangleId, element) opens a payoff card's modal. */
export default function BotDashboards({ bots, onOpen }) {
  if (!bots?.length) return null;
  return (
    <div className="space-y-4">
      {bots.map((bot) => (
        <BotDashboard key={bot.name} bot={bot} onOpen={onOpen} />
      ))}
    </div>
  );
}

export function BotDashboard({ bot, onOpen }) {
  const coin = coinOf(bot);
  return (
    <section aria-label={`${coin} dashboard`} className="space-y-3 rounded-lg border border-line bg-panel/40 p-3">
      <RegimeLine bot={bot} coin={coin} />
      <RegimeChart status={bot.status} positions={bot.positions} coin={coin} />
      <BotStrangles bot={bot} onOpen={onOpen} />
      {isFirstDay(bot) ? <FirstDayPanel bot={bot} /> : <DashboardTiles bot={bot} />}
    </section>
  );
}

/** "BTC $100,000 · POSITIVE confirmed · bull ⓘ · GEX 40s ago". */
export function RegimeLine({ bot, coin }) {
  const s = bot.status;
  const r = regimeBadge(s);
  const t = trendBadge(s);
  const g = gexFreshness(s);
  return (
    <header className="flex flex-wrap items-center gap-2" data-testid={`regime-line-${bot.name}`}>
      <h2 className="text-lg font-bold">{coin}</h2>
      <span className="text-lg tabular-nums">{s?.market?.spot ? formatUSD(s.market.spot) : "spot unknown"}</span>
      {r.word ? (
        <Badge tone={r.word === "NEGATIVE" ? "warn" : "good"} title={r.confirmed && r.confirmed !== r.word ? `confirmed regime: ${r.confirmed}` : undefined}>
          {r.word} · {r.basis}
        </Badge>
      ) : (
        <Badge tone="muted">regime {r.unknown}</Badge>
      )}
      {r.shed.map((side) => (
        <Badge key={side} tone="bad">
          {side}s shed
        </Badge>
      ))}
      <span className="inline-flex items-center gap-1">
        <Badge tone={t.trend === "bull" ? "good" : t.trend === "bear" ? "bad" : "neutral"}>{t.trend ? `trend ${t.trend}` : "trend unknown"}</Badge>
        <InfoTip label={`Why the ${coin} trend is ${t.trend ?? "unknown"}`}>{t.reason}</InfoTip>
      </span>
      <Badge tone={g.stale ? "warn" : "muted"}>{g.text}</Badge>
    </header>
  );
}
