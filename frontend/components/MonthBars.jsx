import clsx from "clsx";
import { monthBars } from "@/lib/overview";
import { toneOf } from "@/lib/summary";

/**
 * Six month bars of realised income, oldest first (#320; the overview's
 * "This month" tile, #394): a loss bar is hatched, the label names the months
 * down, so colour is never the only signal. Nothing without months.
 */
export default function MonthBars({ income, className = "h-12" }) {
  const { bars, label } = monthBars(income);
  if (!bars.length) return null;
  return (
    <div role="img" aria-label={label} data-testid="month-bars" className={clsx("flex items-end gap-1", className)}>
      {bars.map((b) => (
        <div key={b.month} className="flex h-full flex-1 flex-col items-center justify-end gap-0.5">
          <div
            data-loss={b.loss || undefined}
            className={clsx("w-full rounded-sm", b.loss ? "hatch-loss border border-loss" : toneOf(b.value) === "profit" ? "bg-profit" : "bg-slate-700")}
            style={{ height: `${Math.max(b.height, 2)}%` }}
          />
          <span className="text-[10px] leading-none text-muted">{b.label}</span>
        </div>
      ))}
    </div>
  );
}
