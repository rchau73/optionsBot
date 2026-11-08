import { useState } from "react";
import clsx from "clsx";
import Badge from "./Badge";
import MicroBar from "./MicroBar";
import { CloseButton, ConfirmCloseModal } from "./ManualClose";
import { ageSeconds, formatAge, formatDate, formatDateTime, formatNumber, formatPct, formatPrice, formatUSD, isNumber } from "@/lib/format";
import {
  DELTA_EXIT_WARN,
  approxUSD,
  bookTip,
  deltaExitRatio,
  deltaSideWords,
  formatCoinFirst,
  formatPositionDelta,
  hedgeGainWords,
  netPositionDelta,
  positionDelta,
  positionDeltaTip,
  usdPerPct,
} from "@/lib/details";
import { formatSignedUSD } from "@/lib/summary";
import { holdRule } from "@/lib/held";
import { STALE_AFTER_SEC } from "@/lib/monitor";

const pnlClass = (v) => clsx("tabular-nums", v > 0 && "text-profit", v < 0 && "text-loss");
const TD = "px-2 py-1";
const NUM = "px-2 py-1 text-right tabular-nums";

/** Renders a live value; re-keying by value replays the flash when it changes. */
function Live({ value, children }) {
  return (
    <span key={String(value)} className="flash px-0.5">
      {children}
    </span>
  );
}

/** Small inline tag when a leg's price is not live or is getting old. */
function Freshness({ row }) {
  if (row.markSource !== "live") {
    return (
      <span className="ml-1 text-[10px] text-warn" title="No live ticker for this instrument; value from the last decision cycle">
        last cycle
      </span>
    );
  }
  const age = ageSeconds(row.markAsOf);
  if (age != null && age > STALE_AFTER_SEC) return <span className="ml-1 text-[10px] text-warn">{Math.floor(age)}s old</span>;
  return null;
}

/** P&L in the coin with its fixed decimals, ≈ USD small beside it. */
function CoinPnl({ value, unit, usd }) {
  return (
    <span className={pnlClass(value)}>
      {formatCoinFirst(value, unit)} <span className="text-[11px] text-muted">{approxUSD(usd)}</span>
    </span>
  );
}

/** "Position Δ" (#715): the leg's (or a sum's) delta in coin, then what a +1 % move does in USD, muted; neutral colour. */
export function PositionDeltaCell({ delta, unit, spot, prefix = "", suffix = "" }) {
  return (
    <span className="tabular-nums" data-testid="position-delta">
      {prefix}
      {formatPositionDelta(delta, unit)}
      {delta != null ? (
        <span className="text-[11px] text-muted">
          {" "}
          {prefix ? "· " : ""}
          {formatSignedUSD(usdPerPct(delta, spot))}
          {suffix}
        </span>
      ) : null}
    </span>
  );
}

/** One foot row of a bot's book: its label, the delta in the Position Δ column and a note after it. */
function BookRow({ label, delta, unit, spot, note, title, testId }) {
  return (
    <tr className="text-xs" data-testid={testId}>
      <td colSpan={6} className="px-2 py-1 font-semibold">
        {label}
      </td>
      <td className="px-2 py-1 text-right">
        <PositionDeltaCell delta={delta} unit={unit} spot={spot} suffix=" per +1 %" />
      </td>
      <td colSpan={7} className="px-2 py-1 text-muted" title={title}>
        {note}
      </td>
    </tr>
  );
}

/**
 * The foot rows of one bot (#715): All options (Σ position Δ), and while a
 * futures hedge is shown the perpetual and the Book with it, the coin delta
 * the hedge holds flat in the note. b: lib/details bookDelta.
 */
