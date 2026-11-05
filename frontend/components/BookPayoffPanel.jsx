"use client";

import { Legend, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { formatCoin, formatUSD } from "@/lib/format";
import { bookChartLabel, bookChartPoints, formatBookMove } from "@/lib/futuresHedge";
import { formatSignedUSD } from "@/lib/summary";

// Two series, one axis: the hedge solid, the options alone dashed, each named
// at −10 % beside the legend; the table is the text alternative.
const SERIES = { book: "With the hedge", options: "Options only" };

/** The series' name beside its first point. */
const firstPointLabel = (text) =>
  function FirstPointLabel({ x, y, index }) {
    if (index !== 0) return null;
    return (
      <text x={x + 8} y={y - 8} fill="#e2e8f0" fontSize={11}>
        {text}
      </text>
    );
  };

function Chart({ scenarios }) {
  return (
    <div className="h-56" role="img" aria-label={bookChartLabel(scenarios)}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={bookChartPoints(scenarios)} margin={{ top: 12, right: 16, bottom: 0, left: 0 }}>
          <XAxis dataKey="move" type="number" domain={[-10, 10]} ticks={[-10, -5, 0, 5, 10]} tickFormatter={(v) => `${v} %`} stroke="#94a3b8" fontSize={11} />
          <YAxis tickFormatter={(v) => formatUSD(v)} stroke="#94a3b8" fontSize={11} width={70} />
          <ReferenceLine y={0} stroke="#475569" />
          <Tooltip
            contentStyle={{ backgroundColor: "#111a2e", border: "1px solid #475569", fontSize: 12 }}
            labelFormatter={(v) => `${v} %`}
            formatter={(v, name) => [formatSignedUSD(v), SERIES[name] ?? name]}
          />
          <Legend formatter={(name) => SERIES[name] ?? name} wrapperStyle={{ fontSize: 11 }} />
          <Line type="linear" dataKey="book" stroke="#38bdf8" strokeWidth={2} dot={{ r: 4 }} isAnimationActive={false} label={firstPointLabel(SERIES.book)} />
          <Line type="linear" dataKey="options" stroke="#94a3b8" strokeWidth={2} strokeDasharray="6 4" dot={{ r: 4 }} isAnimationActive={false} label={firstPointLabel(SERIES.options)} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

function Amount({ coin, usd, unit }) {
  return (
    <td className={`px-2 py-1 text-right ${usd < 0 ? "text-loss" : ""}`}>
      {formatCoin(coin, unit, 4)} <span className="text-xs">({formatSignedUSD(usd)})</span>
    </td>
  );
}

/**
 * The whole book for a one-day move on the Positions page (#666): the options
 * with and without the perpetual, the same delta–gamma model as the bot's
 * switch-off preview. h.book_scenarios is empty while a leg has no greeks.
 */
export default function BookPayoffPanel({ h, coin }) {
  const scenarios = h.book_scenarios ?? [];
  return (
    <section aria-label="The whole book for a one-day move" className="w-full space-y-2 rounded-lg border border-line bg-panel p-3" data-testid="book-payoff">
      <h2 className="text-base font-semibold">The whole book, for a one-day move</h2>
      {scenarios.length ? (
        <>
          <Chart scenarios={scenarios} />
          <div className="overflow-x-auto">
            <table className="w-full text-sm tabular-nums">
              <thead className="text-left text-xs uppercase text-muted">
                <tr className="border-b border-line">
                  <th className="px-2 py-1">Move</th>
                  <th className="px-2 py-1 text-right">With the hedge</th>
                  <th className="px-2 py-1 text-right">Options only</th>
                </tr>
              </thead>
              <tbody>
                {scenarios.map((s) => (
                  <tr key={s.move_pct} className="border-b border-line/50" data-testid={`book-row-${s.move_pct}`}>
                    <td className="px-2 py-1">{formatBookMove(s.move_pct)}</td>
                    <Amount coin={s.book_coin} usd={s.book_usd} unit={coin} />
                    <Amount coin={s.options_coin} usd={s.options_usd} unit={coin} />
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : (
        <p className="text-sm text-muted">Not yet: a leg has no greeks or the price is missing, so the book cannot be priced.</p>
      )}
      <p className="text-sm">The hedge takes out the slope (delta), not the curve (gamma): the book still loses on a big move either way.</p>
      <p className="text-xs text-muted">A model, not a quote: delta and gamma of the options plus the perpetual&apos;s inverse P&amp;L, from today&apos;s price.</p>
    </section>
  );
}
