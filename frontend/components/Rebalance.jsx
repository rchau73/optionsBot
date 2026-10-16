import { useEffect, useState } from "react";
import clsx from "clsx";
import Badge from "./Badge";
import { rebalanceBot } from "@/lib/api";
import { formatPct } from "@/lib/format";

/**
 * Rebalance button of a bot's margin-policy row. Useful after a deposit or a
 * withdrawal: the bot only rebalances by itself when the confirmed IM limit
 * changes. Gray while new risk is frozen or margin data is missing (the bot
 * re-checks every gate anyway).
 */
export function RebalanceButton({ row, onOpen }) {
  const why = row.error ? "no margin data from Deribit" : row.frozen ? `new risk frozen: ${row.freezeReason ?? ""}` : null;
  return (
    <span title={why ?? "Preview, then resize the book toward the IM limit with the current margin balance"}>
      <button
        type="button"
        disabled={Boolean(why)}
        onClick={() => onOpen(row)}
        className={clsx(
          "rounded px-2 py-0.5 text-xs font-semibold",
          why ? "cursor-not-allowed bg-slate-800 text-slate-500" : "bg-action/20 text-action hover:bg-action/30",
        )}
      >
        Rebalance
      </button>
    </span>
  );
}

const ACTION_LABEL = { top_up: "Top up", downsize: "Buy back", held: "Held", kept: "Kept", skipped: "Skipped" };
const ACTION_TONE = { top_up: "good", downsize: "bad", held: "warn", kept: "muted", skipped: "muted" };

/**
 * Opens with a dry run (nothing is sent), shows what the pass would do, and
 * runs it only on confirmation.
 */
export function RebalanceModal({ row, onClose }) {
  const [state, setState] = useState({ phase: "preview" }); // preview | previewed | running | done | error
  const busy = state.phase === "preview" || state.phase === "running";

  // The preview runs once on open; its result is dropped if the dialog closes first.
  useEffect(() => {
    let open = true;
    rebalanceBot(row.bot, true).then(
      (report) => open && setState({ phase: "previewed", report }),
      (err) => open && setState({ phase: "error", error: err.message, dryRun: true }),
    );
    return () => {
      open = false;
    };
  }, [row.bot]);

  const run = async () => {
    setState({ phase: "running" });
    try {
      setState({ phase: "done", report: await rebalanceBot(row.bot, false) });
    } catch (err) {
      setState({ phase: "error", error: err.message, dryRun: false });
    }
  };

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !busy && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, busy]);

  const report = state.report;
  const orders = report?.actions?.filter((a) => a.action === "top_up" || a.action === "downsize") ?? [];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="rebalance-title">
      <div className="w-full max-w-2xl rounded-lg border border-line bg-panel p-4 text-sm shadow-xl">
        <h2 id="rebalance-title" className="mb-3 text-base font-bold">
          Rebalance {row.bot.toUpperCase()} toward the IM limit?
        </h2>

        {state.phase === "preview" ? <p className="mb-3 text-muted">Asking the bot what it would do (nothing is sent)…</p> : null}
        {state.phase === "running" ? <p className="mb-3 text-muted">Rebalancing…</p> : null}
        {state.phase === "error" ? (
          <p className="mb-3 rounded bg-loss/15 px-2 py-1 text-loss">
            {state.dryRun ? "Preview refused" : "Not rebalanced"}: {state.error}
          </p>
        ) : null}

        {report ? (
          <>
            <p className="mb-2 tabular-nums">
              IM {formatPct(report.im_pct, 1)} of limit {formatPct(report.limit_im_pct, 0)} · share per slot{" "}
              {report.share_per_slot?.toFixed(6)} {report.unit} · margin balance {report.margin_balance?.toFixed(4)} {report.unit}
            </p>
            {state.phase === "done" ? (
              <p className={clsx("mb-2 rounded px-2 py-1", report.done ? "bg-profit/10 text-profit" : "bg-warn/10 text-warn")}>
                {report.done ? "Done." : "Partly done: the rest runs again automatically."} Working orders appear in the monitor.
              </p>
            ) : (
              <p className="mb-2 text-muted">
                {orders.length ? `Would send ${orders.length} order(s):` : "Nothing to send: the book already matches the limit."}
              </p>
            )}
            <ul className="mb-3 max-h-[max(16rem,40vh)] space-y-1 overflow-y-auto" data-testid="rebalance-actions">
              {report.actions.map((a, i) => (
                <li key={`${a.strangle_id}-${a.action}-${a.instrument ?? ""}-${i}`} className="flex flex-wrap items-baseline gap-2">
                  <Badge tone={ACTION_TONE[a.action] ?? "muted"}>{ACTION_LABEL[a.action] ?? a.action}</Badge>
                  <span className="text-muted">{a.slot}</span>
                  <span className="tabular-nums">
                    {a.action === "top_up" ? `${a.call} + ${a.put} × ${a.qty}` : a.instrument ? `${a.instrument} × ${a.qty}` : ""}
                  </span>
                  <span className="text-xs text-muted">{a.detail}</span>
                  {a.error ? <span className="text-xs text-loss">{a.error}</span> : null}
                </li>
              ))}
            </ul>
          </>
        ) : null}

        <div className="flex justify-end gap-2">
          {state.phase === "done" || state.phase === "error" ? (
            <button type="button" onClick={onClose} className="rounded bg-slate-700 px-3 py-1 font-semibold">
              Close window
            </button>
          ) : (
            <>
              <button type="button" onClick={onClose} disabled={busy} className="rounded bg-slate-700 px-3 py-1">
                Cancel
              </button>
              <button
                type="button"
                onClick={run}
                disabled={busy || orders.length === 0}
                className="rounded bg-action px-3 py-1 font-semibold text-white disabled:opacity-60"
              >
                {state.phase === "running" ? "Sending…" : "Run rebalance"}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
