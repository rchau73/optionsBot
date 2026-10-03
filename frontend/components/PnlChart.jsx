"use client";

import { useState } from "react";
import clsx from "clsx";
import { Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { usePnlHistory } from "@/hooks/usePnlHistory";
import { formatUSD } from "@/lib/format";

export const RANGES = [
  ["live", "Live"],
  ["1h", "1h"],
  ["6h", "6h"],
  ["1d", "1d"],
  ["1w", "1w"],
  ["1m", "1m"],
  ["all", "All"],
];

const SHORT = new Set(["live", "1h", "6h", "1d"]);

/** Axis label: time of day for short ranges, date for long ones (UTC). */
export function tickLabel(t, range) {
  const iso = new Date(t).toISOString();
  if (range === "live" || range === "1h") return iso.slice(11, 19);
  if (SHORT.has(range)) return iso.slice(11, 16);
  return `${iso.slice(5, 10)} ${iso.slice(11, 13)}h`;
}

/**
 * P&L across all bots in USD. "Live" is this session sampled every poll;
 * longer ranges come from the bots' stored history and survive restarts.
 */
export default function PnlChart({ live, names }) {
  const [range, setRange] = useState("live");
  const stored = usePnlHistory(names, range);
  const data = range === "live" ? live : stored.points;

  return (
    <div>
      <div className="mb-2 flex flex-wrap gap-1" role="tablist" aria-label="P&L range">
        {RANGES.map(([key, text]) => (
          <button
            key={key}
            role="tab"
            aria-selected={range === key}
            onClick={() => setRange(key)}
            className={clsx(
              "rounded px-2 py-0.5 text-xs focus-visible:outline focus-visible:outline-testnet",
              range === key ? "bg-slate-600 text-white" : "bg-slate-800 text-muted hover:text-white",
            )}
          >
            {text}
          </button>
        ))}
      </div>
      {stored.error && range !== "live" ? <p className="mb-1 text-xs text-warn">{stored.error}</p> : null}
      {data.length < 2 ? (
        <p className="py-10 text-center text-sm text-muted">
          {range === "live" || stored.loading ? "Collecting data…" : "No history for this range yet."}
        </p>
      ) : (
        <div className="h-56" aria-label={`P&L, range ${range}`}>
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 8 }}>
              <XAxis dataKey="t" tickFormatter={(t) => tickLabel(t, range)} stroke="#94a3b8" fontSize={11} minTickGap={50} />
              <YAxis tickFormatter={(v) => formatUSD(v)} stroke="#94a3b8" fontSize={11} width={70} />
              <ReferenceLine y={0} stroke="#475569" />
              <Tooltip
                contentStyle={{ background: "#111a2e", border: "1px solid #1e293b" }}
                labelFormatter={(t) => new Date(t).toISOString().replace("T", " ").slice(0, 19) + " UTC"}
                formatter={(v, name) => [formatUSD(v, { cents: true }), name === "totalUsd" ? "Total" : "Realized"]}
              />
              <Line type="monotone" dataKey="totalUsd" stroke="#38bdf8" dot={false} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="realisedUsd" stroke="#22c55e" dot={false} strokeWidth={1.5} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  );
}
