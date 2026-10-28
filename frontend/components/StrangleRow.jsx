"use client";

import clsx from "clsx";
import { daysLeft, keptOf, rangeStrip } from "@/lib/band";
import { formatDate } from "@/lib/format";
import { missingLine } from "@/lib/payoff";
import { formatSignedUSD } from "@/lib/summary";
import { fmtCoin } from "@/lib/tiles";

// One strangle on the overview (#404): a row-button with its slot, expiry,
// days left and P&L in coin (≈ USD below); a range bar (the break-even
// corridor, put and call ticks, the spot dot, a hatched amber stub on a side
// with no leg) and "spot x % from a break-even"; the premium kept with a small
// bar; an amber note when a leg is held back. A click opens the payoff modal.
// Only spans inside: a button holds phrasing content.

/** The bar under the row's figures; aria-hidden — the row's label says it in words. */
function RangeStrip({ r }) {
  return (
    <span aria-hidden="true" className="relative block h-3 w-full rounded bg-slate-800" data-testid="range-strip">
      <span className={clsx("absolute inset-y-0 block", r.inside ? "bg-profit/25" : "bg-loss/25")} style={{ left: `${r.corridor.left}%`, width: `${r.corridor.width}%` }} />
      {r.stubs.map((s) => (
        <span key={s.side} data-testid={`stub-${s.side}`} className="hatch-warn absolute inset-y-0 block rounded" style={{ left: `${s.left}%`, width: `${s.width}%` }} />
      ))}
      {r.put != null ? <span data-testid="tick-put" className="absolute -top-0.5 block h-4 w-0.5 bg-put" style={{ left: `${r.put}%` }} /> : null}
      {r.call != null ? <span data-testid="tick-call" className="absolute -top-0.5 block h-4 w-0.5 bg-call" style={{ left: `${r.call}%` }} /> : null}
      <span
        data-testid="spot-dot"
        className="absolute top-1/2 block h-2.5 w-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full border border-panel bg-slate-100"
        style={{ left: `${r.spot}%` }}
      />
    </span>
  );
}

/** sp: one strangle of /api/strangles/payoff; coin: its underlying; onOpen(strangleId, element). */
export default function StrangleRow({ sp, coin, onOpen }) {
  const days = daysLeft(sp);
  const r = rangeStrip(sp);
  const kept = keptOf(sp);
  const pnl = fmtCoin(sp.unrealised_coin, coin);
  const notes = (sp.missing ?? []).map(missingLine).filter(Boolean);
  const daysText = days != null ? `${days} d left` : "days left unknown";
  const distance = r ? r.distanceText : "spot unknown";
  const label = [`${coin} ${sp.slot?.dte ?? "?"} d slot`, `expiry ${formatDate(sp.expiry)}`, daysText, `P&L ${pnl}`, distance, kept.text, ...notes].join(", ");
  return (
    <button
      type="button"
      aria-haspopup="dialog"
      aria-label={`${label} — open the payoff chart`}
      onClick={(e) => onOpen?.(sp.strangle_id, e.currentTarget)}
      className="block w-full space-y-1.5 rounded-lg border border-line bg-panel px-3 py-2 text-left hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
      data-testid={`strangle-row-${sp.strangle_id}`}
    >
      <span className="flex items-baseline justify-between gap-2">
        <span className="text-sm">
          <span className="font-semibold">{sp.slot?.dte ?? "?"} d slot</span>
          <span className="text-muted">
            {" "}
            · {formatDate(sp.expiry)} · {daysText}
          </span>
        </span>
        <span className="text-right tabular-nums">
          <span className={clsx("block text-sm font-semibold", sp.unrealised_coin > 0 && "text-profit", sp.unrealised_coin < 0 && "text-loss")}>{pnl}</span>
          <span className="block text-xs text-muted">≈ {formatSignedUSD(sp.unrealised_usd)}</span>
        </span>
      </span>
      {r ? <RangeStrip r={r} /> : null}
      <span className={clsx("block text-xs tabular-nums", r && !r.inside ? "text-loss" : "text-muted")}>{distance}</span>
      <span className="flex items-center gap-2 text-xs tabular-nums">
        <span className={clsx(sp.earned_pct < 0 ? "text-loss" : "text-muted")}>{kept.text}</span>
        <span aria-hidden="true" className="block h-1.5 w-16 overflow-hidden rounded bg-slate-800">
          <span className="block h-full bg-profit" style={{ width: `${kept.bar}%` }} />
        </span>
      </span>
      {notes.map((n) => (
        <span key={n} className="block text-xs text-warn" data-testid="held-note">
          {n}
        </span>
      ))}
    </button>
  );
}
