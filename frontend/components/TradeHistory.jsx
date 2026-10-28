"use client";

import clsx from "clsx";
import { Fragment, useMemo, useState } from "react";
import Badge from "./Badge";
import DotStrip from "./DotStrip";
import { formatDateTime, formatNumber, formatPct, formatPrice, formatUSD } from "@/lib/format";
import { approxUSD, firstSentence, formatCoinFirst, shortLeg } from "@/lib/details";
import { closeLabel, filterTrades, positionDelta, summarizeTrades } from "@/lib/trades";
import { track } from "@/lib/track";

const PAGE = 50;
const pnlClass = (v) => (v > 0 ? "text-profit" : v < 0 ? "text-loss" : "");
const KINDS = [
  ["", "All"],
  ["open", "Opens"],
  ["close", "Closes"],
];
const COLUMNS = 12;

/** One-line state for a collapsed header. */
export function tradeHistorySummary(trades) {
  const s = summarizeTrades(trades);
  if (!s.closes && !s.opens) return "no trades yet";
  const rate = s.winRate == null ? "" : ` · ${formatPct(s.winRate, 0)} wins`;
  return `${s.opens} opens · ${s.closes} closes · realized ${formatUSD(s.realisedUsd)}${rate}`;
}

const reasonOf = (t) => (t.kind === "open" ? t.reason : closeLabel(t.reason));

/** The summary line: realized per coin (≈ USD under), win rate with its dots, avg win ÷ avg loss, opens. */
function SummaryLine({ s }) {
  const ratio = s.avgWinUsd && s.avgLossUsd ? Math.abs(s.avgWinUsd / s.avgLossUsd) : null;
  return (
    <dl className="flex flex-wrap items-start gap-x-6 gap-y-2 text-sm" data-testid="trades-summary">
      <div>
        <dt className="text-xs text-muted">Realized (net of fees)</dt>
        <dd className="flex gap-4">
          {s.coins.length
            ? s.coins.map((c) => (
                <span key={c} className="tabular-nums">
                  <span className={pnlClass(s.realisedByCoin[c])}>{formatCoinFirst(s.realisedByCoin[c], c)}</span>
                  <span className="block text-[11px] text-muted">{approxUSD(s.realisedUsdByCoin[c])}</span>
                </span>
              ))
            : "—"}
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted">Win rate</dt>
        <dd className="flex items-center gap-2 tabular-nums">
          <span>{s.winRate == null ? "—" : formatPct(s.winRate, 0)}</span>
          {s.closes ? <DotStrip wins={s.wins} total={s.closes} /> : null}
        </dd>
      </div>
      <div>
        <dt className="text-xs text-muted" title="Average winning close ÷ average losing close, each in USD at its own spot">
          Avg win ÷ avg loss
        </dt>
        <dd className="tabular-nums">{ratio == null ? "—" : `${formatNumber(ratio, 2)}×`}</dd>
      </div>
      <div>
        <dt className="text-xs text-muted">Opens</dt>
        <dd className="tabular-nums">{s.opens}</dd>
      </div>
    </dl>
  );
}

