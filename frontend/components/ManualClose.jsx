import { useEffect, useState } from "react";
import clsx from "clsx";
import Badge from "./Badge";
import { closePositions } from "@/lib/api";
import { flipBand } from "@/lib/monitor";
import { formatCoin, formatPct, formatPrice, formatUSD, isNumber } from "@/lib/format";

/**
 * Close button of a position row. The bot decides: it is active only while
 * the regime-side block keeps the bot from selling this leg's side; otherwise
 * it is gray and its tooltip says why.
 */
export function CloseButton({ row, onOpen }) {
  const mc = row.manualClose;
  const allowed = Boolean(mc?.allowed) && row.side !== "buy";
  const tip = allowed
    ? `Close at market: ${mc.reason}`
    : mc?.reason || "manual close is not available on this bot";
  return (
    <span title={tip} data-testid={`close-${row.key}`}>
      <button
        type="button"
        disabled={!allowed}
        onClick={() => onOpen(row)}
        className={clsx(
          "rounded px-2 py-0.5 text-xs font-semibold",
          allowed ? "bg-loss/20 text-loss hover:bg-loss/30" : "cursor-not-allowed bg-slate-800 text-slate-500",
        )}
      >
        Close
      </button>
    </span>
  );
}

/** Confirmation before a manual close: what will be bought, the market regime, and what the bot does afterwards. */
export function ConfirmCloseModal({ row, status, onCancel, onDone }) {
  const [state, setState] = useState({ phase: "confirm" }); // confirm | sending | done | error
  const mc = row.manualClose ?? {};
  const band = flipBand(status);
  const rs = status?.risk?.status ?? {};
  const cost = isNumber(row.ask) ? row.ask * row.qty : null;
  const sending = state.phase === "sending";

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !sending && onCancel();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel, sending]);

  const send = async () => {
    setState({ phase: "sending" });
    try {
      const results = await closePositions(row.bot, row.positionIds);
      setState({ phase: "done", results });
    } catch (err) {
      setState({ phase: "error", error: err.message });
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="close-title">
      <div className="w-full max-w-lg rounded-lg border border-line bg-panel p-4 text-sm shadow-xl">
        <h2 id="close-title" className="mb-3 text-base font-bold">
          Close {row.instrument} × {row.qty} at market?
        </h2>

        <dl className="mb-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
          <dt className="text-muted">Position</dt>
          <dd>
            {row.bot.toUpperCase()} · {row.slot} · <Badge tone={row.type === "call" ? "call" : "put"}>{row.type}</Badge> short
            {row.positionIds.length > 1 ? ` · ${row.positionIds.length} positions` : ""}
          </dd>
          <dt className="text-muted">Buy back</dt>
          <dd className="tabular-nums">
            bid {formatPrice(row.bid)} / ask {formatPrice(row.ask)} · about {formatCoin(cost, row.unit)}
            {isNumber(cost) && isNumber(row.spot) ? ` (${formatUSD(cost * row.spot)})` : ""} at the ask
          </dd>
          <dt className="text-muted">Unrealized P&L</dt>
          <dd className={clsx("tabular-nums", row.pnl > 0 && "text-profit", row.pnl < 0 && "text-loss")}>
            {formatCoin(row.pnl, row.unit)} ({formatUSD(row.pnlUsd)}) — realized at the fill
          </dd>
          <dt className="text-muted">Gamma regime</dt>
          <dd>
            confirmed {rs.regime_negative ? <span className="font-semibold text-loss">NEGATIVE</span> : rs.regime_known ? "positive" : "unknown"} · live GEX{" "}
            {status?.market?.gex_regime || "—"}
          </dd>
          <dt className="text-muted">Gamma flip</dt>
          <dd className="tabular-nums">
            {band ? (
              <>
                {formatUSD(band.flip)}
                {band.spotToFlipPct !== null ? ` (spot ${formatPct(band.spotToFlipPct, 1, { signed: true })})` : ""}
                {band.low ? ` · ±1σ ${formatUSD(band.low)}–${formatUSD(band.high)}` : ""}
              </>
            ) : (
              "—"
            )}
          </dd>
          <dt className="text-muted">Trend</dt>
          <dd>{status?.trend ?? "—"}</dd>
        </dl>

        <p className="mb-2 rounded border border-profit/40 bg-profit/10 px-2 py-1 text-profit">Allowed: {mc.reason}</p>
        <p
          data-testid="close-after"
          className={clsx(
            "mb-3 rounded border px-2 py-1",
            mc.rebuilt ? "border-loss/60 bg-loss/15 font-semibold text-loss" : "border-line bg-slate-800/40 text-slate-200",
          )}
        >
          {mc.rebuilt ? "Would be rebuilt: " : "After the close: "}
          {mc.after}
        </p>

        {state.phase === "done" ? <Results results={state.results} /> : null}
        {state.phase === "error" ? <p className="mb-3 rounded bg-loss/15 px-2 py-1 text-loss">Not closed: {state.error}</p> : null}

        <div className="flex justify-end gap-2">
          {state.phase === "done" ? (
            <button type="button" onClick={onDone} className="rounded bg-slate-700 px-3 py-1 font-semibold">
              Close window
            </button>
          ) : (
            <>
              <button type="button" onClick={onCancel} disabled={sending} className="rounded bg-slate-700 px-3 py-1">
                Cancel
              </button>
              <button type="button" onClick={send} disabled={sending} className="rounded bg-loss px-3 py-1 font-semibold text-white disabled:opacity-60">
                {sending ? "Sending…" : "Buy back at market"}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function Results({ results }) {
  return (
    <ul className="mb-3 space-y-1" data-testid="close-results">
      {results.map((r) => (
        <li key={r.position_id} className={r.error ? "text-loss" : "text-profit"}>
          {r.instrument || r.position_id}: {r.error ? `not closed — ${r.error}` : `bought back ${r.filled} of ${r.qty}`}
          {!r.error && r.filled < r.qty ? " (the rest stays open)" : ""}
        </li>
      ))}
    </ul>
  );
}