function BookRows({ b, dim }) {
  const coin = (v) => formatPositionDelta(v, b.unit);
  const side = deltaSideWords(b.options, b.unit);
  const optionsNote = [side, b.unavailable ? "hedge state unavailable" : null].filter(Boolean).join(" · ");
  const h = b.hedge;
  return (
    <tbody className={clsx("border-b border-line", dim && "opacity-60")} data-testid={`book-${b.bot}`}>
      <BookRow label={`${b.unit} · All options`} delta={b.options} unit={b.unit} spot={b.spot} note={optionsNote} testId={`book-options-${b.bot}`} />
      {h ? (
        <>
          <BookRow
            label={`Futures hedge · ${h.instrument} ${h.perpUsd < 0 ? "short" : "long"} ${formatUSD(Math.abs(h.perpUsd ?? 0))}`}
            delta={h.perpDelta}
            unit={b.unit}
            spot={b.spot}
            note={hedgeGainWords(h.perpDelta, b.unit)}
            testId={`book-hedge-${b.bot}`}
          />
          <BookRow
            label="Book"
            delta={h.book}
            unit={b.unit}
            spot={b.spot}
            note={`${b.unit} count held flat: in coin ${coin(h.netCoin)}, ${h.inside ? "inside" : "outside"} the hedge band ±${h.band.toFixed(3)}`}
            title={bookTip(b.options, h, b.unit)}
            testId={`book-total-${b.bot}`}
          />
        </>
      ) : null}
    </tbody>
  );
}

/** "|Δ| per contract → exit": a bar up to the delta exit with an amber tick at its end; amber from 80 % of it. */
export function DeltaExitCell({ delta, threshold }) {
  const ratio = deltaExitRatio(delta, threshold);
  const warn = ratio != null && ratio >= DELTA_EXIT_WARN;
  return (
    <span className={clsx("inline-flex items-center gap-1", warn && "text-warn")} data-testid="delta-exit" data-warn={warn ? "1" : "0"}>
      {ratio != null ? <MicroBar value={Math.abs(delta)} min={0} max={threshold} tick={threshold} tone={warn ? "warn" : "neutral"} /> : null}
      <span className="tabular-nums">
        {isNumber(delta) ? Math.abs(delta).toFixed(2) : "—"}
        {isNumber(threshold) ? <span className="text-muted"> / {threshold.toFixed(2)}</span> : null}
      </span>
    </span>
  );
}

/** "Premium kept": a diverging bar around 0 (−100..+100 %) and the %. */
function KeptCell({ pct }) {
  return (
    <span className="inline-flex items-center gap-1">
      <MicroBar value={pct} min={-100} max={100} zero={0} tone={pct < 0 ? "loss" : "profit"} />
      <span className={pnlClass(pct)}>{formatPct(pct, 0, { signed: true })}</span>
    </span>
  );
}

/** "3 d" / "5 h" / "20 m" since a leg was opened; null when unknown. */
export function heldFor(iso, now = Date.now()) {
  const s = ageSeconds(iso, now);
  if (s == null) return null;
  if (s < 3600) return `${Math.floor(s / 60)} m`;
  if (s < 2 * 86400) return `${Math.floor(s / 3600)} h`;
  return `${Math.floor(s / 86400)} d`;
}

/** The leg's opened date and how long ago; the full time (and the last top-up) in its title. */
function OpenedCell({ row }) {
  if (!row.openedAt) {
    return (
      <span className="text-muted" title="Opened before the trade history the monitor loaded">
        —
      </span>
    );
  }
  const title = `Opened ${formatDateTime(row.openedAt)}${row.lastAddedAt ? ` · last added ${formatDateTime(row.lastAddedAt)}` : ""}`;
  return (
    <span title={title} data-testid="opened">
      {formatDate(row.openedAt)} <span className="text-[11px] text-muted">{heldFor(row.openedAt)}</span>
    </span>
  );
}

const gammaE5 = (v) => (isNumber(v) ? (v * 1e5).toFixed(1) : "—");

/** The bot's state for a group's label: down, stale with its age, or nothing. */
function botNote(health) {
  if (!health) return null;
  if (health.error && !health.status) return `bot down: ${health.error}`;
  if (health.stale) return `stale · last answer ${formatAge(health.ageSec ?? null)}`;
  return null;
}

/**
 * Open legs, one line each (Details (pro), #405), grouped (by strangle by
 * default: "BTC · 25-day slot · expires 13 Nov · 16 days"), with the group's
 * P&L in its coin and its net greeks. A leg row: chip + strike (the full
 * instrument in its title; a click opens the strangle's payoff chart with
 * onOpenChart(bot, strangleId, element), #391), opened (date and age, #614),
 * qty, entry → mark, premium
 * kept, P&L in coin ≈ USD, position Δ (#715), |Δ| per contract → exit, OTM %,
 * stop, Θ/day, Vega, Γ ×10⁻⁵ and Close… only where the bot allows a manual
 * close. held: missing legs by "<bot>:<strangle id>" (heldRows), each shown as
 * its own row in its strangle. health: per bot { stale, error, ageSec, status }
 * for dimmed or down groups. books: per bot lib/details bookDelta, the foot
 * rows (all options, the futures hedge, the book).
 */
