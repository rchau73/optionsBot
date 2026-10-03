"use client";

import { useState } from "react";
import clsx from "clsx";
import Badge from "./Badge";
import { formatTime } from "@/lib/format";

const KINDS = [
  ["all", "All"],
  ["trades", "Opens & closes"],
  ["orders", "Orders"],
  ["skipped", "Skipped"],
];

const MATCH = {
  all: () => true,
  trades: (e) => e.event === "filled" || e.event === "closed",
  orders: (e) => ["submitted", "amended", "cancelled"].includes(e.event),
  skipped: (e) => e.event === "skipped",
};

/** Live list of what the bots did, newest first, each with its market context. */
export default function ActivityFeed({ feed }) {
  const [kind, setKind] = useState("all");
  const lines = feed.filter(MATCH[kind]);
  return (
    <div>
      <div className="mb-2 flex gap-1" role="tablist">
        {KINDS.map(([k, text]) => (
          <button
            key={k}
            role="tab"
            aria-selected={kind === k}
            onClick={() => setKind(k)}
            className={clsx(
              "rounded px-2 py-0.5 text-xs focus-visible:outline focus-visible:outline-testnet",
              kind === k ? "bg-slate-600 text-white" : "bg-slate-800 text-muted hover:text-white",
            )}
          >
            {text}
          </button>
        ))}
      </div>
      {lines.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted">Nothing yet — events appear here as the bots act.</p>
      ) : (
        <ol className="max-h-[520px] space-y-1 overflow-y-auto pr-1">
          {lines.map((e) => (
            <li key={e.id} className="rounded border border-line/60 px-2 py-1 text-sm">
              <div className="flex flex-wrap items-center gap-2">
                <span className="tabular-nums text-xs text-muted">{formatTime(e.at)}</span>
                <span className="text-xs font-semibold uppercase">{e.bot}</span>
                <Badge tone={e.tone}>{e.label}</Badge>
                <span>{e.text}</span>
              </div>
              {e.context.length ? <div className="mt-0.5 text-xs text-muted">{e.context.join(" · ")}</div> : null}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
