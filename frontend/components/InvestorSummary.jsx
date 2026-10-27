import clsx from "clsx";
import Link from "next/link";
import { formatCoin, formatDate, formatDateTime, formatNumber, formatPct, formatUSD, isNumber } from "@/lib/format";
import { isFirstDay } from "@/lib/overview";
import { formatSignedPct, formatSignedUSD, hasIncome, holdingLine, incomeRows, toneOf, worstCaseRows } from "@/lib/summary";
import FirstDayPanel from "./FirstDayPanel";

// The investor view (#246): per bot, the four answers of /api/summary — what it
// holds, income per month, the worst case now in money, the last action in
// plain words. Shown first; the developer panels below are the detail.

/** One investor summary card per bot. */
export default function InvestorSummary({ bots }) {
  if (!bots?.length) return null;
  return (
    <div className="grid gap-3 xl:grid-cols-2">
      {bots.map((bot) => (
        <BotSummary key={bot.name} bot={bot} />
      ))}
    </div>
  );
}

/** One bot's four answers, or why they cannot be shown. */
export function BotSummary({ bot }) {
  // A brand-new bot (nothing held, no trade yet) shows its first day (#321).
  if (isFirstDay(bot)) return <FirstDayPanel bot={bot} />;
  const s = bot.summary;
  const failure = bot.summaryError ?? bot.error;
  const title = `${bot.name.toUpperCase()} · investor summary`;
  return (
    <section aria-label={title} className="rounded-lg border border-line bg-panel">
      <header className="flex flex-wrap items-baseline justify-between gap-2 border-b border-line px-4 py-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">{title}</h2>
        {s && !failure ? <span className="text-xs text-muted">as of {formatDateTime(s.as_of)}</span> : null}
      </header>
      {failure ? (
        <p role="alert" className="px-4 py-3 text-base text-warn">
          Summary unavailable: {failure}. Nothing is shown until the bot answers again.
        </p>
      ) : !s ? (
        <p className="px-4 py-3 text-base text-muted">Loading the summary…</p>
      ) : (
        <div className="grid gap-4 p-4 md:grid-cols-2">
          <Holding holding={s.holding} bot={bot.name} />
          <Income income={s.income} bot={bot.name} />
          <WorstCase wc={s.worst_case} bot={bot.name} />
          <LastAction action={s.last_action} coin={s.underlying} />
        </div>
      )}
    </section>
  );
}

function Answer({ question, children }) {
  return (
    <div className="space-y-1.5">
      <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{question}</h3>
      {children}
    </div>
  );
}

/** A signed amount; the colour follows the sign, the size never changes. */
export function Signed({ children, value }) {
  const tone = toneOf(value);
  return <span className={clsx("font-semibold tabular-nums", tone === "profit" && "text-profit", tone === "loss" && "text-loss")}>{children}</span>;
}

function Holding({ holding: h, bot }) {
  const howLink = (
    <Link
      href={`/how-it-works?bot=${encodeURIComponent(bot)}`}
      className="inline-flex min-h-11 items-center text-base text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-testnet"
    >
      How it works: a strangle in three pictures →
    </Link>
  );
  if (!h || !h.legs) {
    return (
      <Answer question="What it holds">
        <p className="text-base">Nothing open right now. New strangles appear here once the bot sells them.</p>
        {howLink}
      </Answer>
    );
  }
  return (
    <Answer question="What it holds">
      <p className="text-base font-semibold">{holdingLine(h)}</p>
      <p className="text-base tabular-nums">
        {formatNumber(h.notional_coin, 2)} {h.coin} sold ≈ {formatUSD(h.notional_usd)} of the coin
      </p>
      <p className="text-base">
        Nearest expiry {formatDate(h.nearest_expiry)}
        {isNumber(h.nearest_dte) ? ` (in ${formatNumber(h.nearest_dte, 1)} days)` : ""}
      </p>
      {howLink}
    </Answer>
  );
}

