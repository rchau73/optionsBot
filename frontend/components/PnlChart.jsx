"use client";

import { useState } from "react";
import clsx from "clsx";
import { Legend, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { usePnlHistory } from "@/hooks/usePnlHistory";
import { formatDateTime, formatUSD } from "@/lib/format";
import { localParts } from "@/lib/time";

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

// One colour per bot line (dashed), distinct from Total (blue) and Realized (green).
const BOT_COLORS = ["#f59e0b", "#c084fc", "#f43f5e", "#2dd4bf"];

/** Legend and tooltip name of a series key. */
export function seriesName(key) {
  if (key === "totalUsd") return "Total";
  if (key === "realisedUsd") return "Realized";
  return key.replace(/^bots\./, "").toUpperCase();
}

/** Axis label in the display zone (BRT): time of day for short ranges, date for long ones. */
export function tickLabel(t, range) {
  const p = localParts(t);
  if (!p) return "";
  if (range === "live" || range === "1h") return `${p.hour}:${p.minute}:${p.second}`;
  if (SHORT.has(range)) return `${p.hour}:${p.minute}`;
  return `${p.month}-${p.day} ${p.hour}h`;
}

/**
 * P&L across all bots in USD, plus one dashed line per bot when there are
 * several. "Live" is this session sampled every poll; longer ranges come
 * from the bots' stored history and survive restarts.
 */
export default function PnlChart({ live, names }) {
  const [range, setRange] = useState("live");
  const stored = usePnlHistory(names, range);
  const data = range === "live" ? live : stored.points;
  const perBot = names?.length > 1 ? names : []; // names is null until the bot list loads

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
                labelFormatter={(t) => formatDateTime(t)}
                formatter={(v, name) => [formatUSD(v, { cents: true }), seriesName(name)]}
              />
              {perBot.length ? <Legend formatter={seriesName} wrapperStyle={{ fontSize: 11 }} /> : null}
              <Line type="monotone" dataKey="totalUsd" stroke="#38bdf8" dot={false} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="realisedUsd" stroke="#22c55e" dot={false} strokeWidth={1.5} isAnimationActive={false} />
              {perBot.map((bot, i) => (
                <Line
                  key={bot}
                  type="monotone"
                  dataKey={`bots.${bot}`}
                  stroke={BOT_COLORS[i % BOT_COLORS.length]}
                  strokeDasharray="5 3"
                  dot={false}
                  strokeWidth={1.5}
                  isAnimationActive={false}
                  connectNulls
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
    </div>
  );
}
