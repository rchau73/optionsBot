"use client";

import clsx from "clsx";
import { Area, ComposedChart, Line, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { usePnlHistory } from "@/hooks/usePnlHistory";
import { useStoredChoice } from "@/hooks/useStoredChoice";
import { formatDateTime } from "@/lib/format";
import { formatSignedUSD } from "@/lib/summary";
import { fmtCoin } from "@/lib/tiles";
import { track } from "@/lib/track";
import { tickLabel } from "./PnlChart";

// The overview's P&L chart (#404): Coin (default, one small chart per bot —
// coins are never summed) | USD (one chart for all bots); range 1d | 1w | 1m,
// 1w by default. Total is a solid line over a light area, realised a dashed
// grey line, with a zero line. Each chart's header: "now … · realised …".

export const UNITS = [
  ["coin", "Coin"],
  ["usd", "USD"],
];
export const OVERVIEW_RANGES = [
  ["1d", "1d"],
  ["1w", "1w"],
  ["1m", "1m"],
];
export const UNIT_KEY = "monitor.overview_pnl.unit";
export const OVERVIEW_RANGE_KEY = "monitor.overview_pnl.range";

// The design tokens (Recharts takes colours, not classes): data blue, muted grey.
const C = { total: "#38bdf8", realised: "#94a3b8", zero: "#475569", axis: "#94a3b8" };

/** A row of toggle buttons (aria-pressed): one choice of options; name is the group's accessible name. */
function Switch({ name, options, value, onChange }) {
  return (
    <div role="group" aria-label={name} className="flex overflow-hidden rounded-md border border-line">
      {options.map(([key, text]) => (
        <button
          key={key}
          type="button"
          aria-pressed={value === key}
          onClick={() => onChange(key)}
          className={clsx(
            "min-h-11 min-w-11 px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
            value === key ? "bg-testnet/20 font-semibold text-testnet" : "text-muted hover:text-white",
          )}
        >
          {text}
        </button>
      ))}
    </div>
  );
}

/** One chart: header "BTC · now … · realised …", then the lines (or why there are none). */
function SmallChart({ title, data, fmt, range, loading }) {
  const last = data.length ? data[data.length - 1] : null;
  const summary = last ? `now ${fmt(last.total)} · realised ${fmt(last.realised)}` : null;
  return (
    <figure className="min-w-0 space-y-1">
      <figcaption className="flex flex-wrap items-baseline justify-between gap-2 text-sm">
        <span className="font-semibold">{title}</span>
        {summary ? <span className="tabular-nums text-muted">{summary}</span> : null}
      </figcaption>
      {data.length < 2 ? (
        <p className="py-6 text-center text-sm text-muted">{loading ? "Loading…" : "No history for this range yet."}</p>
      ) : (
        <div role="img" aria-label={`P&L ${title}, range ${range}: ${summary}`} className="h-40 w-full">
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart data={data} margin={{ top: 6, right: 8, bottom: 0, left: 4 }}>
              <XAxis dataKey="t" tickFormatter={(t) => tickLabel(t, range)} stroke={C.axis} fontSize={11} minTickGap={50} />
              <YAxis tickFormatter={(v) => fmt(v).replace(/ [A-Z]+$/, "")} stroke={C.axis} fontSize={11} width={72} />
              <ReferenceLine y={0} stroke={C.zero} />
              <Tooltip
                contentStyle={{ background: "#111a2e", border: "1px solid #1e293b" }}
                labelFormatter={(t) => formatDateTime(t)}
                formatter={(v, key) => [fmt(v), key === "realised" ? "Realised" : "Total"]}
              />
              <Area type="monotone" dataKey="total" stroke={C.total} fill={C.total} fillOpacity={0.12} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="realised" stroke={C.realised} strokeDasharray="5 3" dot={false} strokeWidth={1.5} isAnimationActive={false} />
            </ComposedChart>
          </ResponsiveContainer>
        </div>
      )}
    </figure>
  );
}

/** names: the bots' names; units: bot → its coin (BTC, ETH). */
export default function OverviewPnl({ names, units = {} }) {
  const [unit, setUnit] = useStoredChoice(UNIT_KEY, "coin", ["coin", "usd"]);
  const [range, setRange] = useStoredChoice(OVERVIEW_RANGE_KEY, "1w", OVERVIEW_RANGES.map(([k]) => k));
  const { points, perBot, error, loading } = usePnlHistory(names, range);
  const pickUnit = (u) => {
    if (u !== unit) track("overview_unit_toggle", { unit: u });
    setUnit(u);
  };
  const usd = points.map((p) => ({ t: p.t, total: p.totalUsd, realised: p.realisedUsd }));

  return (
    <section aria-label="P&L" className="space-y-2 rounded-lg border border-line bg-panel/40 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="mr-auto text-sm font-semibold uppercase tracking-wide text-muted">P&amp;L, net of fees</h2>
        <Switch name="P&L unit" options={UNITS} value={unit} onChange={pickUnit} />
        <Switch name="P&L range" options={OVERVIEW_RANGES} value={range} onChange={setRange} />
      </div>
      {error ? <p className="text-xs text-warn">{error}</p> : null}
      {unit === "usd" ? (
        <SmallChart title="All bots in USD" data={usd} fmt={formatSignedUSD} range={range} loading={loading} />
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {(names ?? []).map((name) => {
            const coin = units[name] ?? name.toUpperCase();
            return <SmallChart key={name} title={coin} data={perBot[name] ?? []} fmt={(v) => fmtCoin(v, coin)} range={range} loading={loading} />;
          })}
        </div>
      )}
      <p className="text-xs text-muted">
        Solid: total (realised + open). Dashed: realised. {unit === "coin" ? "In each bot's coin: the coin's own price moves are left out." : "In USD at each moment's price: the coin's price moves are in it."}
      </p>
    </section>
  );
}
