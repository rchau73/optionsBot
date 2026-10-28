import clsx from "clsx";

/** At most this many dots; a longer record is scaled to it. */
export const MAX_DOTS = 20;

/** The dots of a strip: one per close up to MAX_DOTS, else scaled; won dots first. */
export function dotsOf(wins, total) {
  const n = Math.max(0, Math.floor(total ?? 0));
  const w = Math.min(n, Math.max(0, Math.floor(wins ?? 0)));
  if (n <= MAX_DOTS) return { won: w, lost: n - w };
  const won = Math.round((w / n) * MAX_DOTS);
  return { won, lost: MAX_DOTS - won };
}

/** Wins and losses as a strip of dots (Details (pro), #405), named "5 of 9 won" for screen readers. */
export default function DotStrip({ wins, total }) {
  const { won, lost } = dotsOf(wins, total);
  const label = `${Math.min(wins ?? 0, total ?? 0)} of ${total ?? 0} won`;
  return (
    <span role="img" aria-label={label} title={label} className="inline-flex flex-wrap items-center gap-0.5 align-middle">
      {Array.from({ length: won + lost }, (_, i) => (
        <span key={i} data-won={i < won ? "1" : "0"} className={clsx("inline-block h-1.5 w-1.5 rounded-full", i < won ? "bg-profit" : "bg-loss")} />
      ))}
    </span>
  );
}
