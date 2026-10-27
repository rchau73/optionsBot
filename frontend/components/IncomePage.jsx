"use client";

import clsx from "clsx";
import Link from "next/link";
import { useState, useSyncExternalStore } from "react";
import Badge from "./Badge";
import { useIncome } from "@/hooks/useIncome";
import { formatDate, formatDateTime, isNumber } from "@/lib/format";
import { UNIT_STORAGE_KEY, formatAmount, incomeTableRows, returnLine, tileAmount, upDownLine, worstMonthMove, worstWeekPct } from "@/lib/income";
import { monthLabel, toneOf } from "@/lib/summary";

// The income page (#319, prototype screen 3): one bot's income month by month
// — four tiles and where each month's money came from — in coin or USD.

/** The page for ?bot=<name>: loads /api/income every minute. */
export default function IncomePage({ bot }) {
  const { income, error, loading } = useIncome(bot);
  const [unit, setUnit] = useUnit();
  return <IncomeView bot={bot} income={income} error={error} loading={loading} unit={unit} onUnit={setUnit} />;
}

// The coin | USD choice lives in localStorage; useSyncExternalStore keeps the
// server render (no storage, coin) and the browser in step, as in Panel.jsx.
const unitListeners = new Set();
const subscribeUnit = (fn) => {
  unitListeners.add(fn);
  window.addEventListener("storage", fn);
  return () => {
    unitListeners.delete(fn);
    window.removeEventListener("storage", fn);
  };
};
const readUnit = () => {
  try {
    return window.localStorage.getItem(UNIT_STORAGE_KEY);
  } catch {
    return null; // storage blocked: the default
  }
};

/** The coin | USD choice, remembered per browser. */
function useUnit() {
  const [local, setLocal] = useState(null); // this visit's choice when storage is unavailable
  const saved = useSyncExternalStore(subscribeUnit, readUnit, () => null);
  const unit = local ?? (saved === "usd" ? "usd" : "coin");
  const choose = (u) => {
    setLocal(u);
    try {
      window.localStorage.setItem(UNIT_STORAGE_KEY, u);
    } catch {
      /* storage blocked: the choice lasts this visit */
    }
    unitListeners.forEach((fn) => fn());
  };
  return [unit, choose];
}

/** The page's content for one state of the data (pure: easy to test). */
export function IncomeView({ bot, income, error, loading, unit = "coin", onUnit = () => {} }) {
  const coin = income?.coin || bot?.toUpperCase() || "coin";
  return (
    <div className="min-h-screen">
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
        <Link href="/" className="inline-flex min-h-11 items-center text-sm text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2">
          ← Overview
        </Link>
        <h1 className="mr-auto text-lg font-bold">Income · {coin}</h1>
        {income?.summary?.label ? (
          <Badge tone={income.summary.label === "live" ? "live" : "testnet"}>{income.summary.label.toUpperCase()}</Badge>
        ) : null}
        <UnitToggle coin={coin} unit={unit} onUnit={onUnit} />
      </header>
      <main className="mx-auto max-w-5xl space-y-4 p-4">
        {!bot ? (
          <p role="alert" className="text-base text-warn">
            No bot chosen: open this page as /income?bot=&lt;name&gt; from the overview.
          </p>
        ) : error && !income ? (
          <p role="alert" className="text-base text-warn">
            Income unavailable: {error}. Nothing is shown until the bot answers again.
          </p>
        ) : loading || !income ? (
          <p className="text-base text-muted">Loading the income…</p>
        ) : (
          <>
            {error ? (
              <p role="status" className="text-sm text-warn">
                Last refresh failed ({error}); showing the figures from before.
              </p>
            ) : null}
            <Tiles income={income} unit={unit} coin={coin} />
            <MonthTable income={income} unit={unit} coin={coin} />
            <HowItWorks coin={coin} />
          </>
        )}
        <footer className="space-y-1 text-xs text-muted">
          <p>
            Months are UTC calendar months. A month&apos;s result counts positions closed in it; open positions count when they close. Testnet
            results are practice, not a track record.
          </p>
          {income?.summary?.missing_days?.length ? (
            <p>
              No P&amp;L history on {income.summary.missing_days.length} day{income.summary.missing_days.length === 1 ? "" : "s"} of the window
              (listed, never filled in): the worst week and holding alone cover only the recorded days.
            </p>
          ) : null}
          <p>Educational study, not investment advice.</p>
        </footer>
      </main>
    </div>
  );
}

function UnitToggle({ coin, unit, onUnit }) {
  const button = (value, label) => (
    <button
      type="button"
      aria-pressed={unit === value}
      onClick={() => onUnit(value)}
      className={clsx(
        "min-h-11 min-w-11 px-3 text-sm font-semibold focus-visible:outline focus-visible:outline-2",
        unit === value ? "bg-slate-700 text-white" : "text-muted hover:text-white",
      )}
    >
      {label}
    </button>
  );
  return (
    <div role="group" aria-label="Show amounts in" className="flex overflow-hidden rounded-md border border-line">
      {button("coin", coin)}
      {button("usd", "USD")}
    </div>
  );
}

/** A signed amount; the colour follows the sign, the size never changes. */
function Signed({ value, children }) {
  const tone = toneOf(value);
  return <span className={clsx("font-semibold tabular-nums", tone === "profit" && "text-profit", tone === "loss" && "text-loss")}>{children}</span>;
}

