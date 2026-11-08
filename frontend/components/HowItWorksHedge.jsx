import Badge from "./Badge";
import { Card, PAD_X, Picture, W } from "./HowItWorksParts";
import { formatAge, formatCoin } from "@/lib/format";
import { hedgeStaleAge } from "@/lib/futuresHedge";
import { STORY_BADGE, bandSize, deltaFigures, deltaWords, hedgeStory, orderCapWords } from "@/lib/howitworksHedge";

/**
 * Picture 4 (#696): the futures hedge in the page's plain words. The four
 * cards always read; the live figures come from `hedge` (null: not read yet,
 * or `error` says why it cannot be read), with `at` the time of the reading.
 */
export default function HedgePicture({ hedge, error, at, coin }) {
  const unit = hedge?.underlying || coin;
  const badge = STORY_BADGE[hedgeStory(hedge)];
  return (
    <Picture n={4} title="The safety net: a futures hedge">
      <p className="flex flex-wrap items-center gap-2 text-base text-muted">
        {badge ? <Badge tone={badge.tone}>{badge.text}</Badge> : null}
        testnet experiment
      </p>
      <LiveFigures hedge={hedge} error={error} at={at} unit={unit} />
      <div className="grid gap-3 sm:grid-cols-2">
        <Card title="What it does">
          The perpetual sits against the options&apos; delta. When the net leaves the <strong>hedge band</strong>
          {bandSize(hedge, unit)}, the bot trades the perpetual to bring it back, {orderCapWords(hedge)}.
        </Card>
        <Card title="What it replaces">
          While it is on, it replaces the GEX shed: both legs stay open and keep earning. If the hedge cannot trade, the shed takes over until it can. The exit
          line, the stop-loss and the margin cut still close legs.
        </Card>
        <Card title="What it does not do" loss>
          It removes the lean, not the curve: a big move still costs money, in either direction. Every hedge trade pays a fee, and on live the perpetual would
          also pay or receive funding.
        </Card>
        <Card title="Where you see it">Overview, Details (pro), Positions, Worst case and Income show it while it is on or holds a position. The switch is in Account.</Card>
      </div>
      <p className="text-base text-muted">Testnet only for now: using it on live needs more work and the owner&apos;s go.</p>
    </Picture>
  );
}

function LiveFigures({ hedge, error, at, unit }) {
  if (error) return <p className="text-base text-warn">Live figures unavailable: {error}. The explanation below still holds.</p>;
  if (!hedge) return <p className="text-base text-muted">Not yet: reading the hedge&apos;s live figures…</p>;
  const f = deltaFigures(hedge);
  if (!f) return <p className="text-base text-muted">The bot holds nothing to hedge right now, so there is no delta to draw.</p>;
  const age = hedgeStaleAge(hedge, at ?? undefined);
  return (
    <>
      <DeltaPicture f={f} unit={unit} />
      <p className="text-base tabular-nums">{deltaWords(f, unit)}</p>
      {age != null ? <p className="text-base text-warn">Not updating: the perpetual was last read {formatAge(age)}.</p> : null}
    </>
  );
}

const coin4 = (v, unit) => formatCoin(v, unit, 4);

function DeltaPicture({ f, unit }) {
  const H = 112;
  const barH = 14;
  const x = (v) => W / 2 + (v / f.scale) * (W / 2 - PAD_X);
  const bar = (v) => ({ x: Math.min(x(0), x(v)), width: Math.abs(x(v) - x(0)) });
  // A row's words sit on the free side of `at`: left of a bar or dot that goes right.
  const beside = (at, v, gap) => (v >= 0 ? { x: at - gap, textAnchor: "end" } : { x: at + gap, textAnchor: "start" });
  return (
    <svg role="img" aria-label={`Delta in ${unit}. ${deltaWords(f, unit)}`} viewBox={`0 0 ${W} ${H}`} className="h-auto w-full">
      {f.band != null ? (
        <>
          <rect x={x(-f.band)} y="12" width={x(f.band) - x(-f.band)} height={H - 12} className="fill-testnet/15" data-zone="band" />
          <text x={x(0)} y="9" textAnchor="middle" className="fill-testnet text-[10px]">
            hedge band
          </text>
        </>
      ) : null}
      <line x1={x(0)} x2={x(0)} y1="12" y2={H} className="stroke-muted" strokeWidth="1" />
      <rect {...bar(f.options)} y="18" height={barH} className="fill-slate-400" data-bar="options" />
      <text {...beside(x(0), f.options, 4)} y="29" className="fill-slate-300 text-[10px]">
        options {coin4(f.options, unit)}
      </text>
      {f.perp != null ? (
        <>
          <rect {...bar(f.perp)} y="46" height={barH} className="fill-testnet" data-bar="perpetual" />
          <text {...beside(x(0), f.perp, 4)} y="57" className="fill-testnet text-[10px]">
            perpetual {coin4(f.perp, unit)}
          </text>
        </>
      ) : null}
      <circle cx={x(f.net)} cy="88" r="5" className="fill-white" data-dot="net" />
      <text {...beside(x(f.net), f.net, 9)} y="92" className="fill-white text-[10px]">
        net {coin4(f.net, unit)}
      </text>
    </svg>
  );
}
