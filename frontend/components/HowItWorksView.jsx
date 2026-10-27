"use client";

import Link from "next/link";
import Badge from "./Badge";
import HowItWorks from "./HowItWorks";
import { useBotSummary } from "@/hooks/useBotSummary";

/** The "How it works" page's shell (#322): header nav, environment badge, the page, the footer. */
export default function HowItWorksView({ bot }) {
  const { names, summary, error } = useBotSummary(bot);
  return <HowItWorksScreen bot={bot} names={names} summary={summary} error={error} />;
}

const linkClass =
  "inline-flex min-h-11 items-center rounded px-2 text-sm text-muted hover:text-white focus-visible:outline focus-visible:outline-testnet";

/** Pure render of the page, for tests: no polling. */
export function HowItWorksScreen({ bot, names, summary, error }) {
  const env = summary?.environment;
  return (
    <div className="min-h-screen">
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
        <nav aria-label="Views" className="flex flex-wrap items-center gap-1">
          <Link href={bot ? `/?bot=${encodeURIComponent(bot)}` : "/"} className={linkClass}>
            ← Overview
          </Link>
          <span aria-hidden="true" className="text-muted">
            ·
          </span>
          <span aria-current="page" className="inline-flex min-h-11 items-center px-2 text-sm font-semibold">
            How it works
          </span>
        </nav>
        {bot ? <span className="text-lg font-bold">{bot.toUpperCase()}</span> : null}
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
            <HowItWorks summary={summary} />
          </>
        ) : error ? (
          <p role="alert" className="text-base text-warn">
            Unknown: {error}. Nothing is shown until the bot answers again.
          </p>
        ) : (
          <p className="text-base text-muted">Not yet: loading the bot&apos;s strangle…</p>
        )}
      </main>
      <footer className="px-4 pb-6 text-center text-xs text-muted">Read-only · educational, not investment advice</footer>
    </div>
  );
}

function BotPicker({ names }) {
  return (
    <div className="space-y-2">
      <p className="text-base">Choose a bot to see how it works with its own numbers.</p>
      <ul className="flex flex-wrap gap-2">
        {(names ?? []).map((n) => (
          <li key={n}>
            <Link
              href={`/how-it-works?bot=${encodeURIComponent(n)}`}
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
