"use client";

import { useMemo, useState } from "react";
import Badge from "./Badge";
import StatTile from "./StatTile";
import { formatCoin, formatDateTime, formatNumber, formatPct, formatPrice, formatUSD } from "@/lib/format";
import { closeLabel, filterTrades, summarizeTrades } from "@/lib/trades";

const PAGE = 50;
const tone = (v) => (v > 0 ? "good" : v < 0 ? "bad" : undefined);
const pnlClass = (v) => (v > 0 ? "text-profit" : v < 0 ? "text-loss" : "");

/** One-line state for the collapsed panel header. */
export function tradeHistorySummary(trades) {
  const s = summarizeTrades(trades);
  if (!s.closes && !s.opens) return "no trades yet";
  const rate = s.winRate == null ? "" : ` · ${formatPct(s.winRate, 0)} wins`;
  return `${s.opens} opens · ${s.closes} closes · realized ${formatUSD(s.realisedUsd)}${rate}`;
}

/**
 * How the strategy is doing since its history began: realized P&L, win
 * rate, average win vs loss, P&L by exit reason and by slot, and every open
 * and close (newest first). Restored from the bots' journals on restart.
 */
export default function TradeHistory({ trades }) {
  const [filters, setFilters] = useState({ bot: "", kind: "", reason: "" });
  const [shown, setShown] = useState(PAGE);
  const s = useMemo(() => summarizeTrades(trades), [trades]);
  const rows = useMemo(() => filterTrades(trades, filters), [trades, filters]);
  const bots = [...new Set(trades.map((t) => t.bot))];
  const reasons = s.byReason.map((g) => g.key);
  const set = (k) => (e) => {
    setFilters((f) => ({ ...f, [k]: e.target.value }));
    setShown(PAGE);
  };

  if (!trades.length) return <p className="py-3 text-center text-sm text-muted">No opens or closes in the journal yet.</p>;
  const coins = Object.entries(s.realisedByCoin).map(([c, v]) => formatCoin(v, c, 4)).join(" · ");
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
        <StatTile label="Realized P&L" value={formatUSD(s.realisedUsd)} tone={tone(s.realisedUsd)} sub={coins || "USD at each close"} />
        <StatTile label="Closes" value={s.closes} sub={`${s.opens} opens`} />
        <StatTile label="Win rate" value={s.winRate == null ? "—" : formatPct(s.winRate, 0)} sub={`${s.wins} wins · ${s.losses} losses`} />
        <StatTile label="Avg win" value={s.avgWinUsd == null ? "—" : formatUSD(s.avgWinUsd)} tone="good" />
        <StatTile label="Avg loss" value={s.avgLossUsd == null ? "—" : formatUSD(s.avgLossUsd)} tone="bad" />
        <StatTile
          label="Win / loss size"
          value={s.avgWinUsd && s.avgLossUsd ? formatNumber(Math.abs(s.avgWinUsd / s.avgLossUsd), 2) : "—"}
          sub="avg win ÷ avg loss"
        />
      </div>

      <div className="grid gap-3 lg:grid-cols-2">
        {[
          ["By exit reason", s.byReason],
          ["By slot", s.bySlot],
        ].map(([title, groups]) => (
          <table key={title} className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-muted">
              <tr className="border-b border-line">
                <th className="px-2 py-1">{title}</th>
                <th className="px-2 py-1 text-right">Closes</th>
                <th className="px-2 py-1 text-right">Wins</th>
                <th className="px-2 py-1 text-right">P&amp;L (USD)</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <tr key={g.key} className="border-b border-line/50">
                  <td className="px-2 py-1">{g.key}</td>
                  <td className="px-2 py-1 text-right tabular-nums">{g.closes}</td>
                  <td className="px-2 py-1 text-right tabular-nums">{g.wins}</td>
                  <td className={`px-2 py-1 text-right tabular-nums ${pnlClass(g.pnlUsd)}`}>{formatUSD(g.pnlUsd)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <label className="text-muted">
          Bot{" "}
          <select value={filters.bot} onChange={set("bot")} className="rounded bg-slate-800 px-1 py-0.5">
            <option value="">all</option>
            {bots.map((b) => (
              <option key={b} value={b}>
                {b.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
        <label className="text-muted">
          Type{" "}
          <select value={filters.kind} onChange={set("kind")} className="rounded bg-slate-800 px-1 py-0.5">
            <option value="">opens and closes</option>
            <option value="open">opens</option>
            <option value="close">closes</option>
          </select>
        </label>
        <label className="text-muted">
          Exit reason{" "}
          <select value={filters.reason} onChange={set("reason")} className="rounded bg-slate-800 px-1 py-0.5">
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
        <table className="w-full text-sm">
          <thead className="text-left text-xs uppercase text-muted">
            <tr className="border-b border-line">
              <th className="px-2 py-1">Time</th>
              <th className="px-2 py-1">Bot</th>
              <th className="px-2 py-1">Type</th>
              <th className="px-2 py-1">Instrument</th>
              <th className="px-2 py-1">Slot</th>
              <th className="px-2 py-1 text-right">Qty</th>
              <th className="px-2 py-1 text-right">Price</th>
              <th className="px-2 py-1">Reason</th>
              <th className="px-2 py-1 text-right">P&amp;L</th>
              <th className="px-2 py-1 text-right">P&amp;L %</th>
              <th className="px-2 py-1 text-right">Held</th>
              <th className="px-2 py-1 text-right">Spot · DVOL</th>
            </tr>
          </thead>
          <tbody>
            {rows.slice(0, shown).map((t) => (
              <tr key={`${t.bot}:${t.seq}`} className="border-b border-line/50">
                <td className="whitespace-nowrap px-2 py-1 text-xs tabular-nums text-muted">{formatDateTime(t.at)}</td>
                <td className="px-2 py-1 uppercase">{t.bot}</td>
                <td className="px-2 py-1">
                  <Badge tone={t.kind === "open" ? "neutral" : "muted"}>{t.kind === "open" ? "OPEN" : "CLOSE"}</Badge>
                </td>
                <td className="px-2 py-1 font-mono text-xs">{t.instrument}</td>
                <td className="px-2 py-1 text-xs text-muted">{t.slot ? `${t.slot.dte}d · Δ${t.slot.delta}` : "—"}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatNumber(t.qty, 1)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{formatPrice(t.price)}</td>
                <td className="px-2 py-1 text-xs">{t.kind === "open" ? t.reason : closeLabel(t.reason)}</td>
                <td className={`px-2 py-1 text-right tabular-nums ${pnlClass(t.pnl)}`}>
                  {t.kind === "close" ? (
                    <>
                      {formatCoin(t.pnl, t.unit, 4)} <span className="text-xs text-muted">({formatUSD(t.pnl_usd)})</span>
                    </>
                  ) : (
                    "—"
                  )}
                </td>
                <td className="px-2 py-1 text-right tabular-nums">{t.kind === "close" ? formatPct(t.roi_pct, 0, { signed: true }) : "—"}</td>
                <td className="px-2 py-1 text-right tabular-nums">{t.kind === "close" ? `${t.hold_days ?? 0}d` : "—"}</td>
                <td className="whitespace-nowrap px-2 py-1 text-right text-xs tabular-nums text-muted">
                  {formatNumber(t.spot, 0)} · {formatNumber(t.dvol, 1)}
                </td>
              </tr>
            ))}
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
