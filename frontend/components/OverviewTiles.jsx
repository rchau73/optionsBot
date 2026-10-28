import clsx from "clsx";
import MarginBar from "./MarginBar";
import StatTile from "./StatTile";
import { earnedLines, marginLines, openLines, roomFooter, usdFooter, worstLines } from "@/lib/tiles";

// The overview's four tiles (#404): one line per bot in its own coin — coins
// are never added together — and a footer with ≈ USD (the margin tile: the
// room left per bot). Loading shows skeleton tiles.

const TONE = { profit: "text-profit", loss: "text-loss", muted: "text-muted", flat: "" };

/** One bot's line: coin, the figure (signed, in its coin) and a note. */
function BotLine({ l }) {
  return (
    <li className="flex flex-wrap items-baseline justify-between gap-x-2 tabular-nums" data-testid={`tile-line-${l.bot}`}>
      <span className="text-xs font-semibold text-muted">{l.coin}</span>
      <span className={clsx("text-base font-semibold", TONE[l.tone] ?? "")}>{l.text}</span>
      {l.note && !l.unknown ? <span className="w-full text-right text-xs text-muted">{l.note}</span> : null}
    </li>
  );
}

function Lines({ lines }) {
  return <ul className="mt-1 space-y-1">{lines.map((l) => <BotLine key={l.bot} l={l} />)}</ul>;
}

/** bots: every bot of useMonitor; loading: no bot answered yet (skeleton tiles). */
export default function OverviewTiles({ bots, loading = false }) {
  if (loading) {
    return (
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4" data-testid="tiles-loading" aria-busy="true">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-24 animate-pulse rounded-lg border border-line bg-panel" />
        ))}
      </div>
    );
  }
  const earned = earnedLines(bots);
  const open = openLines(bots);
  const worst = worstLines(bots);
  const margin = marginLines(bots);
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4" data-testid="overview-tiles">
      <StatTile region label="Coins earned this month" sub="realised, net of fees" footer={usdFooter(earned)}>
        <Lines lines={earned} />
      </StatTile>
      <StatTile region label="Open now" sub="unrealised, moves with the market" footer={usdFooter(open)}>
        <Lines lines={open} />
      </StatTile>
      <StatTile region label="Worst case" sub="one very bad day (a model)" footer={usdFooter(worst)}>
        <Lines lines={worst} />
      </StatTile>
      <StatTile region label="Margin used" sub="initial margin, % of the limit" footer={roomFooter(margin)}>
        <ul className="mt-1 space-y-1">
          {margin.map((l) =>
            l.unknown ? (
              <BotLine key={l.bot} l={l} />
            ) : (
              <li key={l.bot} data-testid={`tile-line-${l.bot}`}>
                <MarginBar label={`${l.coin} ${l.text}`} pct={l.imPct} markers={[{ pct: l.limitPct, label: `${l.coin} IM limit ${l.limitPct}%` }]} />
              </li>
            ),
          )}
        </ul>
      </StatTile>
    </div>
  );
}
