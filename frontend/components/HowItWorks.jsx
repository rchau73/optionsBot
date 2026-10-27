import { useId } from "react";
import { formatCoin, formatUSD, isNumber } from "@/lib/format";
import { findTerm } from "@/lib/glossary";
import { cooldownWords, exampleLabel, payoffPoints, stopWord, strangleFigures } from "@/lib/howitworks";

// "How it works" (#322, E3 prototype screen 7): a short strangle in three
// pictures — two legs, the payoff at expiry, the bot's defences — and the
// words of Details, with the bot's nearest strangle or a labelled example.

const GLOSSARY_TERMS = ["leg", "strike", "premium", "delta", "expiry", "margin"];

/** The page body for one bot's /api/summary (null → the example). */
export default function HowItWorks({ summary }) {
  const fig = strangleFigures(summary);
  const coin = fig.coin;
  const threshold = isNumber(summary?.delta_exit_threshold) && summary.delta_exit_threshold > 0 ? summary.delta_exit_threshold : null;
  return (
    <article aria-label="How it works" className="space-y-6">
      <div className="space-y-2">
        <h1 className="text-2xl font-bold">How your {summary?.underlying || coin} earns, in three pictures</h1>
        <p className="text-base">
          The bot runs one strategy, called a <strong>short strangle</strong>. In plain words: it gets paid now for promising to buy {coin} if it
          falls a lot, and to sell {coin} if it rises a lot, before a date. Two minutes to read.
        </p>
        {fig.live ? null : (
          <p className="rounded border border-warn/60 px-3 py-2 text-base text-warn">
            <strong>{exampleLabel(coin)}.</strong> {fig.note}
          </p>
        )}
      </div>
      <TwoLegs fig={fig} />
      <Payoff fig={fig} />
      <Defences fig={fig} threshold={threshold} stop={stopWord(summary?.stop_loss_multiple)} cooldown={cooldownWords(summary?.repair_cooldown_hours)} />
      <Words threshold={threshold} />
    </article>
  );
}

function Picture({ n, title, children }) {
  return (
    <section aria-label={title} className="space-y-3 rounded-lg border border-line bg-panel p-4">
      <h2 className="text-lg font-semibold">
        <span className="text-muted">{n} · </span>
        {title}
      </h2>
      {children}
    </section>
  );
}

const coin4 = (v, coin) => formatCoin(v, coin, 4);

function TwoLegs({ fig }) {
  const { coin } = fig;
  return (
    <Picture n={1} title="One position, two legs">
      <div className="grid gap-3 md:grid-cols-3">
        <div className="space-y-1 rounded border-2 border-put p-3">
          <p className="text-sm text-put">Leg 1 · the floor</p>
          <p className="text-xl font-bold tabular-nums">{formatUSD(fig.put.strike)}</p>
          <p className="text-base">
            The bot <strong>sells a put</strong>: it promises to buy {coin} at {formatUSD(fig.put.strike)} if {coin} is below that on {fig.expiryText}.
          </p>
          <p className="text-base tabular-nums text-profit">paid now {coin4(fig.put.premium, coin)}</p>
        </div>
        <div className="flex flex-col items-center justify-center gap-1 rounded border border-line p-3 text-center">
          <p className="text-base">
            {coin} today <strong className="tabular-nums">{formatUSD(fig.spot)}</strong>
          </p>
          <p className="text-base text-muted">in the middle, with room on both sides</p>
        </div>
        <div className="space-y-1 rounded border-2 border-call p-3">
          <p className="text-sm text-call">Leg 2 · the ceiling</p>
          <p className="text-xl font-bold tabular-nums">{formatUSD(fig.call.strike)}</p>
          <p className="text-base">
            The bot <strong>sells a call</strong>: it promises to sell {coin} at {formatUSD(fig.call.strike)} if {coin} is above that on {fig.expiryText}.
          </p>
          <p className="text-base tabular-nums text-profit">paid now {coin4(fig.call.premium, coin)}</p>
        </div>
      </div>
      <p className="text-base">
        Together the two legs are one <strong>strangle</strong>:{" "}
        <strong className="tabular-nums">
          {coin4(fig.premium, coin)}
        </strong>{" "}
        paid today. &ldquo;Short&rdquo; means the bot is the seller: it collects the payment and carries the risk. The buyers are usually traders
        protecting themselves against big moves.
      </p>
    </Picture>
  );
}

