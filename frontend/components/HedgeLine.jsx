"use client";

import clsx from "clsx";
import Link from "next/link";
import { formatAge, formatCoin } from "@/lib/format";
import { HEDGE_HEADLINE, hedgePnl, hedgeShown, hedgeStaleAge, hedgeState, pausedWords, perpLine, pricedAtIndex } from "@/lib/futuresHedge";
import { coinOf } from "@/lib/overview";
import { formatSignedUSD } from "@/lib/summary";
import { track } from "@/lib/track";

/**
 * One line per bot while a hedge is on (or a position is left), linking to its
 * row on Details (#666); nothing when it is off and flat. `net` adds the net
 * delta, for the phone, which has no Net delta tile.
 */
export default function HedgeLine({ bot, now, net = false }) {
  const h = bot.hedge;
  if (!hedgeShown(h)) return null;
  const state = hedgeState(h);
  const coin = coinOf(bot);
  const pnl = hedgePnl(h);
  const age = hedgeStaleAge(h, now);
  const paused = state === "paused" ? pausedWords(h) : null;
  const facts = [
    net ? `net delta ${formatCoin(h.net_delta_coin, coin, 4)}` : null,
    perpLine(h, coin),
    `P&L ${formatCoin(pnl.coin, coin, 4)} (${formatSignedUSD(pnl.usd)}) net of fees`,
    pricedAtIndex(h) ? "priced at the index (testnet)" : null,
    age != null ? `as of ${formatAge(age)}` : null,
  ];
  return (
    <Link
      href="/details#hedge"
      onClick={() => track("details_section_jump", { section: "hedge" })}
      data-testid={`hedge-line-${bot.name}`}
      className={clsx(
        "flex min-h-11 flex-wrap items-center gap-x-2 rounded-lg border bg-panel px-3 py-1.5 text-sm tabular-nums hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
        state === "on" ? "border-line" : "border-warn/60",
        age != null && "opacity-60",
      )}
    >
      <span className={clsx("font-semibold", state !== "on" && "text-warn")}>
        {coin} · {HEDGE_HEADLINE[state]}
      </span>
      {paused ? <span className="text-warn">{paused}</span> : null}
      <span className="text-muted">{facts.filter(Boolean).join(" · ")}</span>
      <span aria-hidden="true" className="ml-auto text-muted">
        →
      </span>
    </Link>
  );
}

/** Every bot's line; nothing at all when no bot shows one. */
export function HedgeLines({ bots, now }) {
  const shown = bots.filter((b) => hedgeShown(b.hedge));
  if (!shown.length) return null;
  return (
    <div className="space-y-1" data-testid="hedge-lines">
      {shown.map((b) => (
        <HedgeLine key={b.name} bot={b} now={now} />
      ))}
    </div>
  );
}
