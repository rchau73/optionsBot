"use client";

import { useEffect, useState } from "react";
import clsx from "clsx";
import Badge from "./Badge";
import { fetchBotHedge } from "@/lib/api/bots";
import { setFuturesHedge } from "@/lib/api/actions";
import { useBlockingDialog } from "@/lib/dialogs";
import { formatCoin, formatDateTime, formatPct, formatUSD } from "@/lib/format";

/** The card polls no faster than this: the hedge moves once per 40 s cycle. */
export const HEDGE_POLL_MS = 10000;

const signedCoin = (v, unit, digits = 4) => (v == null ? "—" : `${v > 0 ? "+" : v < 0 ? "−" : ""}${formatCoin(Math.abs(v), unit, digits)}`);
const signedUSD = (v) => (v == null ? "—" : `${v > 0 ? "+" : v < 0 ? "−" : ""}${formatUSD(Math.abs(v))}`);
const tone = (v) => (v > 0 ? "text-profit" : v < 0 ? "text-loss" : "");

/**
 * The futures hedge experiment of one bot (#657), on the Account page: the
 * mode, the perpetual, the book's delta and the benchmark against the GEX
 * shed, with the switch (admins, testnet). Polls GET /api/bots/<bot>/hedge.
 */
export default function FuturesHedge({ bot }) {
  const [state, setState] = useState({ data: null, error: null });
  const [dialog, setDialog] = useState(null); // "on" | "off" | null
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let timer;
    const controller = new AbortController();
    const load = async () => {
      try {
        const data = await fetchBotHedge(bot, false, controller.signal);
        if (!controller.signal.aborted) setState({ data, error: null });
      } catch (e) {
        if (!controller.signal.aborted) setState((s) => ({ ...s, error: e.message }));
      }
      if (!controller.signal.aborted) timer = setTimeout(load, HEDGE_POLL_MS);
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot, tick]);

  return (
    <>
      <FuturesHedgeView bot={bot} data={state.data} error={state.error} onSwitch={setDialog} />
      {dialog ? (
        <FuturesHedgeDialog
          bot={bot}
          enable={dialog === "on"}
          hedge={state.data?.hedge}
          onClose={() => {
            setDialog(null);
            setTick((t) => t + 1); // read the new state at once
          }}
        />
      ) : null}
    </>
  );
}