// SVG geometry shared by pictures 2 and 3.
const W = 360;
const PAD_X = 12;

function Payoff({ fig }) {
  const id = useId().replace(/:/g, "");
  const { coin } = fig;
  const pts = payoffPoints(fig);
  const lo = pts[0].s;
  const hi = pts[pts.length - 1].s;
  const H = 190;
  const top = 24;
  const bottom = H - 30;
  const ymax = fig.premium * 1.3;
  const ymin = Math.min(...pts.map((p) => p.y), -fig.premium);
  const x = (s) => PAD_X + ((s - lo) / (hi - lo)) * (W - 2 * PAD_X);
  const y = (v) => top + ((ymax - v) / (ymax - ymin)) * (bottom - top);
  const curve = pts.map((p) => `${x(p.s).toFixed(1)},${y(p.y).toFixed(1)}`).join(" ");
  const area = `${x(lo).toFixed(1)},${y(0).toFixed(1)} ${curve} ${x(hi).toFixed(1)},${y(0).toFixed(1)}`;
  const zero = y(0);
  const beText = [fig.low, fig.high].filter(isNumber).map((v) => formatUSD(v));
  const label =
    `What the ${coin} strangle pays at expiry, for each ${coin} price: between ${formatUSD(fig.put.strike)} and ${formatUSD(fig.call.strike)} ` +
    `you keep all ${coin4(fig.premium, coin)}; ` +
    (beText.length ? `it falls to zero at ${beText.join(" and ")} (the break-evens); ` : "") +
    `beyond them the loss grows, with no fixed limit. Today ${coin} is ${formatUSD(fig.spot)}.`;
  return (
    <Picture n={2} title={`What you end with on ${fig.expiryText}, for each ${coin} price`}>
      <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} className="h-auto w-full">
        <defs>
          <pattern id={`${id}-hatch`} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <line x1="0" y1="0" x2="0" y2="6" className="stroke-loss" strokeWidth="2" />
          </pattern>
          <clipPath id={`${id}-above`}>
            <rect x="0" y="0" width={W} height={zero} />
          </clipPath>
          <clipPath id={`${id}-below`}>
            <rect x="0" y={zero} width={W} height={H - zero} />
          </clipPath>
        </defs>
        <polygon points={area} className="fill-profit/30" clipPath={`url(#${id}-above)`} data-zone="earn" />
        <polygon points={area} fill={`url(#${id}-hatch)`} clipPath={`url(#${id}-below)`} data-zone="loss" />
        <line x1={PAD_X} x2={W - PAD_X} y1={zero} y2={zero} className="stroke-muted" strokeWidth="1" />
        <polyline points={curve} fill="none" className="stroke-white" strokeWidth="2" />
        <line x1={x(fig.spot)} x2={x(fig.spot)} y1={top - 8} y2={bottom} className="stroke-testnet" strokeWidth="1.5" strokeDasharray="4 3" />
        <text x={x(fig.spot)} y={top - 12} textAnchor="middle" className="fill-testnet text-[10px]">
          today {formatUSD(fig.spot)}
        </text>
        <text x={(x(fig.put.strike) + x(fig.call.strike)) / 2} y={y(fig.premium) - 6} textAnchor="middle" className="fill-profit text-[10px]">
          keep all {coin4(fig.premium, coin)}
        </text>
        {[fig.put.strike, fig.call.strike].map((k) => (
          <text key={k} x={x(k)} y={bottom + 12} textAnchor="middle" className="fill-muted text-[9px]">
            {formatUSD(k)}
          </text>
        ))}
        {[fig.low, fig.high].filter(isNumber).map((v) => (
          <text key={v} x={x(v)} y={zero - 4} textAnchor="middle" className="fill-muted text-[9px]">
            0
          </text>
        ))}
        <text x={PAD_X} y={H - 4} className="fill-loss text-[10px]">
          ◀ loss grows
        </text>
        <text x={W - PAD_X} y={H - 4} textAnchor="end" className="fill-loss text-[10px]">
          loss grows ▶
        </text>
      </svg>
      <ul className="flex flex-wrap gap-4 text-base" aria-label="Legend">
        <li className="flex items-center gap-2">
          <span aria-hidden="true" className="inline-block h-4 w-6 rounded-sm bg-profit/30" /> you earn
        </li>
        <li className="flex items-center gap-2">
          <svg aria-hidden="true" width="24" height="16">
            <rect width="24" height="16" fill={`url(#${id}-hatch)`} />
          </svg>{" "}
          you lose (hatched)
        </li>
      </ul>
      {beText.length ? (
        <p className="text-base tabular-nums">
          Break-even{beText.length > 1 ? "s" : ""} at expiry: {beText.join(" and ")}.
        </p>
      ) : null}
      <p className="text-base">
        The most you can earn is the payment. The loss beyond the floor or the ceiling has no fixed limit, which is why the bot doesn&apos;t wait until{" "}
        {fig.expiryText} when {coin} runs towards one side (picture 3).
      </p>
    </Picture>
  );
}

