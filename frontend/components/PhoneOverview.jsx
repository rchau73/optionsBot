"use client";

import clsx from "clsx";
import Link from "next/link";
import { useId, useState } from "react";
import Badge from "./Badge";
import FirstDayPanel from "./FirstDayPanel";
import { Signed } from "./InvestorSummary";
import MonthBars from "./MonthBars";
import RegimeChart from "./RegimeChart";
import { formatCoin, formatDate, formatUSD, isNumber } from "@/lib/format";
import { clockHM, coinOf, isFirstDay, monthSoFar, moveSince, rangeBar, stateWords, warningChips, worstCaseLine } from "@/lib/overview";
import { formatSignedPct, formatSignedUSD } from "@/lib/summary";

// The overview on a phone (#320, prototype screen 5, < 640 px): one bot at a
// time with a coin switch, the month's result as the headline and four
// stacked cards. Shown under the sm breakpoint only; the desktop keeps every
// bot. Every number comes from /api/summary and /api/status.

const LINK = "inline-flex min-h-11 items-center text-base text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2";

/**
 * bots: every bot; selected/onSelect: the bot shown (by name); details and
 * onDetails: whether the full monitor is shown below (the menu's Details).
 */
export default function PhoneOverview({ bots, selected, onSelect, details = false, onDetails }) {
  const bot = bots.find((b) => b.name === selected) ?? bots[0];
  if (!bot) return null;
  const s = bot.summary;
  const failure = bot.summaryError ?? bot.error;
  return (
    <div className="space-y-3 pb-3" data-testid="phone-overview">
      <PhoneHeader bots={bots} bot={bot} onSelect={onSelect} details={details} onDetails={onDetails} />
      <div className="space-y-3 px-3">
        {isFirstDay(bot) ? (
          <FirstDayPanel bot={bot} headingLevel={1} />
        ) : failure ? (
          <p role="alert" className="rounded-lg border border-line bg-panel px-4 py-3 text-base text-warn">
            Summary unavailable: {failure}. Nothing is shown until the bot answers again.
          </p>
        ) : !s ? (
          <p className="text-base text-muted">Loading the summary…</p>
        ) : (
          <>
            <Headline bot={bot} />
            <Card title={`${coinOf(bot)} market regime`}>
              <RegimeChart status={bot.status} positions={bot.positions} coin={coinOf(bot)} />
            </Card>
            <HoldsCard summary={s} />
            <MakingCard summary={s} bot={bot.name} />
            <WorstCard summary={s} bot={bot.name} />
            <LastActionCard summary={s} bot={bot.name} />
          </>
        )}
        <p className="text-center text-xs text-muted">From the bot&apos;s journal, after fees. Worst case is an estimate. Not investment advice.</p>
      </div>
    </div>
  );
}

