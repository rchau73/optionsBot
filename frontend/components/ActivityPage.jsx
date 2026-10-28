"use client";

import clsx from "clsx";
import { useMemo, useState } from "react";
import { useActivity } from "@/hooks/useActivity";
import { ACTIVITY_FILTERS, DAYS_PER_PAGE, buildActivity, clockOf, filterActivity, groupByDay } from "@/lib/activity";
import { formatCoin, isNumber } from "@/lib/format";
import { toneOf } from "@/lib/summary";
import WhyButton from "./WhyButton";

// The Activity page (#385, E3 prototype screen 4): "What the bot did, and
// why" — the bot's journal day by day, each entry led by its plain-words
// sentence, with four filters.

/** The page for ?bot=<name>: loads the journal every 30 s. */
export default function ActivityPage({ bot }) {
  const { events, trades, error, loading } = useActivity(bot);
  return <ActivityView bot={bot} events={events} trades={trades} error={error} loading={loading} />;
}

/** The page's content for one state of the data (pure apart from the filter and the days shown). */
export function ActivityView({ bot, events = [], trades = [], error = null, loading = false, now }) {
  const coin = bot ? bot.toUpperCase() : "coin";
  const [filter, setFilter] = useState("all");
  const [days, setDays] = useState(DAYS_PER_PAGE);
  const entries = useMemo(() => buildActivity(events, trades, coin), [events, trades, coin]);
  const groups = groupByDay(filterActivity(entries, filter), now);
  const empty = entries.length === 0;

  return (
    <div className="min-h-screen">
      <header className="border-b border-line bg-panel px-4 py-3">
        <h1 className="text-lg font-bold">What the bot did, and why · {coin}</h1>
      </header>
      <main className="mx-auto max-w-3xl space-y-4 p-4">
        {!bot ? (
          <p role="alert" className="text-base text-warn">
            No bot chosen: open this page as /activity?bot=&lt;name&gt; from the menu.
          </p>
        ) : error && empty ? (
          <p role="alert" className="text-base text-warn">
            Unknown: {error}. Nothing is shown until the bot answers again.
          </p>
        ) : loading ? (
          <p className="text-base text-muted">Not yet: loading the bot&apos;s journal…</p>
        ) : (
          <>
            {error ? (
              <p role="status" className="text-sm text-warn">
                Not updating: {error}. The entries below are from the last answer.
              </p>
            ) : null}
            <FilterRow filter={filter} onFilter={setFilter} />
            {empty ? (
              <p className="text-base text-muted">Not yet: the bot has journaled nothing. Its first sale, wait or buy-back is explained here.</p>
            ) : groups.length === 0 ? (
              <p className="text-base text-muted">Nothing of this kind in the journal the bot keeps.</p>
            ) : (
              <>
                {groups.slice(0, days).map((g) => (
                  <DayGroup key={g.day} group={g} coin={coin} bot={bot} />
                ))}
                {groups.length > days ? (
                  <button
                    type="button"
                    onClick={() => setDays(days + DAYS_PER_PAGE)}
                    className="min-h-11 w-full rounded-md border border-line px-3 text-base text-testnet hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
                  >
                    Show earlier days
                  </button>
                ) : null}
              </>
            )}
          </>
        )}
        <footer className="space-y-1 text-xs text-muted">
          <p>Every line comes from the bot&apos;s journal. The raw entries, with all their numbers, are under Details (pro).</p>
          <p>Times in BRT (UTC−3). Educational study, not investment advice.</p>
        </footer>
      </main>
    </div>
  );
}

function FilterRow({ filter, onFilter }) {
  return (
    <div role="group" aria-label="Show" className="flex flex-wrap gap-2">
      {ACTIVITY_FILTERS.map(([key, label]) => (
        <button
          key={key}
          type="button"
          aria-pressed={filter === key}
          onClick={() => onFilter(key)}
          className={clsx(
            "min-h-11 rounded-full border px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
            filter === key ? "border-slate-700 bg-slate-700 font-semibold text-white" : "border-line text-muted hover:text-white",
          )}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

function DayGroup({ group, coin, bot }) {
  return (
    <section aria-label={group.title} className="rounded-lg border border-line bg-panel">
      <h2 className="border-b border-line px-4 py-2 text-sm font-semibold uppercase tracking-wide text-muted">{group.title}</h2>
      <ol className="divide-y divide-line/50">
        {group.entries.map((e) => (
          <Entry key={e.id} entry={e} coin={coin} bot={bot} />
        ))}
      </ol>
    </section>
  );
}

/** An open (filled) or close as a trade for the assistant's Why? (#408); null for other entries. */
const tradeOf = (e, bot) =>
  e.event === "filled" || e.event === "closed" ? { bot, seq: e.seq, kind: e.event === "filled" ? "open" : "close", instrument: e.instrument, at: e.at } : null;

function Entry({ entry: e, coin, bot }) {
  const trade = tradeOf(e, bot);
  return (
    <li className="flex gap-3 px-4 py-2" data-testid="activity-entry">
      <span className="w-12 shrink-0 pt-0.5 font-mono text-sm text-muted">{clockOf(e.at)}</span>
      <Icon kind={e.icon} />
      <div className="min-w-0 flex-1">
        <p className="text-base font-semibold">{e.headline}</p>
        <p className="text-base text-slate-200">
          {e.actor ? <span>By {e.actor}. </span> : null}
          {e.why}
          {e.amount ? <Amount amount={e.amount} coin={coin} /> : null}
        </p>
      </div>
      {trade ? <WhyButton trade={trade} className="shrink-0 self-start" /> : null}
    </li>
  );
}

/** " Result −0.00120 BTC" / " Paid to you +0.00940 BTC", coloured by sign; "unknown" without a figure. */
function Amount({ amount, coin }) {
  const tone = toneOf(amount.value);
  return (
    <>
      {" "}
      {amount.label}{" "}
      {isNumber(amount.value) ? (
        <span className={clsx("font-semibold tabular-nums", tone === "profit" && "text-profit", tone === "loss" && "text-loss")}>
          {formatCoin(amount.value, coin)}
        </span>
      ) : (
        <span className="text-muted">unknown: not in the journal</span>
      )}
      .
    </>
  );
}

const ICONS = {
  in: ["Money in", "M12 4v13m0 0-5-5m5 5 5-5M5 20h14"],
  out: ["Bought back", "M12 20V7m0 0-5 5m5-5 5 5M5 4h14"],
  shield: ["Protection", "M12 3 5 6v5c0 4.5 3 8.3 7 10 4-1.7 7-5.5 7-10V6l-7-3Z"],
  clock: ["Waiting", "M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"],
  person: ["A person's action", "M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm-7 9a7 7 0 0 1 14 0"],
  dot: ["Other", "M12 12h.01"],
};

/** The round badge: the icon carries a text name, so colour is never the only signal. */
function Icon({ kind }) {
  const [label, path] = ICONS[kind] ?? ICONS.dot;
  return (
    <span
      role="img"
      aria-label={label}
      data-icon={kind}
      className={clsx(
        "flex h-8 w-8 shrink-0 items-center justify-center rounded-full",
        kind === "in" && "bg-profit/15 text-profit",
        kind === "out" && "bg-slate-700 text-white",
        kind === "shield" && "bg-warn/15 text-warn",
        kind === "clock" && "bg-slate-800 text-muted",
        kind === "person" && "bg-testnet/20 text-testnet",
        kind === "dot" && "bg-slate-800 text-muted",
      )}
    >
      <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth={kind === "dot" ? 4 : 2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d={path} />
      </svg>
    </span>
  );
}
