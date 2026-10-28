"use client";

import clsx from "clsx";
import { useState } from "react";
import { BAND_LAYERS, bandChart, bandChartText } from "@/lib/band";
import { track } from "@/lib/track";

// The bot band's price chart (#404): 60 daily candles — up green, down neutral
// grey (#64748b), so red always means a loss — the corridor between the lowest
// and the highest sold strike shaded green, the flip with its shed band, SMA9
// and the dashed sold strikes, each labelled on the right edge (nudged apart,
// never overlapping), and the spot. SMA9, the flip band and the sold strikes
// toggle from pills, all on by default.

const LABEL = { put: "fill-put", call: "fill-call", flip: "fill-testnet", sma: "fill-warn", spot: "fill-slate-100" };

/** status: the bot's /api/status; payoff: its /api/strangles/payoff; coin: BTC, ETH. */
export default function BandChart({ status, payoff, coin }) {
  const [layers, setLayers] = useState({ sma: true, flip: true, strikes: true });
  const g = bandChart(status, payoff, layers);
  const toggle = (key) => {
    track("overview_layer_toggle", { layer: key });
    setLayers((l) => ({ ...l, [key]: !l[key] }));
  };
  const clip = `band-clip-${coin}`;

  return (
    <figure className="min-w-0 space-y-2" data-testid={`band-chart-${coin}`}>
      {!g ? (
        <p className="py-6 text-center text-sm text-muted">Candles unknown: the bot has not reported its daily price history yet.</p>
      ) : (
        <svg role="img" aria-label={bandChartText(coin, status, payoff)} viewBox={`0 0 ${g.box.width} ${g.box.height}`} className="h-auto max-h-60 w-full">
          <defs>
            <clipPath id={clip}>
              <rect x={g.box.left} y={g.box.top} width={g.box.plotRight - g.box.left} height={g.box.plotBottom - g.box.top} />
            </clipPath>
          </defs>
          <g clipPath={`url(#${clip})`}>
            {g.corridor ? (
              <rect data-testid="strike-corridor" x={g.box.left} y={g.corridor.y1} width={g.box.plotRight - g.box.left} height={g.corridor.y2 - g.corridor.y1} className="fill-profit" fillOpacity={0.06} />
            ) : null}
            {g.flip?.bandTop != null && g.flip?.bandBottom != null ? (
              <rect data-testid="flip-band" x={g.box.left} y={g.flip.bandTop} width={g.box.plotRight - g.box.left} height={Math.max(g.flip.bandBottom - g.flip.bandTop, 0)} className="fill-testnet" fillOpacity={0.1} />
            ) : null}
            {g.flip?.y != null ? <line x1={g.box.left} x2={g.box.plotRight} y1={g.flip.y} y2={g.flip.y} className="stroke-testnet" strokeWidth={1.5} /> : null}
            {g.strikes.map((s) => (
              <line key={`${s.side}-${s.price}`} data-testid="sold-strike" x1={g.box.left} x2={g.box.plotRight} y1={s.y} y2={s.y} className={s.side === "put" ? "stroke-put" : "stroke-call"} strokeDasharray="4 4" />
            ))}
            {g.candles.map((c) => (
              // Down candles are slate-500 (#64748b), never red: red is a loss.
              <g key={c.date} data-testid="candle" data-up={c.up} className={c.up ? "fill-profit stroke-profit" : "fill-slate-500 stroke-slate-500"}>
                <line x1={c.x} x2={c.x} y1={c.high} y2={c.low} />
                <rect x={c.x - c.w / 2} y={c.bodyTop} width={c.w} height={c.bodyHeight} strokeWidth={0} />
              </g>
            ))}
            {g.sma ? <polyline data-testid="sma9" points={g.sma} fill="none" className="stroke-warn" strokeWidth={1.5} /> : null}
            {g.spot ? <circle cx={g.spot.x} cy={g.spot.y} r={4} className="fill-slate-100 stroke-panel" /> : null}
          </g>
          {g.labels.map((l) => (
            <text key={l.key} x={g.box.plotRight + 6} y={l.labelY} dominantBaseline="middle" fontSize={11} className={clsx("tabular-nums", LABEL[l.kind])}>
              {l.text}
            </text>
          ))}
          <text x={g.box.left} y={g.box.height - 6} fontSize={11} className="fill-muted">
            {g.first.slice(5)}
          </text>
          <text x={g.box.plotRight} y={g.box.height - 6} fontSize={11} textAnchor="end" className="fill-muted">
            {g.last.slice(5)}
          </text>
        </svg>
      )}
      <div role="group" aria-label={`${coin} chart layers`} className="flex flex-wrap gap-1">
        {BAND_LAYERS.map((l) => (
          <button
            key={l.key}
            type="button"
            aria-pressed={layers[l.key]}
            onClick={() => toggle(l.key)}
            className={clsx(
              "inline-flex min-h-11 items-center rounded-full border px-3 text-xs focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
              layers[l.key] ? "border-slate-500 text-slate-200" : "border-line text-muted line-through",
            )}
          >
            {l.label}
          </button>
        ))}
      </div>
    </figure>
  );
}
