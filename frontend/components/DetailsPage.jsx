"use client";

import clsx from "clsx";
import { useMemo, useState } from "react";
import Panel from "./Panel";
import Filters from "./Filters";
import PositionsTable from "./PositionsTable";
import HeldLegs from "./HeldLegs";
import TradeHistory from "./TradeHistory";
import PendingOrders from "./PendingOrders";
import OtherPositions from "./OtherPositions";
import PayoffModal from "./PayoffModal";
import { useMonitor } from "@/hooks/useMonitor";
import { heldRows } from "@/lib/held";
import { consolidateRows, filterRows, groupRows, legRows, summarise } from "@/lib/monitor";
import { pickAccount, unmanagedPositions } from "@/lib/account";
import { countsLine, sectionCounts, stuckOrders, workingOrderCount } from "@/lib/details";
import { DISPLAY_TZ_LABEL } from "@/lib/time";
import { track } from "@/lib/track";

const NO_FILTERS = { bot: "", strategy: "", type: "", moneyness: "", groupBy: "strangle" };

const ORDERS_HINT =
  "Limit orders resting on Deribit. Filled orders move to Open positions; unfilled ones are cancelled after the fill timeout and retried on a later cycle.";
const OTHERS_HINT = "Positions on the account that no bot manages. No bot closes or sizes them, but Deribit counts them in equity and margin.";

/** The title's one text line of counts (no tiles). */
function CountsLine({ kpi, polledAt }) {
  return (
    <p className="text-sm text-muted" data-testid="details-counts">
      {countsLine(kpi, polledAt)}
    </p>
  );
}