export default function PositionsTable({ groups, statuses = {}, onOpenChart, held = {}, health = {}, books = [] }) {
  const [closing, setClosing] = useState(null); // the row whose close is being confirmed
  if (!groups.length) {
    return <p className="px-1 py-2 text-sm text-muted">No open positions match the filters.</p>;
  }
  const allRows = groups.flatMap((g) => g.rows);
  const threshold = allRows.find((r) => isNumber(r.deltaExit))?.deltaExit;
  const exitTip = `One contract's delta, without its sign: the number the delta exit${isNumber(threshold) ? ` (${threshold.toFixed(2)})` : ""} is checked on. Amber from 80 % of it. It is not the position's delta: don't add it up — use Position Δ.`;
  return (
    <div className="max-h-[70vh] overflow-auto">
      <table className="w-full whitespace-nowrap text-sm">
        <thead className="sticky top-0 z-10 bg-panel text-left text-xs uppercase text-muted">
          <tr className="border-b border-line">
            <th className={TD}>Leg</th>
            <th className={TD} title="When the bot sold this leg: the first sale on this strike since it last held none. A leg opened by a roll or a repair is new: its P&L often starts a little negative, from the opening fees and the spread, and improves as time passes.">
              Opened
            </th>
            <th className="px-2 py-1 text-right">Qty</th>
            <th className="px-2 py-1 text-right" title="Entry price → the mark used to value the leg (mid of the live ticker)">
              Entry → mark
            </th>
            <th className={TD} title="Unrealized P&L as % of the premium: +50% = half the premium kept; −100% = the loss equals the premium. Net of opening fees.">
              Premium kept
            </th>
            <th className="px-2 py-1 text-right" title="Net: premium received minus the cost to buy back at the mark and the fees paid to open; in the bot's coin, ≈ USD at spot">
              Unrealized P&L
            </th>
            <th className="px-2 py-1 text-right" title={positionDeltaTip(allRows)}>
              Position Δ
            </th>
            <th className={TD} title={exitTip}>
              |Δ| per contract → exit
            </th>
            <th className="px-2 py-1 text-right">OTM %</th>
            <th className="px-2 py-1 text-right" title="Mark at which the stop-loss fires">
              Stop at
            </th>
            <th className="px-2 py-1 text-right">Θ/day</th>
            <th className="px-2 py-1 text-right">Vega</th>
            <th className="px-2 py-1 text-right">Γ ×10⁻⁵</th>
            <th className={TD}>
              <span className="sr-only">Manual close</span>
            </th>
          </tr>
        </thead>
        {groups.map((g) => {
          const h = health[g.bot];
          const note = botNote(h);
          const dim = Boolean(h?.stale);
          const missing = held[`${g.bot}:${g.strangleId}`] ?? [];
          const coins = Object.entries(g.pnlByCoin ?? {});
          return (
            <tbody key={g.key} className={clsx("border-b border-line", dim && "opacity-60")} data-testid={`group-${g.key}`}>
              <tr className="bg-slate-800/40">
                <td colSpan={5} className="px-2 py-1 font-semibold">
                  {g.key} <span className="text-xs font-normal text-muted">· {g.legs} legs</span>
                  {note ? <span className="ml-2 text-xs font-normal text-warn">{note}</span> : null}
                </td>
                <td className="px-2 py-1 text-right font-semibold">
                  {coins.length
                    ? coins.map(([unit, v], i) => (
                        <span key={unit}>
                          {i ? " · " : ""}
                          <CoinPnl value={v} unit={unit} usd={coins.length === 1 ? g.pnlUsd : null} />
                        </span>
                      ))
                    : "—"}
                </td>
                <td className="px-2 py-1 text-right text-xs" title="The sum of the legs' position Δ: each leg's Δ × qty, negative for a short call, positive for a short put">
                  <PositionDeltaCell delta={g.unit ? netPositionDelta(g.rows) : null} unit={g.unit} spot={g.rows[0].spot} prefix="net " suffix=" per +1 %" />
                </td>
                <td colSpan={3} />
                <td className={clsx(NUM, "font-semibold")} title="Θ of the group: each leg's × qty, negative for shorts">
                  {formatNumber(g.greeks.theta, 1)}
                </td>
                <td className={clsx(NUM, "font-semibold")}>{formatNumber(g.greeks.vega, 1)}</td>
                <td className={clsx(NUM, "font-semibold")}>{gammaE5(g.greeks.gamma)}</td>
                <td />
              </tr>
              {g.rows.map((r) => (
                <tr key={r.key} className="hover:bg-slate-800/30" data-testid={`leg-${r.key}`}>
                  <td className={TD}>
                    <span className="inline-flex items-center gap-1">
                      <Badge tone={r.type === "call" ? "call" : "put"}>{r.type}</Badge>
                      {onOpenChart ? (
                        <button
                          type="button"
                          aria-haspopup="dialog"
                          title={`${r.instrument}: open the payoff chart of this leg's strangle`}
                          onClick={(e) => onOpenChart(r.bot, r.strangleId, e.currentTarget)}
                          className="font-mono text-xs underline decoration-dotted underline-offset-2 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
                        >
                          {formatNumber(r.strike, 0)}
                        </button>
                      ) : (
                        <span className="font-mono text-xs" title={r.instrument}>
                          {formatNumber(r.strike, 0)}
                        </span>
                      )}
                      {r.side === "buy" ? <Badge tone="good">BUY</Badge> : null}
                      {r.positions > 1 ? (
                        <span className="text-[10px] text-muted" title={`${r.positions} positions on this strike, shown as one (entry is their average; stop is the first to fire)`}>
                          ×{r.positions}
                        </span>
                      ) : null}
                    </span>
                  </td>
                  <td className={TD}>
                    <OpenedCell row={r} />
                  </td>
                  <td className={NUM}>{r.qty}</td>
                  <td className={NUM}>
                    {formatPrice(r.entry)} → <Live value={r.mark}>{formatPrice(r.mark)}</Live>
                    <Freshness row={r} />
                  </td>
                  <td className={TD}>
                    <KeptCell pct={r.roiPct} />
                  </td>
                  <td className="px-2 py-1 text-right">
                    <Live value={r.pnl}>
                      <CoinPnl value={r.pnl} unit={r.unit} usd={r.pnlUsd} />
                    </Live>
                  </td>
                  <td className={NUM}>
                    <PositionDeltaCell delta={positionDelta(r)} unit={r.unit} spot={r.spot} />
                  </td>
                  <td className={TD}>
                    <DeltaExitCell delta={r.delta} threshold={r.deltaExit} />
                  </td>
                  <td className={clsx(NUM, r.moneyness === "ITM" && "text-loss", r.moneyness === "ATM" && "text-warn")}>
                    {r.moneyness !== "OTM" && r.moneyness !== "—" ? `${r.moneyness} ` : ""}
                    {isNumber(r.distancePct) ? `${r.distancePct.toFixed(1)}%` : "—"}
                  </td>
                  <td className={clsx(NUM, isNumber(r.lossMultiple) && r.lossMultiple > 1 && "text-warn")}>{formatPrice(r.stopMark)}</td>
                  <td className={NUM}>{formatNumber(r.theta, 1)}</td>
                  <td className={NUM}>{formatNumber(r.vega, 1)}</td>
                  <td className={NUM}>{gammaE5(r.gamma)}</td>
                  <td className={TD}>
                    <CloseButton row={r} onOpen={setClosing} />
                  </td>
                </tr>
              ))}
              {missing.map((m) => (
                <tr key={m.key} className="text-muted" data-testid={`missing-${m.key}`}>
                  <td colSpan={14} className="px-2 py-1 text-xs">
                    <Badge tone={m.missing === "call" ? "call" : "put"}>{m.missing}</Badge> not held: {holdRule(m.rule).label} ·{" "}
                    <a href="#held" className="underline underline-offset-2 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet">
                      Missing legs
                    </a>
                  </td>
                </tr>
              ))}
            </tbody>
          );
        })}
        {books.map((b) => (
          <BookRows key={b.bot} b={b} dim={Boolean(health[b.bot]?.stale)} />
        ))}
      </table>
      {closing ? (
        <ConfirmCloseModal row={closing} status={statuses[closing.bot]} onCancel={() => setClosing(null)} onDone={() => setClosing(null)} />
      ) : null}
    </div>
  );
}
