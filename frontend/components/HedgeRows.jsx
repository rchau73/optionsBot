"use client";

import clsx from "clsx";
import Link from "next/link";
import { useEffect, useRef } from "react";
import Badge from "./Badge";
import Panel from "./Panel";
import TermTip from "./TermTip";
import { approxUSD } from "@/lib/details";
import { formatAge, formatCoin, formatUSD, isNumber } from "@/lib/format";
import { HEDGE_BADGE, bandWords, hedgeShown, hedgeStaleAge, hedgeState, pausedWords, positionWords, pricedAtIndex } from "@/lib/futuresHedge";
import { coinOf } from "@/lib/overview";
import { formatSignedUSD } from "@/lib/summary";

const tone = (v) => (v > 0 ? "text-profit" : v < 0 ? "text-loss" : "");

/** The perpetual's six figures, coin first; stacks to two columns on a phone. */
export function HedgeFigures({ h, unit }) {
  const realised = isNumber(h.realised_coin) && isNumber(h.fees_coin) ? h.realised_coin - h.fees_coin : null;
  const usd = (coin) => approxUSD(isNumber(coin) && isNumber(h.perp_mark_price) ? coin * h.perp_mark_price : null);
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm tabular-nums sm:grid-cols-3 lg:grid-cols-6">
      <div>
        <dt className="text-xs text-muted">Position</dt>
        <dd>
          {formatSignedUSD(h.perp_usd)} <span className="text-xs text-muted">{positionWords(h)}</span>
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted">Delta and net</dt>
        <dd>
          {formatCoin(h.perp_delta_coin, unit, 4)} <span className="text-xs text-muted">net {formatCoin(h.net_delta_coin, unit, 4)}</span>
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted">Average / mark</dt>
        <dd>
          {formatUSD(h.perp_avg_price)} / {formatUSD(h.perp_mark_price)}
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted">Unrealised</dt>
        <dd className={tone(h.unrealised_coin)}>
          {formatCoin(h.unrealised_coin, unit, 4)} <span className="text-xs">{usd(h.unrealised_coin)}</span>
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted">Realised − fees</dt>
        <dd className={tone(realised)}>
          {formatCoin(realised, unit, 4)} <span className="text-xs">{usd(realised)}</span>
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted">Funding</dt>
        <dd>{pricedAtIndex(h) ? <span className="text-muted">left out on testnet</span> : formatCoin(h.funding_coin, unit, 4)}</dd>
      </div>
    </dl>
  );
}

/** The state's sentence under the title: the pause in amber, else what the position is. */
export function HedgeWords({ h, unit, state }) {
  const paused = state === "paused" ? pausedWords(h) : null;
  return (
    <>
      {paused ? <p className="rounded bg-warn/10 px-2 py-1 text-sm text-warn">{paused}</p> : null}
      <p className="text-sm text-muted">
        the bot&apos;s own position · holds the book&apos;s delta at zero within ±{bandWords(h, unit)} · {h.orders_last_hour ?? 0} of {h.max_orders_per_hour ?? 12} orders, last hour
        {pricedAtIndex(h) ? " · priced at the index (testnet)" : ""}
      </p>
    </>
  );
}

function HedgeRow({ bot, now }) {
  const h = bot.hedge;
  const unit = h.underlying || coinOf(bot);
  const state = hedgeState(h);
  const badge = HEDGE_BADGE[state];
  const age = hedgeStaleAge(h, now);
  return (
    <div className={clsx("space-y-2", age != null && "opacity-60")} data-testid={`hedge-row-${bot.name}`}>
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">
          {unit} · Futures hedge · {h.instrument}
        </h3>
        <TermTip term="perpetual" />
        <Badge tone={badge.tone}>{badge.text}</Badge>
        {age != null ? <span className="text-xs text-muted">as of {formatAge(age)}</span> : null}
      </div>
      <HedgeWords h={h} unit={unit} state={state} />
      <HedgeFigures h={h} unit={unit} />
      <Link href="/account" className="inline-flex min-h-11 items-center text-sm text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2">
        Switch on Account →
      </Link>
    </div>
  );
}

/**
 * The Details page's section (#666), under Open positions: one row per bot
 * with a hedge, a bot whose hedge cannot be read says so, nothing when every
 * bot is off and flat. The overview's line links here, and the rows only
 * exist after the first poll, so the jump is made once they do.
 */
export default function HedgeRows({ bots, now }) {
  const shown = bots.filter((b) => hedgeShown(b.hedge));
  const failed = bots.filter((b) => b.hedgeError);
  const any = shown.length > 0 || failed.length > 0;
  const box = useRef(null);
  const jumped = useRef(false);
  useEffect(() => {
    if (jumped.current || !any || window.location.hash !== "#hedge") return;
    jumped.current = true;
    box.current?.scrollIntoView?.();
  }, [any]);
  if (!any) return null;
  return (
    <div ref={box} className="scroll-mt-12">
      <Panel id="hedge" className="scroll-mt-12" title="Futures hedge">
        <div className="space-y-3">
          {shown.map((b) => (
            <HedgeRow key={b.name} bot={b} now={now} />
          ))}
          {failed.map((b) => (
            <p key={b.name} role="alert" className="text-sm text-warn">
              {coinOf(b)}: hedge state unavailable ({b.hedgeError}).
            </p>
          ))}
        </div>
      </Panel>
    </div>
  );
}
