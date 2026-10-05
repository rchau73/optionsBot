import { ageSeconds, formatAge, formatCountdown, formatPrice, slotLabel } from "@/lib/format";

/** Entry and repair orders still working on the book. */
export default function PendingOrders({ bots }) {
  const rows = bots.flatMap((b) =>
    (b.orders?.pending ?? []).flatMap((p) =>
      (p.legs ?? []).map((l) => ({ bot: b.name, p, l, key: `${b.name}:${l.order_id}` })),
    ),
  );
  if (!rows.length) return <p className="py-3 text-center text-sm text-muted">No working orders.</p>;
  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs uppercase text-muted">
        <tr className="border-b border-line">
          <th className="px-2 py-1">Bot</th>
          <th className="px-2 py-1">Instrument</th>
          <th className="px-2 py-1">Slot</th>
          <th className="px-2 py-1 text-right">Filled</th>
          <th className="px-2 py-1 text-right">Limit</th>
          <th className="px-2 py-1 text-right" title="Unfilled orders are cancelled at this time; filled legs are kept">
            Timeout
          </th>
          <th className="px-2 py-1 text-right">Age</th>
        </tr>
      </thead>
      <tbody>
        {rows.map(({ bot, p, l, key }) => (
          <tr key={key} className="border-b border-line/50">
            <td className="px-2 py-1 uppercase">{bot}</td>
            <td className="px-2 py-1 font-mono text-xs">
              {l.instrument} {p.repair ? <span className="text-warn">(repair)</span> : null}
            </td>
            <td className="px-2 py-1 text-xs text-muted">{slotLabel(p.slot)}</td>
            <td className="px-2 py-1 text-right tabular-nums">
              {l.filled_qty}/{l.qty}
            </td>
            <td className="px-2 py-1 text-right tabular-nums">{formatPrice(l.limit_price)}</td>
            <td className="px-2 py-1 text-right tabular-nums">{formatCountdown(p.timeout_at)}</td>
            <td className="px-2 py-1 text-right text-muted">{formatAge(ageSeconds(p.submitted_at))}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
