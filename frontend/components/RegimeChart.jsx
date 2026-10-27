"use client";

import clsx from "clsx";
import { useMemo, useState } from "react";
import { Bar, ComposedChart, Line, ReferenceArea, ReferenceDot, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { formatUSD } from "@/lib/format";
import { candleRows, regimeLevels, regimeText, strikeLines, yDomain } from "@/lib/regime";

// The overview's regime chart (#394): the coin's daily candles (last 60 days
// and today's live one) with SMA9 and SMA21, the last swing high and low as
// walls, the GEX flip and its shed band (flip × (1 ± flip_buffer_pct)), the
// median flip of the bias window under gex_direction bias, the held strikes
// and the last price. Every line kind toggles from the legend; the wrapper's
// aria-label is the text alternative.

// The design tokens of app/globals.css (Recharts takes colours, not classes).
const C = {
  up: "#22c55e", // profit
  down: "#f43f5e", // loss
  axis: "#94a3b8", // muted
  sma9: "#f59e0b", // warn
  sma21: "#818cf8", // action
  wall: "#e2e8f0",
  flip: "#38bdf8", // testnet
  put: "#c084fc",
  call: "#60a5fa",
  panel: "#111a2e",
  line: "#1e293b",
};

/** Legend items: key, label, and the mark drawn beside it. */
export const LAYERS = [
  { key: "sma9", label: "SMA9", color: C.sma9 },
  { key: "sma21", label: "SMA21", color: C.sma21, dashed: true },
  { key: "swings", label: "Swing walls", color: C.wall },
  { key: "flip", label: "Flip ± shed band", color: C.flip },
  { key: "median", label: "Median flip (bias)", color: C.flip, dashed: true },
  { key: "strikes", label: "Held strikes", color: C.put },
];

/** One candle: the wick from low to high, the body from open to close; a falling day is hollow (not colour alone). */
function CandleShape({ x, y, width, height, payload }) {
  if (!payload || !Number.isFinite(y) || !Number.isFinite(height)) return null;
  const { open, close, high, low, up } = payload;
  const span = high - low;
  const yOf = (v) => (span > 0 ? y + ((high - v) / span) * height : y + height / 2);
  const top = yOf(Math.max(open, close));
  const bottom = yOf(Math.min(open, close));
  const cx = x + width / 2;
  const color = up ? C.up : C.down;
  const w = Math.max(width * 0.7, 1);
  return (
    <g>
      <line x1={cx} x2={cx} y1={y} y2={y + height} stroke={color} strokeWidth={1} />
      <rect x={cx - w / 2} y={top} width={w} height={Math.max(bottom - top, 1)} fill={up ? color : C.panel} stroke={color} strokeWidth={1} />
    </g>
  );
}

function tooltipRow(row) {
  if (!row) return null;
  return (
    <div className="rounded border border-line bg-panel px-2 py-1 text-xs tabular-nums">
      <div className="font-semibold">{row.date}</div>
      <div>
        O {formatUSD(row.open)} · H {formatUSD(row.high)} · L {formatUSD(row.low)} · C {formatUSD(row.close)}
      </div>
      {row.sma9 ? <div>SMA9 {formatUSD(row.sma9)}</div> : null}
      {row.sma21 ? <div>SMA21 {formatUSD(row.sma21)}</div> : null}
    </div>
  );
}

const label = (value, fill, position = "insideTopLeft") => ({ value, fill, fontSize: 11, position });

/** status: the bot's /api/status; positions: its /api/positions (the held strikes); coin: BTC, ETH. */
export default function RegimeChart({ status, positions, coin }) {
  const [hidden, setHidden] = useState(() => new Set());
  const rows = useMemo(() => candleRows(status), [status]);
  const levels = regimeLevels(status);
  const strikes = strikeLines(positions);
  const text = regimeText(status, coin);
  const on = (key) => !hidden.has(key);
  const toggle = (key) =>
    setHidden((h) => {
      const next = new Set(h);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const shown = [
    levels.spot,
    on("sma9") ? levels.sma9 : null,
    on("sma21") ? levels.sma21 : null,
    ...(on("swings") ? [levels.swingHigh?.price, levels.swingLow?.price] : []),
    ...(on("flip") && levels.flip ? [levels.flip.price, levels.flip.low, levels.flip.high] : []),
    on("median") ? levels.medianFlip : null,
    ...(on("strikes") ? strikes.map((s) => s.price) : []),
  ];
  const domain = yDomain(rows, shown);
  const last = rows.length ? rows[rows.length - 1] : null;
  const available = LAYERS.filter((l) => l.key !== "median" || levels.medianFlip);

  return (
    <figure className="space-y-2" data-testid={`regime-chart-${coin}`}>
      {rows.length < 2 ? (
        <p className="py-6 text-center text-sm text-muted">
          Candles unknown: {rows.length ? "only today's candle so far" : "the bot has not reported its daily price history (an older bot, or history still loading)"}.
        </p>
      ) : (
        <div role="img" aria-label={text} className="h-[clamp(16rem,36vh,26rem)] w-full">
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart data={rows} margin={{ top: 8, right: 8, bottom: 0, left: 4 }}>
              <XAxis dataKey="date" stroke={C.axis} fontSize={11} minTickGap={40} tickFormatter={(d) => d.slice(5)} />
              <YAxis domain={domain ?? ["auto", "auto"]} stroke={C.axis} fontSize={11} width={64} tickFormatter={(v) => formatUSD(v)} allowDataOverflow />
              <Tooltip content={({ active, payload }) => (active ? tooltipRow(payload?.[0]?.payload) : null)} />
              {on("flip") && levels.flip?.low && levels.flip?.high ? (
                <ReferenceArea y1={levels.flip.low} y2={levels.flip.high} fill={C.flip} fillOpacity={0.12} stroke="none" ifOverflow="hidden" />
              ) : null}
              {on("flip") && levels.flip ? (
                <ReferenceLine y={levels.flip.price} stroke={C.flip} strokeWidth={1.5} label={label(`flip ${formatUSD(levels.flip.price)}`, C.flip)} ifOverflow="hidden" />
              ) : null}
              {on("median") && levels.medianFlip ? (
                <ReferenceLine y={levels.medianFlip} stroke={C.flip} strokeDasharray="6 4" label={label("median flip", C.flip, "insideBottomLeft")} ifOverflow="hidden" />
              ) : null}
              {on("swings") && levels.swingHigh ? (
                <ReferenceLine y={levels.swingHigh.price} stroke={C.wall} strokeWidth={2} label={label(levels.swingHigh.label, C.wall, "insideTopRight")} ifOverflow="hidden" />
              ) : null}
              {on("swings") && levels.swingLow ? (
                <ReferenceLine y={levels.swingLow.price} stroke={C.wall} strokeWidth={2} label={label(levels.swingLow.label, C.wall, "insideBottomRight")} ifOverflow="hidden" />
              ) : null}
              {on("strikes")
                ? strikes.map((s) => (
                    <ReferenceLine
                      key={`${s.side}:${s.price}`}
                      y={s.price}
                      stroke={s.side === "put" ? C.put : C.call}
                      strokeOpacity={0.6}
                      strokeDasharray="2 4"
                      label={label(s.label, s.side === "put" ? C.put : C.call, "insideLeft")}
                      ifOverflow="hidden"
                    />
                  ))
                : null}
              <Bar dataKey="range" shape={CandleShape} isAnimationActive={false} />
              {on("sma9") ? <Line dataKey="sma9" stroke={C.sma9} dot={false} strokeWidth={1.5} isAnimationActive={false} connectNulls /> : null}
              {on("sma21") ? (
                <Line dataKey="sma21" stroke={C.sma21} strokeDasharray="5 3" dot={false} strokeWidth={1.5} isAnimationActive={false} connectNulls />
              ) : null}
              {last && levels.spot ? <ReferenceDot x={last.date} y={levels.spot} r={4} fill="#ffffff" stroke={C.panel} label={label(`now ${formatUSD(levels.spot)}`, "#ffffff", "left")} /> : null}
            </ComposedChart>
          </ResponsiveContainer>
        </div>
      )}
      <figcaption className="sr-only">{text}</figcaption>
      <div role="group" aria-label={`${coin} chart layers`} className="flex flex-wrap gap-1">
        {available.map((l) => (
          <button
            key={l.key}
            type="button"
            aria-pressed={on(l.key)}
            onClick={() => toggle(l.key)}
            className={clsx(
              "inline-flex min-h-11 items-center gap-2 rounded-full border px-3 text-xs focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
              on(l.key) ? "border-slate-500 text-slate-200" : "border-line text-muted line-through",
            )}
          >
            <span aria-hidden="true" className="inline-block w-4 border-t-2" style={{ borderColor: l.color, borderTopStyle: l.dashed ? "dashed" : "solid" }} />
            {l.label}
          </button>
        ))}
      </div>
    </figure>
  );
}