function PhoneHeader({ bots, bot, onSelect, details, onDetails }) {
  const [menu, setMenu] = useState(false);
  const menuId = useId();
  const live = bot.status?.environment === "live";
  return (
    <header className="border-b border-line bg-panel px-3 py-2">
      <div className="flex items-center gap-2">
        <span className="text-base font-bold">optionsBot</span>
        {bot.status ? <Badge tone={live ? "live" : "testnet"}>{live ? "LIVE" : "TESTNET"}</Badge> : null}
        <div role="group" aria-label="Coin" className="ml-auto flex overflow-hidden rounded-md border border-line">
          {bots.map((b) => (
            <button
              key={b.name}
              type="button"
              aria-pressed={b.name === bot.name}
              onClick={() => onSelect?.(b.name)}
              className={clsx(
                "min-h-11 min-w-11 px-3 text-sm font-semibold focus-visible:outline focus-visible:outline-2",
                b.name === bot.name ? "bg-testnet/20 text-testnet" : "text-muted",
              )}
            >
              {coinOf(b)}
            </button>
          ))}
        </div>
        <button
          type="button"
          aria-expanded={menu}
          aria-controls={menuId}
          onClick={() => setMenu(!menu)}
          className="min-h-11 min-w-11 rounded-md border border-line text-lg focus-visible:outline focus-visible:outline-2"
        >
          <span aria-hidden="true">☰</span>
          <span className="sr-only">Menu</span>
        </button>
      </div>
      {warningChips(bot).length ? (
        <div className="mt-2 flex flex-wrap gap-2" data-testid="phone-warnings">
          {warningChips(bot).map((w) => (
            <Badge key={w} tone="bad">
              {w}
            </Badge>
          ))}
        </div>
      ) : null}
      {menu ? (
        <nav id={menuId} aria-label="Views" className="mt-2 flex flex-col border-t border-line pt-1">
          <Link href={`/income?bot=${encodeURIComponent(bot.name)}`} className={LINK}>
            Income, month by month
          </Link>
          <Link href={`/worst-case?bot=${encodeURIComponent(bot.name)}`} className={LINK}>
            Worst case right now
          </Link>
          <button type="button" aria-pressed={details} onClick={() => onDetails?.(!details)} className={clsx(LINK, "text-left")}>
            {details ? "Hide details" : "Details"}
          </button>
        </nav>
      ) : null}
    </header>
  );
}

/** "This month: +0.012 BTC after fees", then "About +$1,200 · 1–19 Oct · running normally". */
function Headline({ bot }) {
  const s = bot.summary;
  const coin = coinOf(bot);
  const m = s.income?.this_month;
  const closes = m?.closes ?? 0;
  const range = monthSoFar(s.as_of);
  return (
    <div className="space-y-1">
      <h1 className="text-xl font-semibold">
        This month:{" "}
        {closes > 0 ? (
          <>
            <Signed value={m.realised_coin}>{formatCoin(m.realised_coin, coin)}</Signed> after fees
          </>
        ) : (
          <span className="text-muted">not yet: no close this month</span>
        )}
      </h1>
      <p className="text-sm text-muted">
        {[closes > 0 ? `About ${formatSignedUSD(m.realised_usd)}` : null, range, stateWords(bot)].filter(Boolean).join(" · ")}
      </p>
    </div>
  );
}

function Card({ title, children, loss = false }) {
  return (
    <section aria-label={title} className={clsx("space-y-2 rounded-lg border bg-panel p-3", loss ? "border-loss/60" : "border-line")}>
      <h2 className="text-xs font-semibold uppercase tracking-wide text-muted">{title}</h2>
      {children}
    </section>
  );
}

function HoldsCard({ summary: s }) {
  const h = s.holding;
  const coin = s.underlying || h?.coin;
  if (!h?.legs) {
    return (
      <Card title="What it holds">
        <p className="text-base">Nothing open right now. New positions appear here once the bot sells them.</p>
      </Card>
    );
  }
  const bar = rangeBar(h.nearest, s.spot);
  const expiry = formatDate(h.nearest?.expiry ?? h.nearest_expiry);
  return (
    <Card title="What it holds">
      <p className="text-base">
        {h.legs} position{h.legs === 1 ? "" : "s"}.{" "}
        {bar && !bar.missing ? (
          <>
            Nearest pays if {coin} stays between <strong>{formatUSD(bar.put)}</strong> and <strong>{formatUSD(bar.call)}</strong> until{" "}
            <strong>{expiry}</strong>.
          </>
        ) : bar?.missing ? (
          <>
            Nearest holds only its {bar.missing === "put" ? "call" : "put"} at <strong>{formatUSD(bar.strike)}</strong> until <strong>{expiry}</strong>; its{" "}
            {bar.missing} side is missing.
          </>
        ) : (
          <>Nearest expiry {expiry}; its strikes are unknown: not in this bot&apos;s summary yet.</>
        )}
      </p>
      {bar && !bar.missing ? <RangeBar bar={bar} /> : null}
    </Card>
  );
}

