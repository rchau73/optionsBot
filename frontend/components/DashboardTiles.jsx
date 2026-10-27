import clsx from "clsx";
import Link from "next/link";
import InfoTip from "./InfoTip";
import MarginBar from "./MarginBar";
import MonthBars from "./MonthBars";
import { riskRows } from "@/lib/account";
import { formatCoin, formatPct, isNumber } from "@/lib/format";
import { clockHM, coinOf, monthSoFar, worstCaseLine } from "@/lib/overview";
import { formatSignedPct, formatSignedUSD, toneOf } from "@/lib/summary";

// The overview's four stat tiles per bot (#394): numbers with a sign, a unit
// and a time; the investor summary's sentences sit in each tile's tooltip.
// Missing data says "unknown: <reason>", never 0.

const LINK = "inline-flex min-h-11 items-center text-sm text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2";

/** One tile: its label and tooltip, the value (coloured by sign, never by colour alone), a sub-line and extras. */
function Tile({ label, tip, value, tone, sub, children }) {
  return (
    <section aria-label={label} className="flex flex-col gap-1 rounded-lg border border-line bg-panel px-3 py-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs uppercase tracking-wide text-muted">{label}</h3>
        <InfoTip label={`About ${label}`}>{tip}</InfoTip>
      </div>
      <div className={clsx("text-lg font-semibold tabular-nums", tone === "profit" && "text-profit", tone === "loss" && "text-loss", tone === "muted" && "text-muted")}>
        {value}
      </div>
      {sub ? <div className="text-xs text-muted tabular-nums">{sub}</div> : null}
      {children}
    </section>
  );
}

const at = (iso) => (clockHM(iso) ? `as of ${clockHM(iso)}` : null);

/** The four tiles of one bot: This month, Open now, Worst case, Margin used. */
export default function DashboardTiles({ bot }) {
  const s = bot.summary;
  const coin = coinOf(bot);
  const failure = bot.summaryError ?? bot.error;
  const unknown = failure ? `unknown: ${failure}` : !s ? "unknown: loading the summary" : null;
  return (
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4" data-testid={`tiles-${bot.name}`}>
      <ThisMonth s={s} coin={coin} bot={bot.name} unknown={unknown} />
      <OpenNow s={s} coin={coin} unknown={unknown} />
      <WorstCase s={s} coin={coin} bot={bot.name} unknown={unknown} />
      <MarginUsed bot={bot} />
    </div>
  );
}

function ThisMonth({ s, coin, bot, unknown }) {
  const m = s?.income?.this_month;
  const closes = m?.closes ?? 0;
  const tip = "Realised income this UTC month: what the closed trades kept, after Deribit's fees, from the bot's journal. The bars are the last six months; a loss month is hatched.";
  return (
    <Tile
      label="This month"
      tip={tip}
      value={unknown ?? (closes > 0 ? formatCoin(m.realised_coin, coin) : "not yet: no close this month")}
      tone={unknown || !closes ? "muted" : toneOf(m.realised_coin)}
      sub={unknown ? null : [closes > 0 ? `≈ ${formatSignedUSD(m.realised_usd)}` : null, monthSoFar(s?.as_of), at(s?.as_of)].filter(Boolean).join(" · ")}
    >
      {unknown ? null : <MonthBars income={s.income} className="h-8" />}
      <Link href={`/income?bot=${encodeURIComponent(bot)}`} className={LINK}>
        Month by month →
      </Link>
    </Tile>
  );
}

function OpenNow({ s, coin, unknown }) {
  const v = s?.income?.unrealised_coin;
  const tip = "What the open positions would make or lose if bought back now at their marks, net of the fees paid to open them. Not realised: it moves with the market until each position closes.";
  const missing = unknown ?? (isNumber(v) ? null : "unknown: no mark yet");
  return (
    <Tile
      label="Open now"
      tip={tip}
      value={missing ?? formatCoin(v, coin)}
      tone={missing ? "muted" : toneOf(v)}
      sub={missing ? null : [`≈ ${formatSignedUSD(s.income.unrealised_usd)}`, "not realised", at(s.as_of)].join(" · ")}
    />
  );
}

function WorstCase({ s, coin, bot, unknown }) {
  const w = unknown ? { unknown: unknown.replace(/^unknown: /, "") } : worstCaseLine(s?.worst_case);
  const tip = `A model, not a quote: ${s?.worst_case?.model || "Black–Scholes on each leg's IV"}. The worse of a very bad day each way (3 daily σ from DVOL), one day passed.`;
  return (
    <Tile
      label="Worst case"
      tip={tip}
      value={w.unknown ? `unknown: ${w.unknown}` : formatCoin(-Math.abs(w.lossCoin), coin)}
      tone={w.unknown ? "muted" : "loss"}
      sub={
        w.unknown
          ? null
          : [
              `≈ ${formatSignedUSD(-Math.abs(w.lossUSD))}`,
              w.pct == null ? "% of account unknown" : `${formatSignedPct(-Math.abs(w.pct))} of account`,
              `a very bad day (${coin} ${formatSignedPct(w.move)})`,
              at(s.as_of),
            ].join(" · ")
      }
    >
      <Link href={`/worst-case?bot=${encodeURIComponent(bot)}`} className={LINK}>
        Five days in money →
      </Link>
    </Tile>
  );
}

function MarginUsed({ bot }) {
  const r = riskRows([bot])[0];
  const tip =
    "Initial margin the bot's book uses, as Deribit reports it, against the limit its margin policy allows now (the tick on the bar). New entries stop at the limit; maintenance margin is shown when over half its cap.";
  const missing = !r ? "unknown: no margin reading yet" : r.error ? `unknown: ${r.error}` : !isNumber(r.imPct) || !isNumber(r.limitIMPct) || r.limitIMPct <= 0 ? "unknown: no margin reading yet" : null;
  const ratio = missing ? null : (r.imPct / r.limitIMPct) * 100;
  const showMM = r && isNumber(r.mmPct) && isNumber(r.maxMMPct) && r.mmPct > r.maxMMPct / 2;
  return (
    <Tile
      label="Margin used"
      tip={tip}
      value={missing ?? `${formatPct(ratio, 0)} of limit`}
      tone={missing ? "muted" : ratio > 100 ? "loss" : undefined}
      sub={missing ? null : [`IM ${formatPct(r.imPct, 1)} of ${formatPct(r.limitIMPct, 0)} limit`, at(bot.status?.risk?.as_of)].filter(Boolean).join(" · ")}
    >
      {missing ? null : <MarginBar label="IM used" pct={r.imPct} markers={[{ pct: r.limitIMPct, label: `IM limit ${r.limitIMPct}%` }]} />}
      {showMM ? (
        <p className={clsx("text-xs tabular-nums", r.mmPct >= r.maxMMPct ? "text-loss" : "text-warn")}>
          MM {formatPct(r.mmPct, 1)} of {formatPct(r.maxMMPct, 0)} cap
        </p>
      ) : null}
    </Tile>
  );
}
