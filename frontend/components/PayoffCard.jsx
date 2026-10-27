"use client";

import clsx from "clsx";
import PayoffChart from "./PayoffChart";
import { formatTime } from "@/lib/format";
import { cardTitle, deltaLine, earnedBar, earnedLine, legDeltaLines, missingLine } from "@/lib/payoff";

// One strangle's card (#391): title, the "premium earned so far" bar, the
// payoff chart and the delta box. Full on the Positions page and in the
// modal (the delta box beside the chart, below it on a phone); compact on the
// overview (a button that opens the modal, the delta box as one line).

/**
 * sp: one strangle of /api/strangles/payoff; report: the whole answer
 * (coin, as_of, the bot's net delta, the model). onOpen(strangleId, element):
 * compact only, opens the modal.
 */
export default function PayoffCard({ sp, report, compact = false, onOpen, headingLevel = "h3" }) {
  const coin = report?.underlying || "";
  const Heading = headingLevel;
  const title = cardTitle(coin, sp);
  return (
    <section
      aria-label={title}
      className={clsx(
        "w-full space-y-2 rounded-lg border border-line bg-panel p-3",
        compact && "relative hover:border-muted focus-within:border-testnet",
      )}
      data-testid={`payoff-card-${sp.strangle_id}`}
    >
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <Heading className="text-base font-semibold">
          {compact ? (
            // The whole card is the button's target (its ::after covers the card).
            <button
              type="button"
              onClick={(e) => onOpen?.(sp.strangle_id, e.currentTarget)}
              aria-haspopup="dialog"
              className="min-h-11 text-left after:absolute after:inset-0 after:content-[''] focus-visible:outline-none"
            >
              {title}
              <span className="sr-only"> — open the payoff chart</span>
            </button>
          ) : (
            title
          )}
        </Heading>
        <span className="text-xs text-muted">as of {formatTime(report?.as_of)}</span>
      </header>
      <Earned sp={sp} coin={coin} />
      {(sp.missing ?? []).map((m) => (
        <p key={m.option_type} className="text-sm text-warn">
          {missingLine(m)}
        </p>
      ))}
      {compact ? (
        <>
          <PayoffChart sp={sp} coin={coin} compact />
          <p className="text-sm tabular-nums">{deltaLine(sp.net_delta_coin, sp.net_delta_usd_per_1pct, coin)}</p>
        </>
      ) : (
        <div className="grid gap-3 lg:grid-cols-[1fr_16rem]">
          <PayoffChart sp={sp} coin={coin} />
          <DeltaBox sp={sp} report={report} coin={coin} />
        </div>
      )}
      {compact ? null : <p className="text-xs text-muted">Model: {report?.model || "unknown"}. A model, not a quote; the dot is the book&apos;s P&L now.</p>}
    </section>
  );
}

/** "62 % of the premium earned · +0.0058 of +0.0094 BTC" with its bar. */
function Earned({ sp, coin }) {
  const pct = earnedBar(sp);
  return (
    <div className="space-y-1">
      <p className={clsx("text-sm tabular-nums", sp.unrealised_coin < 0 && "text-loss")}>{earnedLine(sp, coin)}</p>
      <div className="h-2 w-full overflow-hidden rounded bg-slate-800" role="presentation">
        <div className="h-full bg-profit" style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

/** The strangle's net delta, each leg's, and the bot's for comparison. */
function DeltaBox({ sp, report, coin }) {
  return (
    <aside aria-label="Delta" className="space-y-1 rounded border border-line p-3 text-sm tabular-nums">
      <h4 className="text-xs font-semibold uppercase tracking-wide text-muted">Delta (short-signed)</h4>
      <p className="text-base font-semibold">{deltaLine(sp.net_delta_coin, sp.net_delta_usd_per_1pct, coin)}</p>
      <ul className="space-y-0.5">
        {legDeltaLines(sp, coin).map((l) => (
          <li key={l.key} className={l.type === "put" ? "text-put" : "text-call"}>
            {l.text}
          </li>
        ))}
      </ul>
      <p className="text-muted">Whole bot: {deltaLine(report?.bot_net_delta, report?.bot_net_delta_usd_per_1pct, coin)}</p>
    </aside>
  );
}
