"use client";

import Link from "next/link";
import { Signed } from "./InvestorSummary";
import { formatDateTime, isNumber } from "@/lib/format";
import { MAX_WINDOW_DAYS, agoText, sinceLastVisit } from "@/lib/sincevisit";
import { formatSignedUSD, toneOf } from "@/lib/summary";
import { fmtCoin } from "@/lib/tiles";

// "Since you last looked" (#618, epic E3): under the four answers, one block
// per bot comparing this browser's last visit with now — the closes and their
// reasons, their result net of fees, the futures hedge's lines (#679), the
// worst case then vs now, a warning that began meanwhile. No stored visit: one
// line saying what it will show.
// Helpers in lib/sincevisit.js; the visit is kept by hooks/useLastVisit.js.

export const FIRST_VISIT = "Next time you come back, this shows what changed since this visit.";

const LINK =
  "inline-flex min-h-11 items-center text-sm text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** A signed result; a loss also carries the "▼ loss" mark, so colour is never the only cue. */
function Result({ value, children }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      {toneOf(value) === "loss" ? (
        <span className="rounded border border-loss/60 px-1 text-xs font-medium text-loss" aria-label="loss">
          ▼ loss
        </span>
      ) : null}
      <Signed value={value}>{children}</Signed>
    </span>
  );
}

/**
 * bots: the bots shown; trades/tradesComplete/hedgeEvents: useMonitor's; visit: useLastVisit's
 * { previous, loaded, dismissed, dismiss }; now: the time to compare with (the last poll's).
 */
export default function SinceLastVisit({ bots, trades = {}, tradesComplete, hedgeEvents = {}, visit, now }) {
  if (!visit?.loaded || visit.dismissed || !bots?.length || !now) return null;
  const known = bots.filter((b) => visit.previous[b.name]);
  if (!known.length) {
    return (
      <p className="text-sm text-muted" data-testid="since-first-visit">
        {FIRST_VISIT}
      </p>
    );
  }
  const views = known.map((b) => ({
    bot: b,
    view: sinceLastVisit({
      bot: b,
      trades: trades[b.name],
      tradesComplete: tradesComplete?.[b.name] ?? Array.isArray(trades[b.name]),
      hedgeEvents: hedgeEvents[b.name],
      visit: visit.previous[b.name],
      now,
    }),
  }));
  return (
    <section aria-label="Since you last looked" className="min-w-0 space-y-3 rounded-lg border border-line bg-panel/40 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="mr-auto text-sm font-semibold uppercase tracking-wide text-muted">Since you last looked</h2>
        <button
          type="button"
          onClick={visit.dismiss}
          className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-line px-3 text-sm text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
        >
          Hide until my next visit
        </button>
      </div>
      {views.map(({ bot, view }) => (
        <BotSince key={bot.name} bot={bot} view={view} now={now} />
      ))}
    </section>
  );
}

function BotSince({ bot, view: v, now }) {
  const when = formatDateTime(new Date(v.from).toISOString());
  return (
    <section aria-label={`${v.coin} · since you last looked`} className="min-w-0 space-y-1 text-sm" data-testid={`since-${bot.name}`}>
      <h3 className="font-semibold">
        {v.coin} · since {when} ({agoText(v.from, now)})
      </h3>
      {v.capped ? <p className="text-muted">You were away more than {MAX_WINDOW_DAYS} days: this covers the last {MAX_WINDOW_DAYS} days only.</p> : null}
      {v.pointers.map((p) => (
        <p key={p.chip} role="status" className="text-warn">
          ⚠ {p.chip} began since then: {p.words}. See the header.
        </p>
      ))}
      <HedgeSince hedge={v.hedge} coin={v.coin} />
      {v.unknown ? (
        <p className="text-warn">Closes unknown: {v.unknown}.</p>
      ) : v.loading ? (
        <p className="text-muted">Loading the closes…</p>
      ) : v.nothing ? (
        <p className="text-base">Nothing happened since {when}.</p>
      ) : (
        <Changes bot={bot} v={v} />
      )}
      <WorstLine coin={v.coin} worst={v.worst} />
    </section>
  );
}

/** The hedge's lines: one that changes the book's protection carries ⚠, so colour is never the only cue. */
function HedgeSince({ hedge, coin }) {
  if (!hedge.rows.length) return null;
  return (
    <>
      <p>Futures hedge:</p>
      <ul className="space-y-1" data-testid="since-hedge">
        {hedge.shown.map((r) => (
          <li key={r.seq} className="min-w-0 break-words">
            <span className="tabular-nums text-muted">{formatDateTime(r.at)}</span>{" "}
            <span className={r.risk ? "text-warn" : undefined}>{r.risk ? `⚠ ${r.text}` : r.text}</span>
            {isNumber(r.result) ? (
              <>
                {" "}
                <Result value={r.result}>{fmtCoin(r.result, coin)}</Result>
              </>
            ) : null}
          </li>
        ))}
      </ul>
      {hedge.more ? <p className="text-muted">and {hedge.more} more.</p> : null}
    </>
  );
}

function Changes({ bot, v }) {
  const n = v.closes.length;
  return (
    <>
      {v.result ? (
        <p className="text-base">
          Closed since then, net of fees: <Result value={v.result.coin}>{fmtCoin(v.result.coin, v.coin)}</Result> ·{" "}
          <Result value={v.result.usd}>{formatSignedUSD(v.result.usd)}</Result>
        </p>
      ) : (
        <p>Nothing closed since then.</p>
      )}
      {v.opens ? (
        <p>
          {v.opens} leg{v.opens === 1 ? "" : "s"} sold since then.
        </p>
      ) : null}
      {n ? (
        <>
          <p>
            {n} close{n === 1 ? "" : "s"}:
          </p>
          <ul className="space-y-1">
            {v.shown.map((c) => (
              <li key={c.seq} className="flex flex-wrap items-baseline gap-x-2">
                <span className="tabular-nums text-muted">{formatDateTime(c.at)}</span>
                <span className="break-all">
                  bought back {c.qty} × {c.instrument}
                </span>
                <span>· {c.words}</span>
                <Result value={c.pnlCoin}>{fmtCoin(c.pnlCoin, v.coin)}</Result>
              </li>
            ))}
          </ul>
          {v.more ? <p className="text-muted">and {v.more} more.</p> : null}
        </>
      ) : null}
      <Link href={`/activity?bot=${encodeURIComponent(bot.name)}`} className={LINK}>
        All in Activity →
      </Link>
    </>
  );
}

/** "Worst case (one very bad day): larger — then −0.03000 BTC (≈ −$2,961), now −0.05000 BTC (≈ −$4,935)." */
function WorstLine({ coin, worst: w }) {
  if (w.unknown) return <p className="text-muted">Worst case then vs now unknown: {w.unknown}.</p>;
  if (w.none) return <p>Worst case (one very bad day): about the same, nothing open then or now.</p>;
  const figure = (x) => (x.none ? "nothing open" : `${fmtCoin(-x.coin, coin)}${x.usd != null ? ` (≈ ${formatSignedUSD(-x.usd)})` : ""}`);
  return (
    <p data-testid="since-worst">
      Worst case (one very bad day): <span className="font-semibold">{w.verdict}</span>, then {figure(w.then)}, now {figure(w.now)}.
    </p>
  );
}