function Income({ income, bot }) {
  const coin = income?.coin ?? "";
  return (
    <Answer question="Income per month (realised, net of fees)">
      {hasIncome(income) ? (
        <table className="w-full text-base tabular-nums">
          <thead className="sr-only">
            <tr>
              <th>Month</th>
              <th>{coin}</th>
              <th>USD</th>
              <th>Closes</th>
            </tr>
          </thead>
          <tbody>
            {incomeRows(income).map((m) => (
              <tr key={m.month} className="border-b border-line/50 last:border-0">
                <th scope="row" className="py-0.5 pr-2 text-left font-normal">
                  {m.label}
                  {m.current ? <span className="text-muted"> (this month)</span> : null}
                </th>
                {m.closes ? (
                  <>
                    <td className="py-0.5 pr-2 text-right">
                      <Signed value={m.coin}>{formatCoin(m.coin, coin)}</Signed>
                    </td>
                    <td className="py-0.5 pr-2 text-right">
                      <Signed value={m.usd}>{formatSignedUSD(m.usd)}</Signed>
                    </td>
                    <td className="py-0.5 text-right text-muted">
                      {m.closes} close{m.closes === 1 ? "" : "s"}
                    </td>
                  </>
                ) : (
                  <td colSpan={3} className="py-0.5 text-right text-muted">
                    no closes
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="text-base">
          No closed trades in the last six months yet. Each month&apos;s net income (in {coin || "coin"} and USD, after fees) appears
          here once the bot buys back or lets expire its first option.
        </p>
      )}
      <p className="text-base">
        Open positions now (not realised):{" "}
        <Signed value={income?.unrealised_coin}>{formatCoin(income?.unrealised_coin, coin)}</Signed> ·{" "}
        <Signed value={income?.unrealised_usd}>{formatSignedUSD(income?.unrealised_usd)}</Signed>
      </p>
      {income?.basis ? <p className="text-xs text-muted">{income.basis}</p> : null}
      <Link
        href={`/income?bot=${encodeURIComponent(bot)}`}
        className="inline-flex min-h-11 items-center text-base text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2"
      >
        Income, month by month →
      </Link>
    </Answer>
  );
}

function WorstCase({ wc, bot }) {
  const rows = worstCaseRows(wc);
  return (
    <Answer question="Worst case now (one day)">
      {rows.length ? (
        <ul className="space-y-1 text-base tabular-nums">
          {rows.map((r) => (
            <li key={r.key}>
              If {wc.coin} {r.label} {formatPct(wc.move_pct)} to {formatUSD(r.spot)}:{" "}
              <Signed value={r.coin}>{formatCoin(r.coin, wc.coin)}</Signed> · <Signed value={r.usd}>{formatSignedUSD(r.usd)}</Signed> ·{" "}
              {r.pct == null ? <span className="text-muted">% of equity unknown</span> : <Signed value={r.pct}>{formatSignedPct(r.pct)} of equity</Signed>}
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-base text-warn">Unknown{wc?.reason ? `: ${wc.reason}` : ""}.</p>
      )}
      <p className="text-base">
        <span className="font-semibold">This is a model, not a quote</span>
        {wc?.model ? ` — ${wc.model}` : ""}
        {isNumber(wc?.dvol) && wc.dvol > 0 ? ` (DVOL ${formatNumber(wc.dvol, 1)}, ${wc.sigmas ?? 3}σ).` : "."}
      </p>
      <Link
        href={`/worst-case?bot=${encodeURIComponent(bot)}`}
        className="inline-flex min-h-11 items-center text-base text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-testnet"
      >
        Five days in money: worst case right now →
      </Link>
    </Answer>
  );
}

function LastAction({ action, coin }) {
  if (!action) {
    return (
      <Answer question="Last action">
        <p className="text-base">No trade yet. The bot&apos;s first sale or buy-back is explained here when it happens.</p>
      </Answer>
    );
  }
  return (
    <Answer question="Last action">
      <p className="text-base font-semibold">{action.reason || action.rule}</p>
      <p className="text-base tabular-nums">
        {formatDateTime(action.at)} · {action.kind === "close" ? "bought back" : "sold"} {formatNumber(action.qty, 2)} × {action.instrument}
      </p>
      {action.kind === "close" && (isNumber(action.pnl_coin) || isNumber(action.pnl_usd)) ? (
        <p className="text-base">
          Result of that trade (net of fees): <Signed value={action.pnl_coin}>{formatCoin(action.pnl_coin, coin)}</Signed> ·{" "}
          <Signed value={action.pnl_usd}>{formatSignedUSD(action.pnl_usd)}</Signed>
        </p>
      ) : null}
      {action.detail ? <p className="text-xs text-muted">{action.detail}</p> : null}
    </Answer>
  );
}
