"use client";

import Link from "next/link";
import Badge from "./Badge";
import WorstCaseReport from "./WorstCaseReport";
import { useBotSummary } from "@/hooks/useBotSummary";

/** The worst-case page's shell (#318): back link, bot name, environment badge, the report, the footer. */
export default function WorstCaseView({ bot }) {
  const { names, summary, error } = useBotSummary(bot);
  return <WorstCaseScreen bot={bot} names={names} summary={summary} error={error} />;
}

/** Pure render of the page, for tests: no polling. */
export function WorstCaseScreen({ bot, names, summary, error }) {
  const env = summary?.environment;
  return (
    <div className="min-h-screen">
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
        <Link
          href={bot ? `/?bot=${encodeURIComponent(bot)}` : "/"}
          className="inline-flex min-h-11 items-center rounded px-2 text-sm text-muted hover:text-white focus-visible:outline focus-visible:outline-testnet"
        >
          ← Overview
        </Link>
        <span className="text-lg font-bold">{bot ? `${bot.toUpperCase()} · Worst case right now` : "Worst case right now"}</span>
        {env ? <Badge tone={env === "live" ? "live" : "testnet"}>{env === "live" ? "LIVE" : env.toUpperCase()}</Badge> : null}
      </header>
      <main className="mx-auto max-w-4xl space-y-4 p-4">
        {!bot ? (
          <BotPicker names={names} />
        ) : summary ? (
          <>
            {error ? (
              <p role="alert" className="text-base text-warn">
                Not updating: {error}. The figures below are from the last answer.
              </p>
            ) : null}
            <WorstCaseReport summary={summary} />
          </>
        ) : error ? (
          <p role="alert" className="text-base text-warn">
            Unknown: {error}. Nothing is shown until the bot answers again.
          </p>
        ) : (
          <p className="text-base text-muted">Not yet: loading the estimate…</p>
        )}
      </main>
      <footer className="px-4 pb-6 text-center text-xs text-muted">Read-only · a model, not a quote · educational study, not investment advice</footer>
    </div>
  );
}

function BotPicker({ names }) {
  return (
    <div className="space-y-2">
      <p className="text-base">Choose a bot to see its worst case.</p>
      <ul className="flex flex-wrap gap-2">
        {(names ?? []).map((n) => (
          <li key={n}>
            <Link
              href={`/worst-case?bot=${encodeURIComponent(n)}`}
              className="inline-flex min-h-11 items-center rounded border border-line px-3 text-base hover:border-muted focus-visible:outline focus-visible:outline-testnet"
            >
              {n.toUpperCase()}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