/** Sticky section links with their counts; a count needing attention gets an amber badge. */
function SectionBar({ sections }) {
  return (
    <nav aria-label="Sections" className="sticky top-0 z-20 -mx-3 border-b border-line bg-surface/95 px-3 py-1 backdrop-blur">
      <ul className="flex flex-wrap gap-1">
        {sections.map((s) => (
          <li key={s.id}>
            <a
              href={`#${s.id}`}
              onClick={() => track("details_section_jump", { section: s.id })}
              className="inline-flex min-h-9 items-center gap-1 rounded-full px-3 text-sm text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
            >
              {s.label}
              <span
                data-attention={s.attention ? "1" : "0"}
                className={clsx("rounded px-1.5 text-xs tabular-nums", s.attention ? "bg-warn/20 font-semibold text-warn" : "bg-slate-800 text-slate-300")}
              >
                {s.n}
              </span>
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}

/** Skeleton rows while the first poll is on its way. */
function Skeleton({ rows = 3 }) {
  return (
    <div aria-busy="true" aria-label="Loading" className="space-y-1" data-testid="skeleton">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="h-6 animate-pulse rounded bg-slate-800" />
      ))}
    </div>
  );
}

/** A section shown as one line (an empty Working orders or Not managed). */
function OneLine({ id, title, text, hint }) {
  return (
    <section id={id} aria-label={title} className="scroll-mt-12 rounded-lg border border-line bg-panel px-4 py-2 text-sm">
      <span title={hint} className="font-semibold uppercase tracking-wide text-muted">
        {title}:
      </span>{" "}
      <span className="text-muted">{text}</span>
    </section>
  );
}

/**
 * Details (pro), its own page (#405): /details. For the active trader, numbers
 * first and graphics small — the title with one line of counts, a sticky bar
 * of section links, then Open positions (one line per leg, grouped by
 * strangle), Missing legs, Trades and results, Working orders and Not managed.
 * P&L is in each bot's coin first, ≈ USD beside it; coins are never added
 * together. Reads the existing APIs only, through useMonitor.
 */
export default function DetailsPage() {
  const { names, bots, trades, error, pollMs, polledAt } = useMonitor();
  const [filters, setFilters] = useState(NO_FILTERS);
  // The payoff modal (#391): { bot, strangleId, opener } — opener gets the focus back on close.
  const [chart, setChart] = useState(null);
  const openChart = (botName, strangleId, opener) => setChart({ bot: botName, strangleId, opener });
  const chartBot = chart ? bots.find((b) => b.name === chart.bot) : null;

  const allTrades = useMemo(() => Object.values(trades ?? {}).flat(), [trades]);
  const rows = useMemo(() => consolidateRows(legRows(bots)), [bots]);
  const groups = useMemo(() => groupRows(filterRows(rows, filters), filters.groupBy), [rows, filters]);
  const options = useMemo(() => ({ bots: bots.map((b) => b.name), strategies: [...new Set(rows.map((r) => r.strategyId))] }), [bots, rows]);
  const held = heldRows(bots);
  const heldByStrangle = {};
  for (const h of held) (heldByStrangle[`${h.bot}:${h.strangle_id}`] ??= []).push(h);
  const kpi = summarise(bots);
  const others = unmanagedPositions(pickAccount(bots, polledAt), bots);
  const statuses = Object.fromEntries(bots.map((b) => [b.name, b.status]));
  const health = Object.fromEntries(
    bots.map((b) => [b.name, { stale: b.stale && Boolean(b.status), error: b.error, status: b.status, ageSec: b.updatedAt && polledAt ? Math.max(0, (polledAt - b.updatedAt) / 1000) : null }]),
  );
  const down = bots.filter((b) => b.error && !b.status);
  const loading = names == null || (bots.length > 0 && bots.every((b) => !b.status && !b.error));
  const orders = workingOrderCount(bots);
  const sections = sectionCounts({
    legs: rows.length,
    held: held.length,
    trades: allTrades.length,
    orders,
    stuck: polledAt ? stuckOrders(bots, polledAt) : 0,
    others: others.rows.length,
  });

  return (
    <main className="space-y-3 p-3">
      <header>
        <h1 className="text-xl font-semibold">Details (pro)</h1>
        <CountsLine kpi={kpi} polledAt={polledAt} />
        {error ? (
          <p role="alert" className="text-sm text-warn">
            {error}
          </p>
        ) : null}
      </header>
      {kpi.halted ? (
        <div role="alert" className="rounded border border-loss bg-loss/10 px-3 py-2 text-loss">
          Kill switch fired — trading is halted on at least one bot.
        </div>
      ) : null}
      <SectionBar sections={sections} />

      <Panel
        id="positions"
        className="scroll-mt-12"
        title="Open positions"
        right={<Filters filters={filters} options={options} onChange={setFilters} />}
      >
        {down.map((b) => (
          <p key={b.name} role="alert" className="mb-2 text-sm text-warn">
            {b.name.toUpperCase()}: bot down ({b.error}); its positions are not shown until it answers again.
          </p>
        ))}
        {loading ? (
          <Skeleton rows={8} />
        ) : (
          <PositionsTable groups={groups} statuses={statuses} onOpenChart={openChart} held={filters.groupBy === "strangle" ? heldByStrangle : {}} health={health} />
        )}
      </Panel>

      <Panel id="held" className="scroll-mt-12" title="Missing legs: why repair holds them">
        {loading ? <Skeleton /> : <HeldLegs bots={bots} onOpenChart={openChart} />}
      </Panel>

      <Panel id="trades" className="scroll-mt-12" title="Trades and results">
        {loading ? <Skeleton /> : <TradeHistory trades={allTrades} />}
      </Panel>

      {!loading && orders === 0 ? (
        <OneLine id="orders" title="Working orders (placed, not filled yet)" text="none: no entry or repair order is resting on Deribit." hint={ORDERS_HINT} />
      ) : (
        <Panel id="orders" className="scroll-mt-12" title="Working orders (placed, not filled yet)" hint={ORDERS_HINT}>
          {loading ? <Skeleton rows={1} /> : <PendingOrders bots={bots} />}
        </Panel>
      )}

      {!loading && others.reported && !others.error && others.rows.length === 0 ? (
        <OneLine id="others" title="Not managed by the bot (still uses margin)" text="none: every position on the account is managed by a bot." hint={OTHERS_HINT} />
      ) : (
        <Panel id="others" className="scroll-mt-12" title="Not managed by the bot (still uses margin)" hint={OTHERS_HINT}>
          {loading ? <Skeleton rows={1} /> : <OtherPositions data={others} />}
        </Panel>
      )}

      {chart ? (
        <PayoffModal
          report={chartBot?.payoff ?? null}
          error={chartBot?.payoffError ?? chartBot?.error ?? null}
          strangleId={chart.strangleId}
          opener={chart.opener}
          onClose={() => setChart(null)}
        />
      ) : null}
      <p className="text-center text-xs text-muted">
        Read-only · refreshes every {pollMs / 1000}s · times in {DISPLAY_TZ_LABEL} (UTC−3) · P&amp;L in each bot&apos;s coin, ≈ USD at spot · history and counts restored from each
        bot&apos;s journal · educational study, not investment advice
      </p>
    </main>
  );
}
