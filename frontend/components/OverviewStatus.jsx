import clsx from "clsx";
import { heldBackLines, statusLine } from "@/lib/tiles";

// The overview's first line (#404): a dot and "Both trading · updated 14:02",
// then one amber line per bot that holds something back, in plain words.

const DOT = { ok: "bg-profit", warn: "bg-warn", bad: "bg-loss" };

export default function OverviewStatus({ bots }) {
  const s = statusLine(bots);
  const held = heldBackLines(bots);
  return (
    <div className="space-y-1" data-testid="overview-status">
      <p className="flex items-center gap-2 text-base font-semibold" role="status">
        <span aria-hidden="true" className={clsx("inline-block h-2.5 w-2.5 rounded-full", DOT[s.tone])} />
        {s.text}
      </p>
      {held.map((h) => (
        <p key={h.bot} className="text-sm text-warn" data-testid={`held-back-${h.bot}`}>
          {h.text}
        </p>
      ))}
    </div>
  );
}
