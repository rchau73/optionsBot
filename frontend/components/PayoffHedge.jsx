"use client";

import clsx from "clsx";
import { formatCoin } from "@/lib/format";
import { formatBookMove } from "@/lib/futuresHedge";
import { hedgeDeltaRows, wholeBookLine } from "@/lib/payoff";
import { formatSignedUSD } from "@/lib/summary";

// A strangle's share of the futures hedge on its payoff card (#695): the
// chart's two views, the delta rows, the moves table and, when the perpetual
// is not split, the line that points to the whole book. The bot computes every
// number; these only show them.

const VIEWS = [
  { hedged: true, label: "With its hedge share" },
  { hedged: false, label: "Options only" },
];

/** Which curves the chart draws; the view pressed is the one shown. */
export function HedgeViewToggle({ hedged, onChange }) {
  return (
    <div role="group" aria-label="Curves shown" className="inline-flex overflow-hidden rounded-full border border-line">
      {VIEWS.map((v) => (
        <button
          key={v.label}
          type="button"
          aria-pressed={v.hedged === hedged}
          onClick={() => onChange(v.hedged)}
          className={clsx(
            "min-h-11 px-3 text-sm font-semibold focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
            v.hedged === hedged ? "bg-testnet/20 text-testnet" : "text-muted",
          )}
        >
          {v.label}
        </button>
      ))}
    </div>
  );
}

/** The perpetual held but not shared out: one line and the way to the book panel. */
export function WholeBookNote({ report, coin }) {
  const line = wholeBookLine(report, coin);
  if (!line) return null;
  return (
    <p className="relative z-10 text-sm text-muted" data-testid="hedge-whole-book">
      {line} →{" "}
      <a href={`/positions?bot=${coin.toLowerCase()}#book-payoff`} className="text-testnet underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet">
        see Positions
      </a>
    </p>
  );
}

/** This strangle · its hedge share · net, in the delta box. */
export function HedgeDeltaRows({ sp, coin }) {
  return (
    <dl className="space-y-0.5 border-t border-line pt-1" aria-label="Delta with the hedge share">
      {hedgeDeltaRows(sp, coin).map((r) => (
        <div key={r.key} className="flex justify-between gap-2">
          <dt className="text-muted">{r.label}</dt>
          <dd className={clsx(r.key === "net" && "font-semibold")}>{r.text}</dd>
        </div>
      ))}
    </dl>
  );
}

function Amount({ coinValue, usd, coin }) {
  return (
    <td className={clsx("px-2 py-1 text-right", usd < 0 && "text-loss")}>
      {formatCoin(coinValue, coin, 4)} <span className="text-xs">({formatSignedUSD(usd)})</span>
    </td>
  );
}

/** The moves, options only against with the hedge share, and what the curve assumes. */
export function HedgeMovesTable({ sp, coin }) {
  const moves = sp.hedge_moves ?? [];
  return (
    <div className="space-y-1">
      {moves.length ? (
        <div className="overflow-x-auto">
          <table className="w-full text-sm tabular-nums" aria-label="If bought back today at a moved price">
            <thead className="text-left text-xs uppercase text-muted">
              <tr className="border-b border-line">
                <th className="px-2 py-1">Move</th>
                <th className="px-2 py-1 text-right">Options only</th>
                <th className="px-2 py-1 text-right">With its hedge share</th>
              </tr>
            </thead>
            <tbody>
              {moves.map((m) => (
                <tr key={m.move_pct} className="border-b border-line/50" data-testid={`hedge-move-${m.move_pct}`}>
                  <td className="px-2 py-1">{formatBookMove(m.move_pct)}</td>
                  <Amount coinValue={m.options_coin} usd={m.options_usd} coin={coin} />
                  <Amount coinValue={m.hedged_coin} usd={m.hedged_usd} coin={coin} />
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-sm text-warn">Moves unknown: there is no today curve for this strangle.</p>
      )}
      <p className="text-xs text-muted">
        The hedge is drawn as held now. The bot re-hedges each time the book&apos;s delta leaves its band, so a real move lands near the solid blue curve, minus fees.
      </p>
    </div>
  );
}
