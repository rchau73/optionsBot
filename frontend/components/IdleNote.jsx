"use client";

import clsx from "clsx";
import Link from "next/link";
import { useId, useState } from "react";
import { formatDateTime } from "@/lib/format";
import { idleLine } from "@/lib/idleWords";

// Why the bot is not trading (#781): under the positions answer, one plain
// sentence from /api/summary's `idle` (the journal's latest skip or freeze),
// whether a person must act, and a one-click Why? with the journal line's own
// sentence and numbers. Nothing while the bot holds a strangle with entries
// allowed. A pause reads as plainly as a result, and never as a promise.

const LINK =
  "inline-flex min-h-11 items-center text-sm text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** bot: one bot of useMonitor (its summary's idle). */
export default function IdleNote({ bot }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const idle = bot?.summary?.idle;
  const line = idleLine(idle);
  if (!line) return null;
  const why = idle.seq > 0 || idle.detail;
  return (
    <section aria-label="Why it is not trading" className="space-y-1" data-testid={`idle-note-${bot.name}`}>
      <p className={clsx("break-words text-base font-semibold", line.blocked && "text-warn")}>
        {line.blocked ? "Not opening new positions. " : "Waiting. "}
        {line.text}
      </p>
      <div className="flex flex-wrap items-center gap-x-3">
        {line.act ? <p className="text-sm">{line.act}</p> : null}
        {why ? (
          <button
            type="button"
            aria-expanded={open}
            aria-controls={id}
            onClick={() => setOpen(!open)}
            className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-line px-2 text-xs text-testnet hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
          >
            Why?
          </button>
        ) : null}
        <Link href={`/activity?bot=${encodeURIComponent(bot.name)}`} className={LINK}>
          All actions, explained →
        </Link>
      </div>
      {why && open ? (
        <div id={id} className="break-words rounded-md border border-line bg-panel px-3 py-2" data-testid="idle-why">
          {idle.plain ? <p className="text-base">{idle.plain}</p> : null}
          {idle.detail ? <p className="text-xs text-muted">{idle.detail}</p> : null}
          {idle.at ? <p className="text-xs text-muted">Recorded in the journal {formatDateTime(idle.at)}</p> : null}
        </div>
      ) : null}
    </section>
  );
}
