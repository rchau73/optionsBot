"use client";

import clsx from "clsx";
import { useId, useState } from "react";
import { LINE_KINDS, chartGeometry, chartLines, coin4, nowLabel, price0, textAlternative, todayAtSpot } from "@/lib/payoff";
import { formatSignedUSD } from "@/lib/summary";
import { isNumber } from "@/lib/format";

// One strangle's payoff (#391): the at-expiry curve (dashed), today's curve
// (solid, Black–Scholes on each leg's IV) with the area between it and zero
// filled — gain solid, loss hatched — the spot marker, and the strikes,
// break-evens, exit lines and stop levels, each kind toggled from the legend.
// The bot computes every number; this only draws them.

const LINE_CLASS = {
  strike: { put: "stroke-put", call: "stroke-call" },
  breakeven: "stroke-muted",
  exit: "stroke-warn",
  stop: "stroke-loss",
};
const LABEL_CLASS = {
  strike: { put: "fill-put", call: "fill-call" },
  breakeven: "fill-muted",
  exit: "fill-warn",
  stop: "fill-loss",
};
const DASH = { strike: undefined, breakeven: "2 3", exit: "6 3", stop: "1 2" };

const pick = (map, line) => (typeof map[line.kind] === "string" ? map[line.kind] : map[line.kind]?.[line.type]);

/**
 * sp: one strangle of /api/strangles/payoff; coin: its underlying.
 * compact: the overview's card (no legend, no line labels, shorter).
 */
export default function PayoffChart({ sp, coin, compact = false }) {
  const id = useId().replace(/:/g, "");
  const [hidden, setHidden] = useState(() => new Set());
  const width = 640;
  const height = compact ? 220 : 300;
  const g = chartGeometry(sp, { width, height });
  if (!g) return <p className="text-base text-warn">Chart unknown: the bot sent no curve for this strangle.</p>;

  const lines = chartLines(sp).filter((l) => !hidden.has(l.kind) && l.price >= g.lo && l.price <= g.hi);
  const atSpot = todayAtSpot(sp);
  const spotIn = isNumber(sp.spot) && sp.spot >= g.lo && sp.spot <= g.hi;
  const sx = spotIn ? g.x(sp.spot) : null;
  const toggle = (kind) =>
    setHidden((h) => {
      const next = new Set(h);
      if (next.has(kind)) next.delete(kind);
      else next.add(kind);
      return next;
    });

  return (
    <figure className="space-y-2">
      <svg role="img" aria-label={textAlternative(sp, coin)} viewBox={`0 0 ${width} ${height}`} className="h-auto w-full" data-testid="payoff-chart">
        <defs>
          <pattern id={`${id}-hatch`} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <line x1="0" y1="0" x2="0" y2="6" className="stroke-loss" strokeWidth="2" />
          </pattern>
          <clipPath id={`${id}-above`}>
            <rect x="0" y="0" width={width} height={g.zero} />
          </clipPath>
          <clipPath id={`${id}-below`}>
            <rect x="0" y={g.zero} width={width} height={height - g.zero} />
          </clipPath>
        </defs>
        {g.area ? (
          <>
            <polygon points={g.area} className="fill-profit/30" clipPath={`url(#${id}-above)`} data-zone="gain" />
            <polygon points={g.area} fill={`url(#${id}-hatch)`} clipPath={`url(#${id}-below)`} data-zone="loss" />
          </>
        ) : null}
        <line x1={0} x2={width} y1={g.zero} y2={g.zero} className="stroke-muted" strokeWidth="1" />
        {lines.map((l) => (
          <g key={l.key} data-line={l.kind}>
            <line x1={g.x(l.price)} x2={g.x(l.price)} y1={g.top} y2={g.bottom} className={pick(LINE_CLASS, l)} strokeWidth="1.5" strokeDasharray={DASH[l.kind]} />
            {compact ? null : (
              <text x={g.x(l.price)} y={height - 8} textAnchor="middle" className={clsx("text-[11px]", pick(LABEL_CLASS, l))}>
                {l.label}
              </text>
            )}
          </g>
        ))}
        <polyline points={g.expiry} fill="none" className="stroke-slate-300" strokeWidth="2" strokeDasharray="6 4" data-curve="expiry" />
        {g.today ? <polyline points={g.today} fill="none" className="stroke-white" strokeWidth="2.5" data-curve="today" /> : null}
        {spotIn ? (
          <g data-marker="spot">
            <line x1={sx} x2={sx} y1={g.top - 6} y2={g.bottom} className="stroke-testnet" strokeWidth="1.5" />
            {atSpot != null ? <circle cx={sx} cy={g.y(atSpot)} r="5" className="fill-testnet stroke-panel" strokeWidth="2" /> : null}
            <text x={sx} y={g.top - 10} textAnchor={sx < width * 0.25 ? "start" : sx > width * 0.75 ? "end" : "middle"} className="fill-testnet text-[12px] font-semibold">
              {nowLabel(sp, coin)}
            </text>
          </g>
        ) : null}
      </svg>
      {sp.today ? null : (
        <p className="text-sm text-warn">Today&apos;s curve, exit lines and stop levels: {sp.today_reason || "unknown"}.</p>
      )}
      {compact ? null : <Legend hidden={hidden} onToggle={toggle} sp={sp} coin={coin} />}
    </figure>
  );
}

/** The curves' key and a toggle per kind of line (aria-pressed: shown). */
function Legend({ hidden, onToggle, sp, coin }) {
  return (
    <figcaption className="space-y-2 text-sm">
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-muted">
        <li>
          <span aria-hidden="true" className="mr-1 inline-block w-6 border-t-2 border-dashed border-slate-300 align-middle" />
          at expiry (keeps up to {coin4(sp.premium_coin, coin)})
        </li>
        <li>
          <span aria-hidden="true" className="mr-1 inline-block w-6 border-t-[3px] border-white align-middle" />
          bought back today
        </li>
        <li>
          <span aria-hidden="true" className="mr-1 inline-block h-3 w-4 bg-profit/30 align-middle" /> gain
        </li>
        <li>
          <span aria-hidden="true" className="hatch-loss mr-1 inline-block h-3 w-4 align-middle" /> loss (hatched)
        </li>
        <li>
          <span aria-hidden="true" className="mr-1 inline-block h-3 w-0.5 bg-testnet align-middle" /> now
          {isNumber(sp.unrealised_usd) ? ` (${formatSignedUSD(sp.unrealised_usd)})` : ""}
        </li>
      </ul>
      <div className="flex flex-wrap gap-1" role="group" aria-label="Lines shown">
        {LINE_KINDS.map(({ kind, label }) => (
          <button
            key={kind}
            type="button"
            aria-pressed={!hidden.has(kind)}
            onClick={() => onToggle(kind)}
            className={clsx(
              "inline-flex min-h-11 items-center rounded-full border px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
              hidden.has(kind) ? "border-line text-muted line-through" : "border-muted text-white",
            )}
          >
            {label}
          </button>
        ))}
      </div>
      <p className="text-xs text-muted">
        {chartLines(sp)
          .map((l) => l.label)
          .join(" · ") || "No lines: unknown"}
        {" · "}spot {price0(sp.spot)}
      </p>
    </figcaption>
  );
}
