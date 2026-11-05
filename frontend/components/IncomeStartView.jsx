import clsx from "clsx";
import { formatCoin, formatDate, formatDateTime, formatUSD, isNumber } from "@/lib/format";
import { formatStartPct, startTableRows } from "@/lib/income";
import { toneOf } from "@/lib/summary";

// The income page's "% of start" view (#629): every result as a % of the start
// balance — Deribit's margin balance when the period began — so two
// environments of different size (stable, mvp) compare. All figures share one
// start time; ?from= lines both environments up on the same start.

/** A signed % whose colour follows the sign; the size never changes. */
function Pct({ value }) {
  const tone = toneOf(value);
  return <span className={clsx("font-semibold tabular-nums", tone === "profit" && "text-profit", tone === "loss" && "text-loss")}>{formatStartPct(value)}</span>;
}

/** A % or the words saying why there is none (never a silent 0). */
function PctOr({ value, missing }) {
  return isNumber(value) ? <Pct value={value} /> : <span className="text-sm text-muted">{missing}</span>;
}

/** The "% of start" view of one bot's /api/income. */
export default function IncomeStartView({ income, coin }) {
  const st = income?.start;
  if (!st?.known) {
    return (
      <section aria-label="Start balance" className="rounded-lg border border-line bg-panel px-4 py-3">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">Start balance</h2>
        <p role="status" className="text-base text-warn">
          No start balance yet: {st?.reason || "the bot has not sent its P&L history"}. No % is shown until a balance is recorded.
        </p>
      </section>
    );
  }
  return (
    <>
      <StartStrip st={st} coin={coin} />
      <StartTiles st={st} coin={coin} />
      <StartMonthTable income={income} coin={coin} />
    </>
  );
}

function StartStrip({ st, coin }) {
  return (
    <section aria-label="Start balance" className="rounded-lg border border-line bg-panel px-4 py-3">
      <h2 className="text-xs uppercase tracking-wide text-muted">Start balance</h2>
      <p className="text-lg font-semibold tabular-nums">
        {formatCoin(st.balance_coin, coin).replace(/^\+/, "")}
        {isNumber(st.balance_usd) ? <span className="text-base font-normal text-muted"> · {formatUSD(st.balance_usd)}</span> : null}
      </p>
      <p className="text-sm text-muted">
        Recorded {formatDateTime(st.at)}
        {st.requested ? ` (the first balance at or after ${formatDateTime(st.requested)})` : " (the first balance in the window)"} · {st.source}
      </p>
    </section>
  );
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

function StartTiles({ st, coin }) {
  const ww = st.worst_week;
  const noSpot = "unknown: no spot recorded";
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <Tile label="Bot result, % of start" loss={st.result_pct < 0} sub={`${formatCoin(st.result_coin, coin)} from ${st.closes} close${st.closes === 1 ? "" : "s"}, after fees`}>
        <PctOr value={st.result_pct} missing="unknown" />
      </Tile>
      <Tile label="Holding alone, % of start" loss={st.hold_pct < 0} sub={`in USD; 0 % in ${coin}: a held ${coin} stays one ${coin}`}>
        <PctOr value={st.hold_pct} missing={noSpot} />
      </Tile>
      <Tile label="Bot + holding, % of start" loss={st.combined_pct < 0} sub="in USD: the balance plus the result, at the end's price">
        <PctOr value={st.combined_pct} missing={noSpot} />
      </Tile>
      <Tile
        label="Worst week, % of start"
        loss={ww?.known && ww.coin < 0}
        sub={ww?.known ? `${formatDate(ww.start)} → ${formatDate(ww.end)} · open positions at their marks` : `known on ${formatDateTime(ww?.known_at)}, 7 days after the start balance`}
      >
        {ww?.known ? <Pct value={ww.pct} /> : <span className="text-muted">not yet</span>}
      </Tile>
    </div>
  );
}

function StartMonthTable({ income, coin }) {
  const rows = startTableRows(income);
  return (
    <section aria-label="Each month, as a % of its start balance" className="rounded-lg border border-line bg-panel">
      <h2 className="border-b border-line px-4 py-2 text-sm font-semibold uppercase tracking-wide text-muted">Each month, as a % of its start balance</h2>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[40rem] text-base tabular-nums">
          <thead className="text-left text-xs uppercase tracking-wide text-muted">
            <tr>
              <th className="px-4 py-2 font-medium">Month</th>
              <th className="px-2 py-2 text-right font-medium">Start balance</th>
              <th className="px-2 py-2 text-right font-medium">Bot result</th>
              <th className="px-2 py-2 text-right font-medium">Holding alone</th>
              <th className="px-4 py-2 text-right font-medium">Bot + holding</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.month} className={clsx("border-t border-line/50", r.loss && "bg-loss/10")} data-loss={r.loss || undefined}>
                <th scope="row" className="px-4 py-1.5 text-left font-normal">
                  {r.label}
                  {r.current ? <span className="text-muted"> (so far)</span> : null}
                  {r.partialFrom ? <span className="block text-xs text-muted">partial: from {formatDate(r.partialFrom)}</span> : null}
                  {r.loss ? <span className="font-semibold text-loss"> · loss</span> : null}
                </th>
                {r.reason ? (
                  <td colSpan={4} className="px-4 py-1.5 text-right text-sm text-muted">
                    no %: {r.reason}
                  </td>
                ) : (
                  <>
                    <td className="px-2 py-1.5 text-right">
                      {formatCoin(r.balanceCoin, coin).replace(/^\+/, "")}
                      {isNumber(r.balanceUSD) ? <span className="block text-xs text-muted">{formatUSD(r.balanceUSD)}</span> : null}
                    </td>
                    <td className="px-2 py-1.5 text-right">
                      <Pct value={r.result} />
                      {r.closes === 0 ? <span className="block text-xs text-muted">no closes</span> : null}
                    </td>
                    <td className="px-2 py-1.5 text-right">
                      <PctOr value={r.hold} missing="no spot" />
                    </td>
                    <td className="px-4 py-1.5 text-right">
                      <PctOr value={r.combined} missing="no spot" />
                    </td>
                  </>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="border-t border-line px-4 py-2 text-sm text-muted">
        Each month is measured from its own first balance. Bot result counts the closes after it, and any futures hedge result, net of fees; holding alone is the {coin} price
        move from that time; open positions count when they close. Add <span className="font-semibold">?from=</span> to the address to start both
        environments at the same time.
      </p>
      {income.start?.to ? <p className="px-4 pb-2 text-xs text-muted">as of {formatDateTime(income.start.to)}</p> : null}
    </section>
  );
}
