import clsx from "clsx";
import Badge from "./Badge";
import { ageSeconds, formatCoin, formatNumber, formatPct, formatPrice, formatUSD, isNumber } from "@/lib/format";
import { STALE_AFTER_SEC } from "@/lib/monitor";

const pnlClass = (v) => clsx("tabular-nums", v > 0 && "text-profit", v < 0 && "text-loss");

/** Renders a live value; re-keying by value replays the flash when it changes. */
function Live({ value, children }) {
  return (
    <span key={String(value)} className="flash px-0.5">
      {children}
    </span>
  );
}

/** Small tag when a leg's price is not live or is getting old. */
function Freshness({ row }) {
  if (row.markSource !== "live") {
    return <div className="text-[10px] text-warn" title="No live ticker for this instrument; value from the last decision cycle">last cycle</div>;
  }
  const age = ageSeconds(row.markAsOf);
  if (age != null && age > STALE_AFTER_SEC) {
    return <div className="text-[10px] text-warn">{Math.floor(age)}s old</div>;
  }
  return null;
}

/**
 * A group's position greeks (per-option greek × qty, short-signed), so they
 * differ in scale from the per-option greeks on the leg rows below.
 * Delta and gamma are blank when the group mixes underlyings.
 */
function GreekCells({ greeks }) {
  const title = "Position greeks of the group: each leg's greek × qty, negative for shorts";
  return (
    <>
      <td className="px-2 py-1 text-right font-semibold tabular-nums" title={title}>{formatNumber(greeks.delta, 3)}</td>
      <td className="px-2 py-1 text-right font-semibold tabular-nums" title={title}>{isNumber(greeks.gamma) ? greeks.gamma.toExponential(1) : "—"}</td>
      <td className="px-2 py-1 text-right font-semibold tabular-nums" title={title}>{formatNumber(greeks.theta, 1)}</td>
      <td className="px-2 py-1 text-right font-semibold tabular-nums" title={title}>{formatNumber(greeks.vega, 1)}</td>
    </>
  );
}

/** Open legs, one row per bot + slot + strike (see consolidateRows), grouped (strategy, slot, expiry or type), with unrealized P&L per group. Live values flash when they change. */
export default function PositionsTable({ groups }) {
  if (!groups.length) {
    return <p className="px-1 py-6 text-center text-sm text-muted">No open positions match the filters.</p>;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[1100px] text-sm">
        <thead className="text-left text-xs uppercase text-muted">
          <tr className="border-b border-line">
            <th className="px-2 py-1">Instrument</th>
            <th className="px-2 py-1">Slot</th>
            <th className="px-2 py-1">Type</th>
            <th className="px-2 py-1" title="SELL = short (premium collected, profits as the option loses value); BUY = long (premium paid, e.g. an iron-condor wing)">Side</th>
            <th className="px-2 py-1 text-right">Strike</th>
            <th className="px-2 py-1 text-right">DTE</th>
            <th className="px-2 py-1 text-right">Qty</th>
            <th className="px-2 py-1 text-right">Entry</th>
            <th className="px-2 py-1 text-right">Bid / Ask</th>
            <th className="px-2 py-1 text-right" title="Mid price used to value the leg, from the live ticker">Mark</th>
            <th className="px-2 py-1 text-right" title="Premium received minus the cost to buy back at the mark">Unrealized P&L</th>
            <th
              className="px-2 py-1 text-right"
              title="Unrealized P&L as % of the premium: +50% = half the premium is profit; 0% = break-even; -100% = the loss equals the premium. The stop-loss fires at -(stop_loss_multiplier) × 100%, e.g. -200%."
            >
              P&L %
            </th>
            <th className="px-2 py-1 text-right" title="Mark at which the stop-loss fires">Stop @</th>
            <th className="px-2 py-1">Moneyness</th>
            <th className="px-2 py-1 text-right">Δ</th>
            <th className="px-2 py-1 text-right">Γ</th>
            <th className="px-2 py-1 text-right">Θ</th>
            <th className="px-2 py-1 text-right">Vega</th>
          </tr>
        </thead>
        {groups.map((g) => (
          <tbody key={g.key} className="border-b border-line">
            <tr className="bg-slate-800/40">
              <td colSpan={10} className="px-2 py-1 font-semibold">
                {g.key} <span className="text-xs font-normal text-muted">· {g.legs} legs</span>
              </td>
              <td className={clsx("px-2 py-1 text-right font-semibold", pnlClass(g.pnlUsd))}>
                {g.unit ? formatCoin(g.pnl, g.unit) : ""}
                <div className="text-xs">{formatUSD(g.pnlUsd)}</div>
              </td>
              <td className={clsx("px-2 py-1 text-right font-semibold tabular-nums", pnlClass(g.roiPct))} title="Group unrealized P&L as % of the group's premium (in USD)">
                {formatPct(g.roiPct, 0, { signed: true })}
              </td>
              <td colSpan={2} />
              <GreekCells greeks={g.greeks} />
            </tr>
            {g.rows.map((r) => (
              <tr key={r.key} className="hover:bg-slate-800/30">
                <td className="px-2 py-1 font-mono text-xs">
                  {r.instrument}
                  {r.positions > 1 ? (
                    <span className="ml-1 text-[10px] text-muted" title={`${r.positions} positions on this strike, shown as one (entry is their average; stop is the first to fire)`}>
                      ×{r.positions}
                    </span>
                  ) : null}
                </td>
                <td className="px-2 py-1 text-xs text-muted">{r.slot}</td>
                <td className="px-2 py-1">
                  <Badge tone={r.type === "call" ? "call" : "put"}>{r.type}</Badge>
                </td>
                <td className="px-2 py-1">
                  <Badge tone={r.side === "buy" ? "good" : "warn"}>{r.side === "buy" ? "BUY" : "SELL"}</Badge>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(r.strike, 0)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(r.dte, 1)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{r.qty}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatPrice(r.entry)}</td>
                <td className="px-2 py-1 text-right text-xs tabular-nums text-muted">
                  <Live value={`${r.bid}/${r.ask}`}>
                    {formatPrice(r.bid)} / {formatPrice(r.ask)}
                  </Live>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">
                  <Live value={r.mark}>{formatPrice(r.mark)}</Live>
                  <Freshness row={r} />
                </td>
                <td className={clsx("px-2 py-1 text-right", pnlClass(r.pnl))}>
                  <Live value={r.pnl}>{formatCoin(r.pnl, r.unit)}</Live>
                  <div className="text-xs">{formatUSD(r.pnlUsd)}</div>
                </td>
                <td className={clsx("px-2 py-1 text-right tabular-nums", pnlClass(r.roiPct))}>{formatPct(r.roiPct, 0, { signed: true })}</td>
                <td className={clsx("px-2 py-1 text-right tabular-nums", isNumber(r.lossMultiple) && r.lossMultiple > 1 && "text-warn")}>
                  {formatPrice(r.stopMark)}
                </td>
                <td className="px-2 py-1">
                  <Badge tone={r.moneyness === "ITM" ? "bad" : r.moneyness === "ATM" ? "warn" : "muted"}>
                    {r.moneyness} {isNumber(r.distancePct) ? `${r.distancePct.toFixed(1)}%` : ""}
                  </Badge>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">
                  <Live value={r.delta}>{formatNumber(r.delta, 3)}</Live>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">{isNumber(r.gamma) ? r.gamma.toExponential(1) : "—"}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(r.theta, 1)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(r.vega, 1)}</td>
              </tr>
            ))}
          </tbody>
        ))}
      </table>
    </div>
  );
}
