import clsx from "clsx";

/**
 * A labelled number. tone: "good" | "bad" | undefined. children (optional)
 * replace the single value, e.g. one line per bot (#404); footer is a last
 * muted line; region makes the tile a named landmark (aria-label = label).
 */
export default function StatTile({ label, value, sub, tone, children, footer, region = false }) {
  const Box = region ? "section" : "div";
  return (
    <Box aria-label={region ? label : undefined} className="flex flex-col rounded-lg border border-line bg-panel px-3 py-2">
      <div className="text-xs uppercase tracking-wide text-muted">{label}</div>
      {value != null ? (
        <div className={clsx("mt-0.5 text-lg font-semibold tabular-nums", tone === "good" && "text-profit", tone === "bad" && "text-loss")}>{value}</div>
      ) : null}
      {sub ? <div className="text-xs text-muted">{sub}</div> : null}
      {children}
      {footer ? <div className="mt-auto pt-1 text-xs text-muted tabular-nums">{footer}</div> : null}
    </Box>
  );
}
