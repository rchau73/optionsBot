"use client";

import { useState } from "react";
import clsx from "clsx";
import { Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { usePnlHistory } from "@/hooks/usePnlHistory";
import { formatCoin, formatDateTime } from "@/lib/format";
import { RANGES, tickLabel } from "./PnlChart";

const COIN_RANGES = RANGES.filter(([key]) => key !== "live"); // stored history only

/** Legend and tooltip name of a coin series key. */
export function coinSeriesName(key) {
  return key === "realised" ? "Realized" : "Total";
}

/**
 * P&L of each bot in its own coin (BTC, ETH), from the bots' stored history.
 * The strategy's goal is to grow the coin stack, and the USD chart mixes that
 * with the coin's price: here a rally or a crash moves nothing by itself. The
 * coins differ, so each bot has its own chart (they cannot be summed).
 */
export default function CoinPnlChart({ names, units = {} }) {
  const [range, setRange] = useState("1d");
  const { perBot, error, loading } = usePnlHistory(names, range);

  return (
    <div>
      <p className="mb-2 text-xs text-muted">
        Realized + open P&amp;L, net of fees, counted in each bot&apos;s coin: the coin&apos;s own price moves are left out.
      </p>
      <div className="mb-2 flex flex-wrap gap-1" role="tablist" aria-label="Coin P&L range">
        {COIN_RANGES.map(([key, text]) => (
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
      {error ? <p className="mb-1 text-xs text-warn">{error}</p> : null}
      {(names ?? []).map((name) => {
        const unit = units[name] ?? name.toUpperCase();
        const data = perBot[name] ?? [];
        const last = data.length ? data[data.length - 1] : null;
        return (
          <div key={name} className="mb-2">
            <div className="flex items-baseline justify-between text-xs">
              <span className="font-semibold text-white">{unit}</span>
              {last ? (
                <span className="text-muted">
                  total <span className={last.total >= 0 ? "text-profit" : "text-loss"}>{formatCoin(last.total, unit, 4)}</span> · realized{" "}
                  {formatCoin(last.realised, unit, 4)}
                </span>
              ) : null}
            </div>
            {data.length < 2 ? (
              <p className="py-4 text-center text-sm text-muted">{loading ? "Loading…" : "No history for this range yet."}</p>
            ) : (
              <div className="h-40" aria-label={`P&L in ${unit}, range ${range}`}>
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={data} margin={{ top: 8, right: 12, bottom: 0, left: 8 }}>
                    <XAxis dataKey="t" tickFormatter={(t) => tickLabel(t, range)} stroke="#94a3b8" fontSize={11} minTickGap={50} />
                    <YAxis tickFormatter={(v) => formatCoin(v, "", 3)} stroke="#94a3b8" fontSize={11} width={70} />
                    <ReferenceLine y={0} stroke="#475569" />
                    <Tooltip
                      contentStyle={{ background: "#111a2e", border: "1px solid #1e293b" }}
                      labelFormatter={(t) => formatDateTime(t)}
                      formatter={(v, key) => [formatCoin(v, unit, 5), coinSeriesName(key)]}
                    />
                    <Line type="monotone" dataKey="total" stroke="#38bdf8" dot={false} strokeWidth={2} isAnimationActive={false} />
                    <Line type="monotone" dataKey="realised" stroke="#22c55e" dot={false} strokeWidth={1.5} isAnimationActive={false} />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