function Defences({ fig, threshold, stop, cooldown }) {
  const id = useId().replace(/:/g, "");
  const { coin } = fig;
  const exits = fig.exitLines && isNumber(fig.put.exit) && isNumber(fig.call.exit);
  const marks = [fig.put.strike, fig.call.strike, fig.spot, ...(exits ? [fig.put.exit, fig.call.exit] : [])];
  const span = Math.max(...marks) - Math.min(...marks);
  const lo = Math.min(...marks) - span * 0.15;
  const hi = Math.max(...marks) + span * 0.15;
  const H = 96;
  const lineY = 48;
  const x = (s) => PAD_X + ((s - lo) / (hi - lo)) * (W - 2 * PAD_X);
  const deltaText = threshold != null ? ` (delta ${threshold.toFixed(2)})` : "";
  const label =
    `${coin} price line: floor ${formatUSD(fig.put.strike)}, ceiling ${formatUSD(fig.call.strike)}, today ${formatUSD(fig.spot)}. ` +
    (exits
      ? `Exit lines${deltaText} at ${formatUSD(fig.put.exit)} for the put and ${formatUSD(fig.call.exit)} for the call, before the loss zone.`
      : "Exit lines move with the market.") +
    " Beyond the floor and the ceiling is the loss zone.";
  return (
    <Picture n={3} title="How the bot defends you before the loss zone">
      <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} className="h-auto w-full">
        <defs>
          <pattern id={`${id}-hatch`} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <line x1="0" y1="0" x2="0" y2="6" className="stroke-loss" strokeWidth="2" />
          </pattern>
        </defs>
        <rect x={PAD_X} y={lineY - 8} width={x(fig.put.strike) - PAD_X} height="16" fill={`url(#${id}-hatch)`} data-zone="loss" />
        <rect x={x(fig.call.strike)} y={lineY - 8} width={W - PAD_X - x(fig.call.strike)} height="16" fill={`url(#${id}-hatch)`} data-zone="loss" />
        <line x1={PAD_X} x2={W - PAD_X} y1={lineY} y2={lineY} className="stroke-muted" strokeWidth="2" />
        <Mark x={x(fig.put.strike)} y={lineY} cls="put" above text={`floor ${formatUSD(fig.put.strike)}`} />
        <Mark x={x(fig.call.strike)} y={lineY} cls="call" above text={`ceiling ${formatUSD(fig.call.strike)}`} />
        <Mark x={x(fig.spot)} y={lineY} cls="testnet" text={`today ${formatUSD(fig.spot)}`} dashed />
        {exits ? (
          <>
            <Mark x={x(fig.put.exit)} y={lineY} cls="warn" text="exit" dashed small />
            <Mark x={x(fig.call.exit)} y={lineY} cls="warn" text="exit" dashed small />
          </>
        ) : null}
      </svg>
      <p className="text-base tabular-nums">
        {exits ? (
          <>
            Exit lines{deltaText}: {formatUSD(fig.put.exit)} on the floor&apos;s side and {formatUSD(fig.call.exit)} on the ceiling&apos;s side
            {fig.live ? " — a model on today's prices; they move with the market" : ""}.
          </>
        ) : (
          <>
            Exit lines move with the market{fig.exitReason ? ` (unknown: ${fig.exitReason})` : ""}: the bot acts on each leg&apos;s delta{deltaText}, before the
            floor or the ceiling.
          </>
        )}
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Card title="Calm · take profit">
          Most of the payment is earned early: the bot closes and sells a fresh position, instead of waiting for the last cents.
        </Card>
        <Card title="Near a side · exit line">
          {coin} moves towards the floor or ceiling: the bot buys back <strong>that leg only</strong>, taking a small loss before it can become a big
          one. The other leg keeps earning.
        </Card>
        <Card title="Sudden jump · stop-loss" loss>
          {stop ? (
            <>
              A leg that now costs <strong>{stop}</strong> what it was paid is bought back at once, whatever the price.
            </>
          ) : (
            "A leg whose cost reaches its stop-loss limit is bought back at once, whatever the price."
          )}
        </Card>
        <Card title="After · wait, then repair">
          The bot doesn&apos;t sell into the move: it waits ({cooldown ? `${cooldown} after a stop` : "a set time after a stop"}), then sells a new leg further
          away to rebuild the position.
        </Card>
      </div>
    </Picture>
  );
}

