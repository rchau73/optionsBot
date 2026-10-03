import clsx from "clsx";
import { marginLevel } from "@/lib/account";
import { formatPct } from "@/lib/format";

const COLORS = { ok: "bg-profit", warn: "bg-warn", danger: "bg-loss", unknown: "bg-slate-600" };

/**
 * Horizontal bar for a margin usage %, coloured by risk level. markers are
 * limits drawn as ticks on the bar: [{ pct, label }].
 */
export default function MarginBar({ label, pct, markers = [] }) {
  const level = marginLevel(pct);
  const width = Math.min(Math.max(pct ?? 0, 0), 100);
  return (
    <div className="min-w-40">
      <div className="flex justify-between text-xs">
        <span className="text-muted">{label}</span>
        <span className={clsx("tabular-nums", level === "danger" && "text-loss", level === "warn" && "text-warn")}>{formatPct(pct, 1)}</span>
      </div>
      <div className="relative mt-1 h-2 rounded bg-slate-800" role="meter" aria-label={label} aria-valuenow={pct ?? 0} aria-valuemin={0} aria-valuemax={100}>
        <div className={clsx("h-2 rounded", COLORS[level])} style={{ width: `${width}%` }} />
        {markers.map((m) => (
          <span
            key={m.label}
            data-testid="limit-marker"
            title={m.label}
            className="absolute -top-1 h-4 w-0.5 bg-slate-200"
            style={{ left: `${Math.min(Math.max(m.pct, 0), 100)}%` }}
          />
        ))}
      </div>
    </div>
  );
}