function Tile({ label, loss, children, sub }) {
  return (
    <section aria-label={label} className={clsx("rounded-lg border bg-panel px-3 py-2", loss ? "border-loss/60" : "border-line")}>
      <h2 className="text-xs uppercase tracking-wide text-muted">{label}</h2>
      <div className="mt-0.5 text-lg">{children}</div>
      {sub ? <p className="text-xs text-muted">{sub}</p> : null}
    </section>
  );
}

function Tiles({ income, unit, coin }) {
  const s = income.summary;
  const amount = (v) => <Signed value={v}>{formatAmount(v, unit, coin)}</Signed>;
  const total = tileAmount(s?.total_6m, unit);
  const average = tileAmount(s?.average_month, unit);
  const worst = s?.worst_month ? (unit === "usd" ? s.worst_month.usd : s.worst_month.coin) : null;
  const ww = s?.worst_week;
  const wwUSD = unit === "usd" && ww?.known;
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <Tile label="Last 6 months, after fees" sub={s ? returnLine(s, coin) : "not yet"}>
        {s ? amount(total) : <span className="text-muted">not yet</span>}
      </Tile>
      <Tile label="Average month" sub={s?.average_month ? upDownLine(s) : "not yet: no month with a close"}>
        {isNumber(average) ? amount(average) : <span className="text-muted">not yet</span>}
      </Tile>
      <Tile
        label="Worst month"
        loss={s?.worst_month?.coin < 0}
        sub={s?.worst_month ? [monthLabel(s.worst_month.month), worstMonthMove(s.worst_month, coin)].filter(Boolean).join(" · ") : "not yet: no month with a close"}
      >
        {isNumber(worst) ? amount(worst) : <span className="text-muted">not yet</span>}
      </Tile>
      <Tile
        label="Worst week"
        loss={ww?.coin < 0}
        sub={ww?.known ? `${formatDate(ww.start)} → ${formatDate(ww.end)} · ${worstWeekPct(ww)}${wwUSD ? " · in coin (P&L history)" : ""}` : "not yet: needs 7 days of P&L history"}
      >
        {ww?.known ? <Signed value={ww.coin}>{formatAmount(ww.coin, "coin", coin)}</Signed> : <span className="text-muted">not yet</span>}
      </Tile>
    </div>
  );
}

function MonthTable({ income, unit, coin }) {
  const rows = incomeTableRows(income, unit);
  const amount = (v) => <Signed value={v}>{formatAmount(v, unit, coin)}</Signed>;
  return (
    <section aria-label="Each month, where the money came from" className="rounded-lg border border-line bg-panel">
      <h2 className="border-b border-line px-4 py-2 text-sm font-semibold uppercase tracking-wide text-muted">Each month, where the money came from</h2>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[40rem] text-base tabular-nums">
          <thead className="text-left text-xs uppercase tracking-wide text-muted">
            <tr>
              <th className="px-4 py-2 font-medium">Month</th>
              <th className="px-2 py-2 text-right font-medium">Paid to you</th>
              <th className="px-2 py-2 text-right font-medium">Paid to buy back</th>
              <th className="px-2 py-2 text-right font-medium">Exchange fees</th>
              <th className="px-2 py-2 text-right font-medium">Result</th>
              <th className="px-4 py-2 text-right font-medium">Holding alone</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.month} className={clsx("border-t border-line/50", r.loss && "bg-loss/10")} data-loss={r.loss || undefined}>
                <th scope="row" className="px-4 py-1.5 text-left font-normal">
                  {r.label}
                  {r.current ? <span className="text-muted"> (so far)</span> : null}
                  {r.loss ? <span className="font-semibold text-loss"> · loss</span> : null}
                </th>
                {r.closes ? (
                  <>
                    <td className="px-2 py-1.5 text-right">{amount(r.premium)}</td>
                    <td className="px-2 py-1.5 text-right">{amount(r.buyback)}</td>
                    <td className="px-2 py-1.5 text-right">{amount(r.fees)}</td>
                    <td className="px-2 py-1.5 text-right">{amount(r.result)}</td>
                  </>
                ) : (
                  <td colSpan={4} className="px-2 py-1.5 text-right text-muted">
                    no closes
                  </td>
                )}
                <td className="px-4 py-1.5 text-right">
                  {r.holdReason ? <span className="text-sm text-muted">unknown: {r.holdReason}</span> : amount(r.hold)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="border-t border-line px-4 py-2 text-sm text-muted">
        Counted in {coin}, holding alone neither gains nor loses: every {coin} earned here is extra. In dollars, both move with the {coin} price;
        switch to <span className="font-semibold">USD</span> above to compare.
      </p>
      {income.summary?.to ? <p className="px-4 pb-2 text-xs text-muted">as of {formatDateTime(income.summary.to)}</p> : null}
    </section>
  );
}

function HowItWorks({ coin }) {
  return (
    <section aria-label="How the income works" className="rounded-lg border border-line bg-panel px-4 py-3">
      <h2 className="mb-1 text-sm font-semibold uppercase tracking-wide text-muted">How the income works</h2>
      <p className="text-base">
        Buyers pay the bot for the right to buy {coin} above a ceiling or sell it below a floor before a date. The bot keeps the payment if{" "}
        {coin} stays between them, and buys the right back early when {coin} gets close to one side. <strong>Paid to you</strong> minus{" "}
        <strong>paid to buy back</strong> minus <strong>fees</strong> is the month&apos;s result.
      </p>
    </section>
  );
}
