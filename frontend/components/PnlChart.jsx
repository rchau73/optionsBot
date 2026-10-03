"use client";

import { Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { formatUSD } from "@/lib/format";

const time = (t) => new Date(t).toISOString().slice(11, 19);

/** Session P&L (all bots, USD) sampled every poll. */
export default function PnlChart({ history }) {
  if (history.length < 2) return <p className="py-10 text-center text-sm text-muted">Collecting data…</p>;
  return (
    <div className="h-56" aria-label="P&L over this session">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={history} margin={{ top: 8, right: 12, bottom: 0, left: 8 }}>
          <XAxis dataKey="t" tickFormatter={time} stroke="#94a3b8" fontSize={11} minTickGap={60} />
          <YAxis tickFormatter={(v) => formatUSD(v)} stroke="#94a3b8" fontSize={11} width={70} />
          <ReferenceLine y={0} stroke="#475569" />
          <Tooltip
            contentStyle={{ background: "#111a2e", border: "1px solid #1e293b" }}
            labelFormatter={time}
            formatter={(v, name) => [formatUSD(v, { cents: true }), name === "totalUsd" ? "Total" : "Realised"]}
          />
          <Line type="monotone" dataKey="totalUsd" stroke="#38bdf8" dot={false} strokeWidth={2} isAnimationActive={false} />
          <Line type="monotone" dataKey="realisedUsd" stroke="#22c55e" dot={false} strokeWidth={1.5} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
