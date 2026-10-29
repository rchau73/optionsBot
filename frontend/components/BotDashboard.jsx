"use client";

import clsx from "clsx";
import Badge from "./Badge";
import BandChart from "./BandChart";
import FirstDayPanel from "./FirstDayPanel";
import InfoTip from "./InfoTip";
import LastTrade from "./LastTrade";
import MarginBar from "./MarginBar";
import StrangleRow from "./StrangleRow";
import { ageSeconds, formatAge, formatNumber, formatUSD, isNumber } from "@/lib/format";
import { coinOf, isFirstDay } from "@/lib/overview";
import { payoffStrangles } from "@/lib/payoff";
import { gexFreshness, regimeBadge, trendBadge } from "@/lib/regime";
import { marginLines } from "@/lib/tiles";

// One band per bot on the overview (#404): a header (coin, spot, regime and
// trend chips, GEX age · DVOL, the margin bar), the price chart with the
// sold-strike corridor on the left (about 60 %) and one row-button per
// strangle on the right; stacked on a phone, chart first; under them the last
// trade with its one-click Why? (#489). A stale bot is
// dimmed with its age; a bot that is down says so.

export const NOTHING_HELD = "Nothing held: the bot opens a strangle when a slot is vacant and margin allows";

/** One band per bot. onOpen(bot, strangleId, element) opens a strangle row's payoff modal. */
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
  const down = !bot.status && bot.error;
  return (
    <section aria-label={`${coin} dashboard`} className={clsx("space-y-3 rounded-lg border border-line bg-panel/40 p-3", bot.stale && "opacity-60")}>
      <RegimeLine bot={bot} coin={coin} />
      {down ? (
        <p role="alert" className="text-base text-loss">
          {coin} bot is down: {bot.error}. Nothing is shown until it answers again.
        </p>
      ) : isFirstDay(bot) ? (
        <div className="grid gap-3 lg:grid-cols-[3fr_2fr]">
          <BandChart status={bot.status} payoff={bot.payoff} coin={coin} />
          <FirstDayPanel bot={bot} />
        </div>
      ) : (
        <>
          <div className="grid gap-3 lg:grid-cols-[3fr_2fr]">
            <BandChart status={bot.status} payoff={bot.payoff} coin={coin} />
            <StrangleList bot={bot} coin={coin} onOpen={onOpen} />
          </div>
          <LastTrade bot={bot} coin={coin} />
        </>
      )}
    </section>
  );
}

/** The strangle rows of one bot, or why there are none. */
function StrangleList({ bot, coin, onOpen }) {
  const report = bot.payoff;
  const list = payoffStrangles(report);
  const failure = bot.payoffError ?? (report ? null : bot.error);
  return (
    <section aria-label={`${coin} · strangles`} className="space-y-2">
      <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">Strangles</h3>
      {failure && !report ? (
        <p role="alert" className="text-sm text-warn">
          Strangles unknown: {failure}.
        </p>
      ) : !report ? (
        <p className="text-sm text-muted">Loading the strangles…</p>
      ) : !list.length ? (
        <p className="text-sm text-muted">{NOTHING_HELD}.</p>
      ) : (
        <ul className="space-y-2">
          {list.map((sp) => (
            <li key={sp.strangle_id}>
              <StrangleRow sp={sp} coin={coin} onOpen={(id, el) => onOpen?.(bot.name, id, el)} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** "BTC $100,000 · NEGATIVE confirmed · trend bear ⓘ · GEX 40s ago · DVOL 45.2", the margin bar on the right. */
export function RegimeLine({ bot, coin }) {
  const s = bot.status;
  const r = regimeBadge(s);
  const t = trendBadge(s);
  const g = gexFreshness(s);
  const m = marginLines([bot])[0];
  const dvol = s?.market?.dvol;
  const age = ageSeconds(s?.as_of ?? undefined);
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
      <span className="text-xs text-muted tabular-nums">DVOL {isNumber(dvol) && dvol > 0 ? formatNumber(dvol, 1) : "unknown"}</span>
      {bot.stale ? <Badge tone="warn">stale · updated {formatAge(age)}</Badge> : null}
      <div className="ml-auto w-48" data-testid={`band-margin-${bot.name}`}>
        {m.unknown ? (
          <span className="text-xs text-muted">margin {m.unknown}</span>
        ) : (
          <MarginBar label={`margin ${m.text}`} pct={m.imPct} markers={[{ pct: m.limitPct, label: `IM limit ${m.limitPct}%` }]} />
        )}
      </div>
    </header>
  );
}