/** Pure render of the card, for tests. */
export function FuturesHedgeView({ bot, data, error, onSwitch }) {
  const h = data?.hedge;
  if (!h) {
    return (
      <div className="rounded border border-line p-3 text-sm" data-testid={`hedge-${bot}`}>
        <h3 className="font-semibold">{bot.toUpperCase()} · Futures hedge (experiment)</h3>
        <p className="text-muted">{error ? `Unknown: ${error}.` : "Loading…"}</p>
      </div>
    );
  }
  const unit = h.underlying || bot.toUpperCase();
  const b = h.benchmark ?? {};
  const why = !h.available ? h.unavailable : h.halted ? "the kill switch has fired" : !data.switchable ? data.switch_off_reason || "the switch is off on this bot" : null;
  return (
    <div className="space-y-2 rounded border border-line p-3 text-sm" data-testid={`hedge-${bot}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-semibold">
          {unit} · Futures hedge (experiment) <Badge tone={h.enabled ? "good" : "muted"}>{h.enabled ? "ON" : "OFF"}</Badge>
        </h3>
        <span title={why ?? (h.enabled ? "Preview, then close the perpetual and switch off" : "Switch on: the GEX shed keeps its legs, a future offsets their direction")}>
          <button
            type="button"
            disabled={Boolean(why)}
            onClick={() => onSwitch?.(h.enabled ? "off" : "on")}
            className={clsx(
              "min-h-11 rounded px-3 text-sm font-semibold",
              why ? "cursor-not-allowed bg-slate-800 text-slate-500" : "bg-action/20 text-action hover:bg-action/30",
            )}
          >
            {h.enabled ? "Switch off…" : "Switch on…"}
          </button>
        </span>
      </div>
      <p className="text-muted">
        {h.enabled
          ? `On since ${formatDateTime(h.since)}${h.by ? ` (by ${h.by})` : ""}: the GEX shed keeps its legs and ${h.instrument} holds the book's direction within ±${formatCoin(h.band_coin, unit, 4)}. Stop-loss, delta exit and the margin cut still close legs.`
          : "Off: the GEX shed buys the threatened side back, as usual. The hedge below runs on paper only, for the comparison."}
        {h.shed_now ? ` The GEX shed is active on ${h.shed_now} now.` : ""}
      </p>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 tabular-nums sm:grid-cols-4">
        <div>
          <dt className="text-xs text-muted">Options delta</dt>
          <dd>{signedCoin(h.options_delta_coin, unit)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Perpetual</dt>
          <dd>
            {signedCoin(h.perp_delta_coin, unit)} <span className="text-xs text-muted">({signedUSD(h.perp_usd)})</span>
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Net delta</dt>
          <dd>{signedCoin(h.net_delta_coin, unit)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Hedge orders, last hour</dt>
          <dd>
            {h.orders_last_hour} of {h.max_orders_per_hour}
          </dd>
        </div>
      </dl>
      <div className="rounded bg-slate-900/40 p-2" data-testid={`hedge-benchmark-${bot}`}>
        <p className="text-xs uppercase tracking-wide text-muted">Hedge vs GEX shed{b.since ? ` since ${formatDateTime(b.since)}` : ""}</p>
        <p className={clsx("text-lg font-bold tabular-nums", tone(b.advantage_coin))}>
          {signedCoin(b.advantage_coin, unit, 6)} <span className="text-sm font-normal">({signedUSD(b.advantage_usd)})</span>
        </p>
        <p className="text-xs text-muted">
          {b.advantage_coin > 0 ? "The hedge did better than the shed." : b.advantage_coin < 0 ? "The shed did better than the hedge." : "No difference yet."} Hedge{" "}
          {signedCoin(b.hedge_pnl_coin, unit, 6)} ({b.real_trades ?? 0} real, {b.virtual_trades ?? 0} paper trades) · holding the shed legs{" "}
          {signedCoin(b.legs_coin, unit, 6)} ({b.legs?.length ?? 0} legs).
        </p>
        {b.legs?.length ? (
          <ul className="mt-1 space-y-0.5 text-xs tabular-nums">
            {b.legs
              .slice(-5)
              .reverse()
              .map((l) => (
                <li key={`${l.at}-${l.instrument}`}>
                  {formatDateTime(l.at)} · {l.mode === "hedged" ? "kept" : "shed"} {l.instrument} × {l.qty} at {l.shed_price?.toFixed(4)} → {l.last_price?.toFixed(4)}
                  {l.done ? " (ended)" : ""}
                </li>
              ))}
          </ul>
        ) : null}
        <p className="mt-1 text-xs text-muted">{b.note}</p>
      </div>
      {error ? <p className="text-xs text-warn">Not updating: {error}.</p> : null}
    </div>
  );
}

/** The switch: on states what changes; off previews the close and the risk it leaves, then asks. */
export function FuturesHedgeDialog({ bot, enable, hedge, onClose }) {
  const [state, setState] = useState({ phase: enable ? "ready" : "preview" }); // preview | ready | running | done | error
  useBlockingDialog();
  const busy = state.phase === "preview" || state.phase === "running";

  useEffect(() => {
    if (enable) return undefined;
    let open = true;
    fetchBotHedge(bot, true).then(
      // A failed preview must not trap the admin: the switch-off stays possible, with a warning.
      (d) => open && setState(d.preview ? { phase: "ready", preview: d.preview } : { phase: "ready", previewError: d.preview_error ?? "no preview" }),
      (e) => open && setState({ phase: "ready", previewError: e.message }),
    );
    return () => {
      open = false;
    };
  }, [bot, enable]);

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !busy && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, busy]);

  const run = async () => {
    setState((s) => ({ ...s, phase: "running" }));
    try {
      const result = await setFuturesHedge(bot, enable);
      setState((s) => ({ ...s, phase: "done", result }));
    } catch (err) {
      setState((s) => ({ ...s, phase: "error", error: err.message }));
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="hedge-title">
      <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg border border-line bg-panel p-4 text-sm shadow-xl">
        <h2 id="hedge-title" className="mb-3 text-base font-bold">
          {enable ? `Switch the futures hedge ON for ${bot.toUpperCase()}?` : `Switch the futures hedge OFF for ${bot.toUpperCase()}?`}
        </h2>
        {enable ? <SwitchOnText hedge={hedge} /> : null}
        {!enable && state.phase === "preview" ? <p className="mb-3 text-muted">Asking the bot what switching off would do (nothing is sent)…</p> : null}
        {!enable && state.preview ? <SwitchOffPreview p={state.preview} /> : null}
        {state.previewError ? (
          <p className="mb-3 rounded bg-warn/10 px-2 py-1 text-warn">
            The preview is unavailable: {state.previewError}. You can still switch off: any open perpetual is closed at market, without seeing its result, the margin after or the risk it
            leaves.
          </p>
        ) : null}
        {state.phase === "running" ? <p className="mb-3 text-muted">Switching…</p> : null}
        {state.phase === "error" ? <p className="mb-3 rounded bg-loss/15 px-2 py-1 text-loss">Not switched: {state.error}</p> : null}
        {state.phase === "done" && state.result ? <p className="mb-3 rounded bg-profit/10 px-2 py-1 text-profit">Done: {state.result.detail}</p> : null}
        <div className="flex justify-end gap-2">
          {state.phase === "done" || state.phase === "error" ? (
            <button type="button" onClick={onClose} className="min-h-11 rounded bg-slate-700 px-3 font-semibold">
              Close window
            </button>
          ) : (
            <>
              <button type="button" onClick={onClose} disabled={busy} className="min-h-11 rounded bg-slate-700 px-3">
                Cancel
              </button>
              <button type="button" onClick={run} disabled={busy} className="min-h-11 rounded bg-action px-3 font-semibold text-white disabled:opacity-60">
                {state.phase === "running" ? "Sending…" : enable ? "Switch on" : "Close the perpetual and switch off"}
              </button>
            </>
          )}
        </div>
        <p className="mt-2 text-xs text-muted">Testnet study, admins only · a model, not a quote · educational, not investment advice</p>
      </div>
    </div>
  );
}

function SwitchOnText({ hedge }) {
  const unit = hedge?.underlying ?? "";
  return (
    <ul className="mb-3 list-disc space-y-1 pl-5">
      <li>From the next cycle the GEX shed no longer buys a side back: those legs stay open.</li>
      <li>
        The bot trades {hedge?.instrument ?? "the perpetual"} whenever the book&apos;s direction (delta) is beyond ±{formatCoin(hedge?.band_coin, unit, 4)}, to bring
        it back to zero: at most {hedge?.max_orders_per_hour ?? 12} orders an hour, each at the best price on the book.
      </li>
      <li>The hedge removes direction only: a big move still costs through gamma, and each re-hedge pays a fee and the spread.</li>
      <li>Stop-loss, delta exit, the margin cut and the kill switch (which also closes the perpetual) work as before.</li>
    </ul>
  );
}

// The preview table compares the book now with the book after the switch (rows say what).
const PREVIEW_COLUMNS = ["", "Now", "After switching off"];

function SwitchOffPreview({ p }) {
  const unit = p.underlying;
  const closing = Math.abs(p.perp_usd ?? 0) >= 0.5;
  return (
    <div className="mb-3 space-y-2" data-testid="hedge-preview">
      <p className="font-semibold">
        {closing
          ? `This ${p.perp_usd < 0 ? "buys back" : "sells"} ${formatUSD(Math.abs(p.perp_usd))} of ${p.instrument} at market (fee ≈ ${formatCoin(p.close_fee_coin, unit, 6)}, ${formatUSD(p.close_fee_usd, { cents: true })}).`
          : `No ${p.instrument} position is open: switching off closes nothing.`}
      </p>
      {closing ? (
        <p className="tabular-nums">
          Its result now: <span className={tone(p.unrealised_coin)}>{signedCoin(p.unrealised_coin, unit, 6)}</span> ({signedUSD(p.unrealised_usd)}), funding{" "}
          {signedCoin(p.funding_coin, unit, 6)}.
        </p>
      ) : null}
      <table className="w-full text-left tabular-nums">
        <thead className="text-xs text-muted">
          <tr>
            {PREVIEW_COLUMNS.map((c) => (
              <th key={c} className="py-1">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          <tr>
            <td className="py-0.5 text-muted">Delta</td>
            <td>{signedCoin(p.delta_now_coin, unit)}</td>
            <td>{signedCoin(p.delta_after_coin, unit)}</td>
          </tr>
          <tr>
            <td className="py-0.5 text-muted">Initial margin (limit {formatPct(p.limit_im_pct, 0)})</td>
            <td>{formatPct(p.im_pct_now, 1)}</td>
            <td>{p.margin_error ? "unknown" : formatPct(p.im_pct_after, 1)}</td>
          </tr>
          <tr>
            <td className="py-0.5 text-muted">Maintenance margin (limit {formatPct(p.max_mm_pct, 0)})</td>
            <td>{formatPct(p.mm_pct_now, 1)}</td>
            <td>{p.margin_error ? "unknown" : formatPct(p.mm_pct_after, 1)}</td>
          </tr>
        </tbody>
      </table>
      {p.margin_error ? <p className="text-xs text-warn">Margin after the close is unknown: {p.margin_error}</p> : null}
      <div>
        <p className="text-xs text-muted">The risk it leaves: the options&apos; result for a price move, without the hedge (a delta–gamma estimate)</p>
        <ul className="grid grid-cols-2 gap-1 tabular-nums sm:grid-cols-4">
          {p.scenarios?.map((s) => (
            <li key={s.move_pct} className="rounded border border-line px-2 py-1">
              <span className="text-xs text-muted">{s.move_pct > 0 ? "+" : "−"}{Math.abs(s.move_pct)} %</span>
              <br />
              <span className={tone(s.pnl_coin)}>{signedCoin(s.pnl_coin, unit)}</span>
              <br />
              <span className="text-xs">{signedUSD(s.pnl_usd)}</span>
            </li>
          ))}
        </ul>
      </div>
      {p.warnings?.length ? (
        <ul className="space-y-1">
          {p.warnings.map((w) => (
            <li key={w} className="rounded bg-warn/10 px-2 py-1 text-warn">
              {w}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
