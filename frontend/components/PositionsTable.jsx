import clsx from "clsx";
import Badge from "./Badge";
import { formatCoin, formatNumber, formatPct, formatPrice, formatUSD, isNumber } from "@/lib/format";

const pnlClass = (v) => clsx("tabular-nums", v > 0 && "text-profit", v < 0 && "text-loss");

/** Open legs grouped (strategy, slot, expiry or type), with totals per group. */
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
            <th className="px-2 py-1 text-right">Strike</th>
            <th className="px-2 py-1 text-right">DTE</th>
            <th className="px-2 py-1 text-right">Qty</th>
            <th className="px-2 py-1 text-right">Entry</th>
            <th className="px-2 py-1 text-right">Mark</th>
            <th className="px-2 py-1 text-right">P&L</th>
            <th className="px-2 py-1 text-right">Captured</th>
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
              <td colSpan={8} className="px-2 py-1 font-semibold">
                {g.key} <span className="text-xs font-normal text-muted">· {g.legs} legs</span>
              </td>
              <td className={clsx("px-2 py-1 text-right font-semibold", pnlClass(g.pnlUsd))}>
                {g.unit ? formatCoin(g.pnl, g.unit) : ""}
                <div className="text-xs">{formatUSD(g.pnlUsd)}</div>
              </td>
              <td colSpan={7} />
            </tr>
            {g.rows.map((r) => (
              <tr key={r.key} className="hover:bg-slate-800/30">
                <td className="px-2 py-1 font-mono text-xs">{r.instrument}</td>
                <td className="px-2 py-1 text-xs text-muted">{r.slot}</td>
                <td className="px-2 py-1">
                  <Badge tone={r.type === "call" ? "call" : "put"}>{r.type}</Badge>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(r.strike, 0)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(r.dte, 1)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{r.qty}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatPrice(r.entry)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatPrice(r.mark)}</td>
                <td className={clsx("px-2 py-1 text-right", pnlClass(r.pnl))}>
                  {formatCoin(r.pnl, r.unit)}
                  <div className="text-xs">{formatUSD(r.pnlUsd)}</div>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">{formatPct(r.roiPct, 0)}</td>
                <td className={clsx("px-2 py-1 text-right tabular-nums", isNumber(r.lossMultiple) && r.lossMultiple > 1 && "text-warn")}>
                  {formatPrice(r.stopMark)}
                </td>
                <td className="px-2 py-1">
                  <Badge tone={r.moneyness === "ITM" ? "bad" : r.moneyness === "ATM" ? "warn" : "muted"}>
                    {r.moneyness} {isNumber(r.distancePct) ? `${r.distancePct.toFixed(1)}%` : ""}
                  </Badge>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(r.delta, 3)}</td>
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
