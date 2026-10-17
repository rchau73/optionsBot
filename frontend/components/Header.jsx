import Badge from "./Badge";
import { flipBand, loopStalled, smaLevels } from "@/lib/monitor";
import { ageSeconds, formatAge, formatDateTime, formatPct, formatUSD } from "@/lib/format";

/** One chip per bot: environment, market context and data freshness. */
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
      {s ? (
        <span className="text-muted tabular-nums">
          {formatUSD(s.market?.spot)} · DVOL {s.market?.dvol ? s.market.dvol.toFixed(1) : "—"} · IV pct {formatPct(s.market?.iv_percentile, 0)}
          {s.market?.gex_regime ? ` · ${s.market.gex_regime}` : ""}
          {directionText(s)}
        </span>
      ) : null}
      {s ? <FlipBand band={flipBand(s)} /> : null}
      {s ? <SmaLevels sma={smaLevels(s)} /> : null}
    </div>
  );
}

/** SMA9, SMA21 and the last swings (with their days), each with spot's distance to it. */
export function SmaLevels({ sma }) {
  if (!sma || sma.levels.length === 0) return null;
  const title = `SMA trend ${sma.trend ?? "—"}${sma.reason ? `: ${sma.reason}` : ""} · % = level vs spot`;
  return (
    <span className="text-xs text-muted tabular-nums" data-testid="sma-levels" title={title}>
      {sma.levels
        .map((l) => {
          const dist = l.distancePct !== null ? ` (${formatPct(l.distancePct, 1, { signed: true })})` : "";
          const day = l.date ? ` ${l.date.slice(5)}` : "";
          return `${l.label} ${formatUSD(l.value)}${dist}${day}`;
        })
        .join(" · ")}
    </span>
  );
}

/** Gamma flip, spot's distance to it, and the ±1σ band the GEX shed uses. */
function FlipBand({ band }) {
  if (!band) return null;
  const sigma = band.bufferPct > 0 ? `±1σ (${formatPct(band.bufferPct, 1)}) ${formatUSD(band.low)}–${formatUSD(band.high)}` : null;
  return (
    <span className="flex items-center gap-2 text-muted tabular-nums" data-testid="flip-band">
      <span title="gamma flip: the price where dealer gamma changes sign">
        Flip {formatUSD(band.flip)}
        {band.spotToFlipPct !== null ? ` (spot ${formatPct(band.spotToFlipPct, 1, { signed: true })})` : ""}
        {sigma ? ` · ${sigma}` : ""}
      </span>
      {band.zone === "below" ? (
        <Badge tone="bad" title="spot is more than 1σ below the flip: in a negative regime with a trend, the bot sheds that side">
          below flip −1σ
        </Badge>
      ) : null}
    </span>
  );
}

/** " · bias up (SMA bear)" under gex_direction bias, " · bear (bias up)" under sma: the source in use first. */
export function directionText(s) {
  if (!s?.trend) return "";
  if (s.direction === "bias") return ` · bias ${s.gex_bias ?? "—"} (SMA ${s.sma_trend ?? "—"})`;
  return ` · ${s.trend}${s.gex_bias ? ` (bias ${s.gex_bias})` : ""}`;
}
