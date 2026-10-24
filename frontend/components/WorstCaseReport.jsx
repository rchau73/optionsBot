import clsx from "clsx";
import { formatCoin, formatNumber, formatUSD, isNumber } from "@/lib/format";
import { formatSignedPct, formatSignedUSD, toneOf } from "@/lib/summary";
import { clockHHMM, formatMove, headlineLoss, scenarioRows, stopWord, whyNoScenarios } from "@/lib/worstcase";

// "Worst case right now" (#318, prototype screen 2): a headline with the worse
// 3σ day, five one-day moves in money, what the bot does on a bad day and what
// the estimate cannot promise. Every number comes from /api/summary.

/** The page's content for one bot's summary. */
export default function WorstCaseReport({ summary }) {
  const coin = summary?.underlying || summary?.worst_case?.coin || "the coin";
  const why = whyNoScenarios(summary);
  if (why) {
    return (
      <section aria-label="Worst case right now" className="space-y-3">
        <h1 className="text-xl font-bold">If {coin} had a very bad day today</h1>
        <p className="text-base text-warn">{why}</p>
        <BotActions summary={summary} coin={coin} />
      </section>
    );
  }
  const wc = summary.worst_case;
  const rows = scenarioRows(summary);
  const worstFall = rows[0]?.move;
  return (
    <section aria-label="Worst case right now" className="space-y-4">
      <Headline summary={summary} coin={coin} />

      <Card title="What different days would do">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[34rem] text-base tabular-nums">
            <thead>
              <tr className="border-b border-line text-left text-xs uppercase tracking-wide text-muted">
                <th scope="col" className="py-2 pr-3 font-semibold">
                  If {coin} moves in a day
                </th>
                <th scope="col" className="py-2 pr-3 font-semibold">
                  How big
                </th>
                <th scope="col" className="py-2 pr-3 text-right font-semibold">
                  Result
                </th>
                <th scope="col" className="py-2 text-right font-semibold">
                  Of the account
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.key} className="border-b border-line/50 last:border-0">
                  <th scope="row" className="py-2 pr-3 text-left font-semibold">
                    {r.move} <span className="font-normal text-muted">({r.words})</span>
                  </th>
                  <td className="py-2 pr-3">{r.swing}</td>
                  <td className="py-2 pr-3 text-right">
                    <Result value={r.usd}>
                      {formatCoin(r.coin, coin)} · {formatSignedUSD(r.usd)}
                    </Result>
                  </td>
                  <td className="py-2 text-right">
                    {r.pct == null ? <span className="text-muted">unknown: no equity reading</span> : <Result value={r.pct}>{formatSignedPct(r.pct)}</Result>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="mt-3 text-base">
          These positions earn when {coin} is calm and lose when it moves a lot either way. Quiet days pay a little; big days cost more.
          That&apos;s the trade.
        </p>
      </Card>

      <div className="grid gap-4 md:grid-cols-2">
        <BotActions summary={summary} coin={coin} />
        <Card title="What this estimate can't promise">
          <ul className="list-disc space-y-1.5 pl-5 text-base">
            <li>
              A crash can be bigger than {worstFall}, and prices can jump past the exit lines before the bot acts.
            </li>
            <li>In a fast market buying back costs more than the screen price.</li>
            <li>It&apos;s worked out from today&apos;s prices with a standard option model.</li>
          </ul>
        </Card>
      </div>

      <details className="rounded-lg border border-line bg-panel">
        <summary className="flex min-h-11 cursor-pointer items-center px-4 text-sm font-semibold uppercase tracking-wide text-muted focus-visible:outline focus-visible:outline-testnet">
          How it&apos;s worked out
        </summary>
        <p className="px-4 pb-4 text-base">
          Each position is re-priced for the move, with today&apos;s implied volatility (DVOL {formatNumber(wc.dvol, 1)}). &ldquo;Very
          bad&rdquo; is {wc.sigmas ?? 3} daily standard deviations: {formatNumber(wc.dvol, 1)} % ÷ √365 × {wc.sigmas ?? 3} ≈{" "}
          {formatNumber(wc.move_pct, 1)} %. The account figure is Deribit&apos;s equity in {coin}. Exits are not modelled here: the real loss
          stops at the exit lines if prices move smoothly.
        </p>
      </details>
    </section>
  );
}

function Headline({ summary, coin }) {
  const loss = headlineLoss(summary);
  const at = clockHHMM(summary.as_of);
  return (
    <div className="space-y-1">
      <h1 className="text-xl font-bold">
        If {coin} had a very bad day today,{" "}
        {loss && loss.usd > 0 ? (
          <>
            you&apos;d lose about{" "}
            <strong className="text-loss">
              {Math.abs(loss.coin).toFixed(4)} {coin} ({formatUSD(Math.abs(loss.usd))})
            </strong>
            , {isNumber(loss.pct) ? `${Math.abs(loss.pct).toFixed(1)} % of the account` : "% of the account unknown: no equity reading"}
          </>
        ) : (
          <>this model shows no loss on either side</>
        )}
      </h1>
      <p className="text-base text-muted">
        Estimated at {at ?? "an unknown time"} from today&apos;s option prices. It changes as {coin} and its expected swings move.
      </p>
    </div>
  );
}

/** What the bot does on a bad day; the stop multiple comes from the bot's config. */
function BotActions({ summary, coin }) {
  const times = stopWord(summary?.stop_loss_multiple);
  return (
    <Card title="What the bot does on a bad day">
      <ul className="list-disc space-y-1.5 pl-5 text-base">
        <li>Buys back a side once {coin} gets close to it (its exit line), before the loss grows.</li>
        <li>
          {times ? (
            <>
              Buys back any side that has lost <strong>{times}</strong> what it was paid, at once.
            </>
          ) : (
            "Buys back any side whose loss reaches its stop-loss limit, at once."
          )}
        </li>
        <li>Stops opening new positions while the market is unsettled.</li>
        <li>Cuts positions at once if the account&apos;s safety margin gets thin.</li>
      </ul>
    </Card>
  );
}

function Card({ title, children }) {
  return (
    <section aria-label={title} className="rounded-lg border border-line bg-panel p-4">
      <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">{title}</h2>
      {children}
    </section>
  );
}

/** A signed result: colour follows the sign, a loss also carries a "loss" mark (never colour alone). */
function Result({ value, children }) {
  const tone = toneOf(value);
  return (
    <span className="inline-flex items-center gap-1.5">
      {tone === "loss" ? (
        <span className="rounded border border-loss/60 px-1 text-xs font-medium text-loss" aria-label="loss">
          ▼ loss
        </span>
      ) : null}
      <span className={clsx("font-semibold tabular-nums", tone === "profit" && "text-profit", tone === "loss" && "text-loss")}>{children}</span>
    </span>
  );
}