/** The band between the two strikes with a marker at spot. */
export function RangeBar({ bar }) {
  return (
    <div data-testid="range-bar" aria-label={`Put ${formatUSD(bar.put)}, now ${formatUSD(bar.spot)}, call ${formatUSD(bar.call)}`} role="img">
      <div className="relative h-3 rounded bg-slate-800">
        <div className="absolute inset-y-0 rounded bg-profit/40" style={{ left: `${bar.bandLeft}%`, width: `${bar.bandWidth}%` }} />
        <div className={clsx("absolute -inset-y-1 w-0.5", bar.inside ? "bg-white" : "bg-loss")} style={{ left: `${bar.marker}%` }} />
      </div>
      <div className="mt-1 flex justify-between text-xs tabular-nums text-muted">
        <span>{formatUSD(bar.put)}</span>
        <span className="text-slate-200">now {formatUSD(bar.spot)}</span>
        <span>{formatUSD(bar.call)}</span>
      </div>
    </div>
  );
}

function MakingCard({ summary: s, bot }) {
  const coin = s.underlying;
  const m = s.income?.this_month;
  return (
    <Card title="Making or losing">
      <dl className="grid grid-cols-2 gap-2 text-base tabular-nums">
        <div>
          <dt className="text-xs text-muted">This month</dt>
          <dd>{m?.closes ? <Signed value={m.realised_coin}>{formatCoin(m.realised_coin, coin)}</Signed> : <span className="text-muted">not yet</span>}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Open now</dt>
          <dd>
            {isNumber(s.income?.unrealised_coin) ? (
              <Signed value={s.income.unrealised_coin}>{formatCoin(s.income.unrealised_coin, coin)}</Signed>
            ) : (
              <span className="text-muted">unknown</span>
            )}
          </dd>
        </div>
      </dl>
      <MonthBars income={s.income} />
      <Link href={`/income?bot=${encodeURIComponent(bot)}`} className={LINK}>
        Month by month →
      </Link>
    </Card>
  );
}

function WorstCard({ summary: s, bot }) {
  const coin = s.underlying;
  const w = worstCaseLine(s.worst_case);
  return (
    <Card title="Worst case right now" loss>
      {w.unknown ? (
        <p className="text-base text-warn">Unknown: {w.unknown}.</p>
      ) : (
        <p className="text-base">
          A very bad day ({coin} {formatSignedPct(w.move).replace("%", " %")}) would cost about{" "}
          <strong className="tabular-nums">
            {formatCoin(w.lossCoin, coin).replace(/^[+−]/, "")} ({formatUSD(w.lossUSD)})
          </strong>
          , {w.pct == null ? "% of the account unknown" : `${Math.abs(w.pct).toFixed(1)} % of the account`}.
        </p>
      )}
      <Link href={`/worst-case?bot=${encodeURIComponent(bot)}`} className={LINK}>
        What the bot does then →
      </Link>
    </Card>
  );
}

function LastActionCard({ summary: s, bot }) {
  const a = s.last_action;
  const coin = s.underlying;
  const at = a ? clockHM(a.at) : null;
  const move = a ? moveSince(a.spot, s.spot) : null;
  return (
    <Card title={`Last action${at ? ` · ${at}` : ""}`}>
      {a ? (
        <>
          <p className="text-base font-semibold">{a.reason || a.rule}</p>
          <p className="text-base tabular-nums">
            {move == null ? `${coin} move since: unknown` : `${coin} ${formatSignedPct(move).replace("%", " %")} since`}
            {a.kind === "close" && isNumber(a.pnl_coin) ? (
              <>
                {" · result "}
                <Signed value={a.pnl_coin}>{formatCoin(a.pnl_coin, coin)}</Signed>
              </>
            ) : null}
          </p>
        </>
      ) : (
        <p className="text-base">No trade yet. The bot&apos;s first sale or buy-back is explained here when it happens.</p>
      )}
      <Link href={`/activity?bot=${encodeURIComponent(bot)}`} className={LINK}>
        All actions, explained →
      </Link>
    </Card>
  );
}
