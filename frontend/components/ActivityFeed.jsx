"use client";

import { useState } from "react";
import clsx from "clsx";
import Badge from "./Badge";
import Details from "./Details";
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

/**
 * Live list of what the bots did, newest first, each with its market context.
 * Inside a `fill` Panel the list takes the rest of the panel: on lg screens the
 * whole grid row (never less than 20rem or 70 % of the window), scrolling
 * inside it — absolute, so its events never make the row taller. Below lg the
 * panels stack and the list is bounded by the window height instead.
 *
 * Each item leads with the bot's plain-words sentence (#287); the technical
 * line (event, rule detail, market context) folds under "details", open at
 * first when detailsOpen (managers and admins). An older bot without `plain`
 * shows the technical line as before.
 */
export default function ActivityFeed({ feed, detailsOpen = false }) {
  const [kind, setKind] = useState("all");
  const lines = feed.filter(MATCH[kind]);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
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
        <div className="relative flex-1 lg:min-h-[max(20rem,70vh)]" data-testid="activity-box">
          <ol
            className="max-h-[max(20rem,60vh)] space-y-1 overflow-y-auto pr-1 lg:absolute lg:inset-0 lg:max-h-none"
            data-testid="activity-list"
          >
            {lines.map((e) => (
              <li key={e.id} className="rounded border border-line/60 px-2 py-1 text-sm">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="tabular-nums text-xs text-muted">{formatTime(e.at)}</span>
                  <span className="text-xs font-semibold uppercase">{e.bot}</span>
                  <Badge tone={e.tone}>{e.label}</Badge>
                  <span>{e.plain ?? e.text}</span>
                </div>
                {e.plain ? (
                  <Details defaultOpen={detailsOpen}>
                    <div>{e.text}</div>
                    {e.context.length ? <div>{e.context.join(" · ")}</div> : null}
                  </Details>
                ) : e.context.length ? (
                  <div className="mt-0.5 text-xs text-muted">{e.context.join(" · ")}</div>
                ) : null}
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
}
