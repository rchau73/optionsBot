"use client";

import clsx from "clsx";
import { useState } from "react";
import PayoffCard from "./PayoffCard";
import { usePayoff } from "@/hooks/usePayoff";
import { payoffStrangles } from "@/lib/payoff";

/**
 * The Positions page (#391): /positions?bot=<name>. One payoff card per
 * strangle of the chosen coin, ordered by DTE, with a coin switch as on the
 * overview (it rewrites ?bot=, the menu follows). Without ?bot= the first
 * configured bot.
 */
export default function PositionsPage({ bot = null }) {
  const [chosen, setChosen] = useState(bot);
  const { names, payoff, error } = usePayoff(chosen ?? null);
  const current = chosen ?? names?.[0] ?? null;
  // The first bot is known only once the monitor answers; poll it from then on.
  if (!chosen && current) setChosen(current);

  const select = (name) => {
    setChosen(name);
    const url = new URL(window.location.href);
    url.searchParams.set("bot", name);
    window.history.replaceState(null, "", url);
  };
  return <PositionsScreen names={names} bot={current} payoff={payoff} error={error} onSelect={select} />;
}

/** Pure render of the page, for tests: no polling. */
export function PositionsScreen({ names, bot, payoff, error, onSelect }) {
  const list = payoffStrangles(payoff);
  const coin = payoff?.underlying || bot?.toUpperCase() || "";
  return (
    <main className="mx-auto max-w-5xl space-y-4 p-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-bold">Positions{coin ? ` · ${coin}` : ""}</h1>
        {names?.length > 1 ? (
          <div role="group" aria-label="Coin" className="flex overflow-hidden rounded-full border border-line">
            {names.map((n) => (
              <button
                key={n}
                type="button"
                aria-pressed={n === bot}
                onClick={() => onSelect?.(n)}
                className={clsx(
                  "min-h-11 min-w-11 px-3 text-sm font-semibold focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
                  n === bot ? "bg-testnet/20 text-testnet" : "text-muted",
                )}
              >
                {n.toUpperCase()}
              </button>
            ))}
          </div>
        ) : null}
      </header>
      <p className="text-base text-muted">
        One chart per strangle: what it pays at expiry (dashed), what it would give if bought back now (solid, filled: gain solid, loss
        hatched), where the price is now, and the lines where the bot acts.
      </p>
      {error && payoff ? (
        <p role="alert" className="text-base text-warn">
          Not updating: {error}. The charts below are from the last answer.
        </p>
      ) : null}
      {!payoff ? (
        error ? (
          <p role="alert" className="text-base text-warn">
            Unknown: {error}. Nothing is shown until the bot answers again.
          </p>
        ) : (
          <p className="text-base text-muted">Not yet: loading the bot&apos;s strangles…</p>
        )
      ) : !list.length ? (
        <p className="text-base">Nothing open right now. A chart appears here for each strangle once the bot sells one.</p>
      ) : (
        <div className="space-y-4">
          {list.map((sp) => (
            <PayoffCard key={sp.strangle_id} sp={sp} report={payoff} headingLevel="h2" />
          ))}
        </div>
      )}
      <p className="text-center text-xs text-muted">Read-only · a model, not a quote · educational, not investment advice</p>
    </main>
  );
}