const MARK_CLASSES = {
  put: { stroke: "stroke-put", fill: "fill-put" },
  call: { stroke: "stroke-call", fill: "fill-call" },
  testnet: { stroke: "stroke-testnet", fill: "fill-testnet" },
  warn: { stroke: "stroke-warn", fill: "fill-warn" },
};

function Mark({ x, y, cls, text, above = false, dashed = false, small = false }) {
  const c = MARK_CLASSES[cls];
  return (
    <g>
      <line x1={x} x2={x} y1={y - 16} y2={y + 16} className={c.stroke} strokeWidth="2" strokeDasharray={dashed ? "3 2" : undefined} />
      <text x={x} y={above ? y - 22 : y + 30} textAnchor="middle" className={`${c.fill} ${small ? "text-[9px]" : "text-[10px]"}`}>
        {text}
      </text>
    </g>
  );
}

function Card({ title, children, loss = false }) {
  return (
    <div className="space-y-1 rounded border border-line p-3">
      <h3 className="text-base font-semibold">
        {title}
        {loss ? (
          <span className="ml-2 text-sm text-loss" aria-label="loss">
            ▼
          </span>
        ) : null}
      </h3>
      <p className="text-base">{children}</p>
    </div>
  );
}

function Words({ threshold }) {
  return (
    <section aria-label="Words you'll see in Details (pro)" className="space-y-3">
      <h2 className="text-lg font-semibold">Words you&apos;ll see in Details (pro)</h2>
      <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {GLOSSARY_TERMS.map((t) => {
          const entry = findTerm(t);
          return (
            <div key={t} className="rounded border border-line bg-panel p-3">
              <dt className="text-base font-semibold capitalize">{t}</dt>
              <dd className="text-base">
                {entry?.short}
                {t === "delta" && threshold != null ? ` The exit line is a delta of ${threshold.toFixed(2)}.` : ""}
              </dd>
            </div>
          );
        })}
      </dl>
    </section>
  );
}