/** By exit reason (a column per coin) or by slot (realized in that slot's coin). */
function GroupTable({ title, groups, coins, perCoin }) {
  return (
    <table className="w-full text-sm">
      <thead className="text-left text-xs uppercase text-muted">
        <tr className="border-b border-line">
          <th className="px-2 py-1">{title}</th>
          <th className="px-2 py-1 text-right">Closes</th>
          <th className="px-2 py-1">Won</th>
          {perCoin ? (
            coins.map((c) => (
              <th key={c} className="px-2 py-1 text-right">
                {c}
              </th>
            ))
          ) : (
            <th className="px-2 py-1 text-right">Realized</th>
          )}
        </tr>
      </thead>
      <tbody>
        {groups.map((g) => (
          <tr key={g.key} className="border-b border-line/50">
            <td className="px-2 py-1">{g.key}</td>
            <td className="px-2 py-1 text-right tabular-nums">{g.closes}</td>
            <td className="px-2 py-1">
              <DotStrip wins={g.wins} total={g.closes} />
            </td>
            {perCoin ? (
              coins.map((c) => (
                <td key={c} className={clsx("px-2 py-1 text-right tabular-nums", pnlClass(g.coins[c]))}>
                  {c in g.coins ? formatCoinFirst(g.coins[c], c) : "—"}
                </td>
              ))
            ) : (
              <td className="px-2 py-1 text-right tabular-nums">
                {Object.entries(g.coins).map(([c, v]) => (
                  <span key={c} className={pnlClass(v)}>
                    {formatCoinFirst(v, c)}
                  </span>
                ))}
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** One trade: a single line, its caret opening the detail row (the full plain words, the rule and its numbers, greeks and instrument). */
function TradeRow({ t, open, onToggle }) {
  const reason = reasonOf(t);
  const lead = firstSentence(t.plain);
  const detailId = `trade-detail-${t.bot}-${t.seq}`;
  return (
    <Fragment>
      <tr className="border-b border-line/50" data-testid={`trade-${t.bot}-${t.seq}`}>
        <td className="px-1 py-1">
          <button
            type="button"
            aria-expanded={open}
            aria-controls={detailId}
            aria-label={`${open ? "Hide" : "Show"} details of the ${t.kind} of ${t.instrument}`}
            onClick={onToggle}
            className="inline-flex h-6 w-6 items-center justify-center rounded text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
          >
            <span aria-hidden="true" className={clsx("transition-transform", open && "rotate-90")}>
              ▸
            </span>
          </button>
        </td>
        <td className="px-2 py-1 text-xs tabular-nums text-muted">{formatDateTime(t.at)}</td>
        <td className="px-2 py-1 uppercase">{t.bot}</td>
        <td className="px-2 py-1">
          <Badge tone={t.kind === "open" ? "neutral" : "muted"}>{t.kind === "open" ? "OPEN" : "CLOSE"}</Badge>
        </td>
        <td className="px-2 py-1 font-mono text-xs" title={t.instrument}>
          {shortLeg(t.instrument)}
        </td>
        <td className="px-2 py-1 text-xs text-muted">{t.slot ? `${t.slot.dte}d · Δ${t.slot.delta}` : "—"}</td>
        <td className="px-2 py-1 text-right tabular-nums">{formatNumber(t.qty, 1)}</td>
        <td className="px-2 py-1 text-right tabular-nums">{formatPrice(t.price)}</td>
        <td className="max-w-[24rem] truncate px-2 py-1 text-xs" title={t.plain || reason}>
          <Badge tone={t.kind === "open" ? "neutral" : "muted"}>{reason}</Badge> {lead}
        </td>
        <td className={clsx("px-2 py-1 text-right tabular-nums", pnlClass(t.pnl))}>
          {t.kind === "close" ? (
            <>
              {formatCoinFirst(t.pnl, t.unit)} <span className="text-[11px] text-muted">{approxUSD(t.pnl_usd)}</span>
            </>
          ) : (
            "—"
          )}
        </td>
        <td className="px-2 py-1 text-right tabular-nums">{t.kind === "close" ? formatPct(t.roi_pct, 0, { signed: true }) : "—"}</td>
        <td className="px-2 py-1 text-right text-xs tabular-nums text-muted">
          {formatNumber(t.spot, 0)} · {formatNumber(t.dvol, 1)}
        </td>
      </tr>
      <tr id={detailId} hidden={!open} className="border-b border-line/50 bg-slate-800/30">
        <td colSpan={COLUMNS} className="whitespace-normal px-3 py-2 text-xs">
          {t.plain ? <p className="text-slate-200">{t.plain}</p> : null}
          <p className="mt-1 font-mono text-[11px] text-muted">
            {reason}
            {t.detail ? `: ${t.detail}` : ""}
          </p>
          <p className="mt-1 tabular-nums text-muted">
            <span className="font-mono">{t.instrument}</span> · Δ {formatNumber(t.delta, 3)} (position {formatNumber(positionDelta(t), 2)}) · Θ {formatNumber(t.theta, 1)} · Vega{" "}
            {formatNumber(t.vega, 1)}
            {t.kind === "close" ? ` · held ${t.hold_days ?? 0}d` : ""}
          </p>
        </td>
      </tr>
    </Fragment>
  );
}

/**
 * Trades and results (Details (pro), #405): the summary line (realized per
 * coin ≈ USD, win rate with its dots, avg win ÷ avg loss, opens), by exit
 * reason and by slot side by side (they stack on a phone), then the log, one
 * line per trade, newest first, each opening a detail row. Restored from the
 * bots' journals on restart. The filters read the underlying fields, never
 * the sentence.
 */
export default function TradeHistory({ trades }) {
  const [filters, setFilters] = useState({ bot: "", kind: "", reason: "" });
  const [shown, setShown] = useState(PAGE);
  const [open, setOpen] = useState(() => new Set());
  const s = useMemo(() => summarizeTrades(trades), [trades]);
  const rows = useMemo(() => filterTrades(trades, filters), [trades, filters]);
  const bots = [...new Set(trades.map((t) => t.bot))];
  const reasons = s.byReason.map((g) => g.key);
  const set = (k, v) => {
    setFilters((f) => ({ ...f, [k]: v }));
    setShown(PAGE);
  };
  const toggle = (id) => {
    if (!open.has(id)) track("details_trade_expand");
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  if (!trades.length) return <p className="py-2 text-sm text-muted">No opens or closes in the journal yet.</p>;
  return (
    <div className="space-y-3">
      <SummaryLine s={s} />

      <div className="grid gap-3 lg:grid-cols-2">
        <GroupTable title="By exit reason" groups={s.byReason} coins={s.coins} perCoin />
        <GroupTable title="By slot" groups={s.bySlot} coins={s.coins} />
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <div role="group" aria-label="Show" className="flex overflow-hidden rounded border border-line">
          {KINDS.map(([v, label]) => (
            <button
              key={label}
              type="button"
              aria-pressed={filters.kind === v}
              onClick={() => set("kind", v)}
              className={clsx("px-2 py-0.5 text-xs focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet", filters.kind === v ? "bg-slate-700 text-white" : "text-muted")}
            >
              {label}
            </button>
          ))}
        </div>
        <label className="text-muted">
          Bot{" "}
          <select value={filters.bot} onChange={(e) => set("bot", e.target.value)} className="rounded bg-slate-800 px-1 py-0.5">
            <option value="">all</option>
            {bots.map((b) => (
              <option key={b} value={b}>
                {b.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
        <label className="text-muted">
          Exit reason{" "}
          <select value={filters.reason} onChange={(e) => set("reason", e.target.value)} className="rounded bg-slate-800 px-1 py-0.5">
            <option value="">any</option>
            {reasons.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
        </label>
        <span className="text-xs text-muted">{rows.length} rows</span>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full whitespace-nowrap text-sm">
          <thead className="text-left text-xs uppercase text-muted">
            <tr className="border-b border-line">
              <th className="px-1 py-1">
                <span className="sr-only">Details</span>
              </th>
              <th className="px-2 py-1">Time</th>
              <th className="px-2 py-1">Bot</th>
              <th className="px-2 py-1">Type</th>
              <th className="px-2 py-1">Leg</th>
              <th className="px-2 py-1">Slot</th>
              <th className="px-2 py-1 text-right">Qty</th>
              <th className="px-2 py-1 text-right">Price</th>
              <th className="px-2 py-1">Reason</th>
              <th className="px-2 py-1 text-right">P&amp;L</th>
              <th className="px-2 py-1 text-right" title="P&L as % of the premium: the share of it kept">
                % kept
              </th>
              <th className="px-2 py-1 text-right">Spot · DVOL</th>
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, shown).map((t) => {
              const id = `${t.bot}:${t.seq}`;
              return <TradeRow key={id} t={t} open={open.has(id)} onToggle={() => toggle(id)} />;
            })}
          </tbody>
        </table>
      </div>
      {rows.length > shown ? (
        <button type="button" onClick={() => setShown((n) => n + PAGE)} className="rounded bg-slate-800 px-3 py-1 text-sm text-muted hover:text-white">
          Show {Math.min(PAGE, rows.length - shown)} more
        </button>
      ) : null}
    </div>
  );
}
