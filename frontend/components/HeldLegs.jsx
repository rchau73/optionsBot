import clsx from "clsx";
import Badge from "./Badge";
import { MISSING, formatDateTime, formatPct, formatUSD, slotLabel } from "@/lib/format";
import { expiryLabel, flipBarGeometry } from "@/lib/details";
import { heldRows, holdRule } from "@/lib/held";

/** The 120 px bar: the flip ± buffer band shaded, the flip's tick, spot's dot. Hidden from screen readers: the numbers beside it say it. */
function FlipBar({ spot, flip, bufferPct }) {
  const g = flipBarGeometry(spot, flip, bufferPct);
  if (!g) return null;
  return (
    <span aria-hidden="true" data-testid="flip-bar" className="relative inline-block h-3 w-[120px] shrink-0 rounded-sm bg-slate-800 align-middle">
      <span className="absolute inset-y-0 bg-slate-600/60" style={{ left: `${g.bandLeft}%`, width: `${g.bandWidth}%` }} />
      <span className="absolute -inset-y-0.5 w-px bg-white" style={{ left: `${g.flip}%` }} />
      <span
        className={clsx("absolute top-1/2 h-2 w-2 -translate-x-1/2 -translate-y-1/2 rounded-full", g.below ? "bg-loss" : g.above ? "bg-profit" : "bg-warn")}
        style={{ left: `${g.spot}%` }}
      />
    </span>
  );
}

/** When repair may re-sell the leg, in plain words, with the times and snapshot counts the bot reports. */
export function releaseText(r) {
  const parts = [];
  const a = r.anchor;
  if (a?.mode === "bias") {
    parts.push(`cooldown until ${formatDateTime(a.cooldown_until)}, then the bias off the side on ${a.need} snapshots (${a.clear}/${a.need}) · shed at flip ${formatUSD(a.flip)}`);
  } else if (a) {
    parts.push(
      `spot above ${formatUSD(a.level)} (${r.toAnchorPct == null ? MISSING : formatPct(r.toAnchorPct, 1, { signed: true })}) on ${a.need} snapshots (${a.above}/${a.need}), or ${a.release_hours} h non-negative · shed at flip ${formatUSD(a.flip)}`,
    );
  }
  if (r.cooldown_until) parts.push(`not before ${formatDateTime(r.cooldown_until)}`);
  if (!parts.length) parts.push(r.reason || "repair may sell it this cycle");
  return parts.join("; ");
}

/**
 * Missing legs of one-legged strangles and why repair has not re-sold them
 * (Details (pro), #405), one row each: the leg (bot, side, slot, expiry, the
 * leg it pairs with), the hold, the market now, spot against the live flip +
 * buffer, and when it is released. With onOpenChart(bot, strangleId,
 * element), the paired leg opens its strangle's payoff chart (#391).
 */
export default function HeldLegs({ bots, onOpenChart }) {
  const rows = heldRows(bots);
  if (!rows.length) return <p className="py-2 text-sm text-muted">Every strangle has both legs.</p>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full whitespace-nowrap text-sm">
        <thead className="text-left text-xs uppercase text-muted">
          <tr className="border-b border-line">
            <th className="px-2 py-1">Leg</th>
            <th className="px-2 py-1">Held by</th>
            <th className="px-2 py-1" title="Confirmed regime (daily closes) · the GEX bias (spot vs flip ± buffer) · the SMA trend">
              Market now
            </th>
            <th className="px-2 py-1" title="Spot against the live gamma flip ± its 1σ buffer (shaded); % = how far spot must still rise to clear flip + buffer">
              Spot vs flip + buffer
            </th>
            <th className="px-2 py-1">Released when</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const rule = holdRule(r.rule);
            const expiry = expiryLabel(String(r.open_leg ?? "").split("-")[1]);
            return (
              <tr key={r.key} className="border-b border-line/50" data-testid={`held-${r.key}`}>
                <td className="px-2 py-1">
                  <span className="uppercase">{r.bot}</span> <Badge tone={r.missing === "call" ? "call" : "put"}>{r.missing}</Badge>{" "}
                  <span className="text-xs text-muted">
                    {slotLabel(r.slot)}
                    {expiry ? ` · ${expiry}` : ""} · with{" "}
                  </span>
                  {onOpenChart ? (
                    <button
                      type="button"
                      aria-haspopup="dialog"
                      onClick={(e) => onOpenChart(r.bot, r.strangle_id, e.currentTarget)}
                      className="font-mono text-xs text-muted underline decoration-dotted underline-offset-2 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
                      title="Open the payoff chart of this strangle"
                    >
                      {r.open_leg}
                    </button>
                  ) : (
                    <span className="font-mono text-xs text-muted">{r.open_leg}</span>
                  )}
                </td>
                <td className="px-2 py-1">
                  <Badge tone={rule.tone} title={r.reason || "repair may sell it this cycle"}>
                    {rule.label}
                  </Badge>
                </td>
                <td className="px-2 py-1 text-xs">
                  {r.confirmed_negative ? "confirmed negative" : "confirmed non-negative"}
                  <span className="text-muted">
                    {" "}
                    · bias {r.gex_bias || MISSING} · SMA {r.sma_trend || r.trend || MISSING}
                  </span>
                </td>
                <td className="px-2 py-1 text-xs tabular-nums">
                  <span className="inline-flex items-center gap-2">
                    <FlipBar spot={r.spot} flip={r.live_flip} bufferPct={r.buffer_pct} />
                    <span>
                      spot {formatUSD(r.spot)} · flip {r.live_flip ? formatUSD(r.live_flip) : MISSING} · +buffer{" "}
                      {r.above_flip_level ? formatUSD(r.above_flip_level) : MISSING}
                      {r.toAboveFlipPct != null ? (
                        <span className={r.toAboveFlipPct <= 0 ? "text-profit" : "text-muted"}> ({formatPct(r.toAboveFlipPct, 1, { signed: true })})</span>
                      ) : null}
                    </span>
                  </span>
                </td>
                <td className="px-2 py-1 text-xs">{releaseText(r)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
