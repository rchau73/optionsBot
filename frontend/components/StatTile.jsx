import clsx from "clsx";

/** A labelled number. tone: "good" | "bad" | undefined. */
export default function StatTile({ label, value, sub, tone }) {
  return (
    <div className="rounded-lg border border-line bg-panel px-3 py-2">
      <div className="text-xs uppercase tracking-wide text-muted">{label}</div>
      <div className={clsx("mt-0.5 text-lg font-semibold tabular-nums", tone === "good" && "text-profit", tone === "bad" && "text-loss")}>
        {value}
      </div>
      {sub ? <div className="text-xs text-muted">{sub}</div> : null}
    </div>
  );
}
