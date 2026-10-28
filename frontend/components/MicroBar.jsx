import clsx from "clsx";
import { isNumber } from "@/lib/format";

const FILL = {
  profit: "bg-profit",
  loss: "bg-loss",
  warn: "bg-warn",
  neutral: "bg-slate-400",
};

/**
 * A small inline bar beside its number (Details (pro), #405). The value is
 * clamped to [min, max]. With `zero` the bar diverges from it (e.g. premium
 * kept, −100..+100 %); otherwise it fills from min. `tick` draws an amber
 * mark at that value (e.g. the delta exit). Hidden from screen readers: the
 * number next to it carries the meaning. width in px (64 by default).
 */
export default function MicroBar({ value, min = 0, max = 1, zero = null, tick = null, tone = "neutral", width = 64 }) {
  const span = max - min;
  const at = (v) => (span > 0 ? ((Math.min(max, Math.max(min, v)) - min) / span) * 100 : 0);
  const known = isNumber(value);
  const from = isNumber(zero) ? at(zero) : 0;
  const to = known ? at(value) : from;
  return (
    <span
      aria-hidden="true"
      data-testid="micro-bar"
      data-tone={tone}
      className="relative inline-block h-2 shrink-0 rounded-sm bg-slate-800 align-middle"
      style={{ width }}
    >
      {known ? (
        <span
          data-testid="micro-bar-fill"
          className={clsx("absolute inset-y-0 rounded-sm", FILL[tone] ?? FILL.neutral)}
          style={{ left: `${Math.min(from, to)}%`, width: `${Math.abs(to - from)}%` }}
        />
      ) : null}
      {isNumber(zero) ? <span className="absolute -inset-y-0.5 w-px bg-slate-500" style={{ left: `${from}%` }} /> : null}
      {isNumber(tick) ? <span data-testid="micro-bar-tick" className="absolute -inset-y-0.5 w-0.5 bg-warn" style={{ left: `calc(${at(tick)}% - 1px)` }} /> : null}
    </span>
  );
}
