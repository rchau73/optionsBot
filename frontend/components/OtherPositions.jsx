import Badge from "./Badge";
import { formatCoin, formatNumber, formatPrice } from "@/lib/format";
import { sumByCurrency } from "@/lib/account";

const pnlTone = (v) => (v > 0 ? "text-profit" : v < 0 ? "text-loss" : "");

/** One-line state for the collapsed panel header. */
export function otherPositionsSummary({ rows, error, reported }) {
  if (!reported) return "not reported by this bot version";
  if (error) return "unavailable";
  if (!rows.length) return "none";
  const pnl = Object.entries(sumByCurrency(rows, "total_pnl"))
    .map(([c, v]) => formatCoin(v, c, 4))
    .join(" · ");
  return `${rows.length} position${rows.length > 1 ? "s" : ""} · P&L ${pnl}`;
}

/**
 * Positions on the account that no bot manages (bought or sold by hand, a
 * hedge, another instrument kind). No bot closes or sizes them, but Deribit
 * counts them in equity and margin, so they are shown with their margin.
 */
export default function OtherPositions({ data }) {
  const { rows, error, reported } = data;
  if (!reported) return <p className="py-3 text-center text-sm text-muted">This bot version does not report account positions.</p>;
  if (error) {
    return (
      <p className="py-3 text-center text-sm text-warn" role="alert">
        Account positions unavailable: {error}
      </p>
    );
  }
  if (!rows.length) return <p className="py-3 text-center text-sm text-muted">Every position on the account is managed by a bot.</p>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs uppercase text-muted">
          <tr className="border-b border-line">
            <th className="px-2 py-1">Instrument</th>
            <th className="px-2 py-1">Kind</th>
            <th className="px-2 py-1">Side</th>
            <th className="px-2 py-1 text-right">Size</th>
            <th className="px-2 py-1 text-right">Avg price</th>
            <th className="px-2 py-1 text-right">Mark</th>
            <th className="px-2 py-1 text-right">P&amp;L</th>
            <th className="px-2 py-1 text-right" title="Position delta, as Deribit reports it">Δ</th>
            <th className="px-2 py-1 text-right" title="Initial / maintenance margin of this position, as Deribit reports it">IM / MM</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((p) => (
            <tr key={`${p.currency}:${p.instrument}`} className="border-b border-line/50">
              <td className="px-2 py-1 font-mono text-xs">{p.instrument}</td>
              <td className="px-2 py-1 text-xs text-muted">{p.kind}</td>
              <td className="px-2 py-1">
                <Badge tone={p.size > 0 ? "good" : "warn"}>{p.size > 0 ? "LONG" : "SHORT"}</Badge>
              </td>
              <td className="px-2 py-1 text-right tabular-nums">{formatNumber(Math.abs(p.size), 1)}</td>
              <td className="px-2 py-1 text-right tabular-nums">{formatPrice(p.average_price)}</td>
              <td className="px-2 py-1 text-right tabular-nums">{formatPrice(p.mark_price)}</td>
              <td className={`px-2 py-1 text-right tabular-nums ${pnlTone(p.total_pnl)}`}>{formatCoin(p.total_pnl, p.currency, 4)}</td>
              <td className="px-2 py-1 text-right tabular-nums">{formatNumber(p.delta, 3)}</td>
              <td className="px-2 py-1 text-right tabular-nums">
                {formatPrice(p.initial_margin)} / {formatPrice(p.maintenance_margin)} {p.currency}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
