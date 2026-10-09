import Badge from "./Badge";
import { flipBand, loopStalled } from "@/lib/monitor";
import { ageSeconds, formatAge, formatDateTime, formatPct, formatUSD } from "@/lib/format";

/** One chip per bot: environment, market context and data freshness. */
export default function Header({ bots, error }) {
  return (
    <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
      <h1 className="mr-2 text-lg font-bold">optionsBot · Live monitor</h1>
      {error ? <Badge tone="bad">{error}</Badge> : null}
      {bots.map((b) => (
        <BotChip key={b.name} bot={b} />
      ))}
    </header>
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
          {s.trend ? ` · ${s.trend}` : ""}
        </span>
      ) : null}
      {s ? <FlipBand band={flipBand(s)} /> : null}
    </div>
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
