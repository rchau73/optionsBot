"use client";

import Link from "next/link";
import { useId, useState } from "react";
import { Signed } from "./InvestorSummary";
import { formatCoin, formatDateTime } from "@/lib/format";
import { lastTradeLine } from "@/lib/overview";
import { formatSignedUSD } from "@/lib/summary";

// The band's last trade (#489): "Last trade · <when>: sold 1.00 × <instrument>"
// and a Why? that opens, in one click, the reason the bot recorded in its
// journal (and, for a buy-back, its past result net of fees). Activity holds
// every trade; this line answers only "why did it make its last trade?".

const LINK =
  "inline-flex min-h-11 items-center text-sm text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** bot: one bot of useMonitor (its summary's last_action); coin: its underlying. */
export default function LastTrade({ bot, coin }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const t = lastTradeLine(bot);
  const activity = (
    <Link href={`/activity?bot=${encodeURIComponent(bot.name)}`} className={LINK}>
      All actions, explained →
    </Link>
  );
  if (t.unknown || t.none) {
    return (
      <section aria-label={`${coin} · last trade`} className="flex flex-wrap items-center gap-x-3 text-sm" data-testid={`last-trade-${bot.name}`}>
        <p className={t.unknown ? "text-warn" : "text-muted"}>
          {t.unknown ? `Last trade unknown: ${t.unknown}.` : "No trade yet. The bot's first sale or buy-back is explained here when it happens."}
        </p>
        {activity}
      </section>
    );
  }
  return (
    <section aria-label={`${coin} · last trade`} className="space-y-1 text-sm" data-testid={`last-trade-${bot.name}`}>
      <div className="flex flex-wrap items-center gap-x-3">
        <p>
          <span className="font-semibold">Last trade</span> · {formatDateTime(t.when)}: {t.what}
        </p>
        <button
          type="button"
          aria-expanded={open}
          aria-controls={id}
          onClick={() => setOpen(!open)}
          className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-line px-2 text-xs text-testnet hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
        >
          Why?
        </button>
        {activity}
      </div>
      {open ? (
        <div id={id} className="rounded-md border border-line bg-panel px-3 py-2" data-testid="last-trade-why">
          <p className="text-base font-semibold">{t.reason}</p>
          {t.detail ? <p className="text-xs text-muted">{t.detail}</p> : null}
          {t.pnlCoin != null || t.pnlUSD != null ? (
            <p className="text-base">
              Result of that trade (past, net of fees): <Signed value={t.pnlCoin}>{formatCoin(t.pnlCoin, coin)}</Signed> ·{" "}
              <Signed value={t.pnlUSD}>{formatSignedUSD(t.pnlUSD)}</Signed>
            </p>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
