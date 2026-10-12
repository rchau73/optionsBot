import Badge from "./Badge";
import { MISSING, formatDateTime, formatPct, formatUSD, slotLabel } from "@/lib/format";
import { heldRows, holdRule } from "@/lib/held";

/** Spot's distance to a release level: "$85,071 (+1.6%)", green once spot is above it. */
function Level({ level, pct }) {
  if (!level) return <span className="text-muted">{MISSING}</span>;
  const above = pct !== null && pct <= 0;
  return (
    <span className={above ? "text-profit" : undefined}>
      {formatUSD(level)} <span className="text-xs text-muted">({pct === null ? MISSING : formatPct(pct, 1, { signed: true })})</span>
    </span>
  );
}

/**
 * Missing legs of one-legged strangles and why repair has not re-sold them:
 * the rule the bot applies now, and the levels each release is measured
 * against — the live flip + buffer next to the flip a GEX-shed leg was shed
 * at (its anchor), the stop cooldown, and the script's direction label.
 */
export default function HeldLegs({ bots }) {
  const rows = heldRows(bots);
  if (!rows.length) return <p className="py-3 text-center text-sm text-muted">Every strangle has both legs.</p>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="text-left text-xs uppercase text-muted">
          <tr className="border-b border-line">
            <th className="px-2 py-1">Bot</th>
            <th className="px-2 py-1">Missing</th>
            <th className="px-2 py-1">Held by</th>
            <th className="px-2 py-1" title="Confirmed regime (daily closes) · the bot's SMA trend · the script's bias (spot vs flip)">
              Regime · trend · bias
            </th>
            <th className="px-2 py-1 text-right" title="Live gamma flip × (1 + 1σ buffer); % = how far spot must still rise">
              Live flip + buffer
            </th>
            <th className="px-2 py-1 text-right" title="GEX-shed legs only: the flip at the shed × (1 + its buffer), held on N snapshots — or a non-negative live regime for the release hours">
              Anchor flip + buffer
            </th>
            <th className="px-2 py-1 text-right" title="Stopped or hand-closed legs: no re-sell before this">
              Cooldown until
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const rule = holdRule(r.rule);
            return (
              <tr key={r.key} className="border-b border-line/50 align-top" data-testid={`held-${r.key}`}>
                <td className="px-2 py-1 uppercase">{r.bot}</td>
                <td className="px-2 py-1">
                  <Badge tone={r.missing}>{r.missing}</Badge> <span className="text-xs text-muted">{slotLabel(r.slot)}</span>
                  <div className="font-mono text-xs text-muted">with {r.open_leg}</div>
                </td>
                <td className="px-2 py-1">
                  <Badge tone={rule.tone}>{rule.label}</Badge>
                  <div className="text-xs text-muted">{r.reason || "repair may sell it this cycle"}</div>
                </td>
                <td className="px-2 py-1 text-xs">
                  <div>{r.confirmed_negative ? "confirmed negative" : "confirmed non-negative"}</div>
                  <div className="text-muted">
                    trend {r.trend || MISSING} · bias {r.bias ? `accel. ${r.bias}` : MISSING}
                  </div>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">
                  <Level level={r.above_flip_level} pct={r.toAboveFlipPct} />
                  <div className="text-xs text-muted">
                    spot {formatUSD(r.spot)} · flip {r.live_flip ? formatUSD(r.live_flip) : MISSING}
                  </div>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">
                  {r.anchor ? (
                    <>
                      <Level level={r.anchor.level} pct={r.toAnchorPct} />
                      <div className="text-xs text-muted">
                        flip {formatUSD(r.anchor.flip)} · {r.anchor.above}/{r.anchor.need} snapshots · or {r.anchor.release_hours} h non-negative
                      </div>
                    </>
                  ) : (
                    <span className="text-xs text-muted">not shed by GEX</span>
                  )}
                </td>
                <td className="px-2 py-1 text-right text-xs tabular-nums">{r.cooldown_until ? formatDateTime(r.cooldown_until) : MISSING}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
