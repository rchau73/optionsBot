import Badge from "./Badge";
import { loopStalled } from "@/lib/monitor";
import { ageSeconds, formatAge, formatDateTime, formatPct, formatUSD } from "@/lib/format";

/**
 * One chip per bot: environment, warnings and data freshness, spot and DVOL.
 * The regime, trend, flip and SMA levels are drawn on each bot's regime chart
 * (#394); the person's menu is the menu bar's last item.
 */
export default function Header({ bots, error }) {
  return (
    <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
      <h1 className="mr-2 text-lg font-bold">optionsBot · Live monitor</h1>
      <Deployment bots={bots} />
      {error ? <Badge tone="bad">{error}</Badge> : null}
      {bots.map((b) => (
        <BotChip key={b.name} bot={b} />
      ))}
    </header>
  );
}

/**
 * The local environment the bots run in (scripts/envctl.sh): STABLE runs a
 * promoted release, MVP runs main. Shown once, so two monitors side by side are
 * never confused. Nothing when the bots were started by hand.
 */
export function Deployment({ bots }) {
  const s = bots.find((b) => b.status?.deployment)?.status;
  if (!s) return null;
  const stable = s.deployment === "stable";
  return (
    <span data-testid="deployment">
      <Badge tone={stable ? "good" : "warn"} title={stable ? "promoted release (scripts/promote.sh)" : "main: every merge lands here"}>
        {s.deployment.toUpperCase()}
        {s.version ? ` · ${s.version}` : ""}
      </Badge>
    </span>
  );
}

function BotChip({ bot }) {
  const s = bot.status;
  const live = s?.environment === "live";
  const age = ageSeconds(s?.as_of);
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border border-line px-2 py-1 text-sm" data-testid={`bot-${bot.name}`}>
      <span className="font-semibold">{bot.name.toUpperCase()}</span>
      {s ? <Badge tone={live ? "live" : "testnet"}>{live ? "LIVE" : (s.environment || "testnet").toUpperCase()}</Badge> : null}
      {s?.halted ? <Badge tone="bad">HALTED</Badge> : null}
      {!bot.stale && loopStalled(s) ? (
        <Badge tone="bad" title={`no decision cycle since ${formatDateTime(s.loop_at)}: exits and stop-losses are not being checked`}>
          LOOP STALLED
        </Badge>
      ) : null}
      {bot.stale ? (
        <Badge tone="warn" title={bot.error ?? "no update in the last seconds"}>
          {bot.error ? "OFFLINE" : "STALE"}
        </Badge>
      ) : (
        <Badge tone="good">● {formatAge(age)}</Badge>
      )}
      {s?.risk?.im_over_limit ? <Badge tone="warn">IM over limit</Badge> : null}
      {s ? (
        <span className="text-muted tabular-nums">
          {formatUSD(s.market?.spot)} · DVOL {s.market?.dvol ? s.market.dvol.toFixed(1) : "—"} · IV pct {formatPct(s.market?.iv_percentile, 0)}
        </span>
      ) : null}
    </div>
  );
}

/** " · bias up (SMA bear)" under gex_direction bias, " · bear (bias up)" under sma: the source in use first. */
export function directionText(s) {
  if (!s?.trend) return "";
  if (s.direction === "bias") return ` · bias ${s.gex_bias ?? "—"} (SMA ${s.sma_trend ?? "—"})`;
  return ` · ${s.trend}${s.gex_bias ? ` (bias ${s.gex_bias})` : ""}`;
}
