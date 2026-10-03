import clsx from "clsx";
import Badge from "./Badge";
import MarginBar from "./MarginBar";
import StatTile from "./StatTile";
import { LIQUIDATION_MM_PCT, marginLevel, marginModelLabel, worstMMPct } from "@/lib/account";
import { STALE_AFTER_SEC } from "@/lib/monitor";
import { formatAge, formatPct, formatUSD } from "@/lib/format";

const ACCOUNT_STALE_SEC = Math.max(STALE_AFTER_SEC, 30); // the bot polls the account every ~10 s

const amount = (v) => (typeof v === "number" ? v.toLocaleString("en-US", { maximumFractionDigits: 6 }) : "—");

/** Collateral per asset and margin usage, as reported by Deribit. */
export default function AccountPanel({ account }) {
  if (!account) {
    return <p className="py-3 text-center text-sm text-muted">No account data yet — the bots report it every few seconds.</p>;
  }
  const { snapshot: s, bot, ageSec, error } = account;
  const stale = ageSec == null || ageSec > ACCOUNT_STALE_SEC;
  const worst = worstMMPct(s);
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge tone="neutral">{marginModelLabel(s.margin_model)}</Badge>
        <Badge tone={s.cross_collateral ? "good" : "muted"}>Cross collateral {s.cross_collateral ? "on" : "off"}</Badge>
        {stale ? <Badge tone="warn">STALE</Badge> : null}
        {error ? <Badge tone="warn" title={error}>last poll failed</Badge> : null}
        {marginLevel(worst) === "danger" ? <Badge tone="bad">MARGIN HIGH</Badge> : null}
        <span className="text-xs text-muted">
          reported by {bot.toUpperCase()} · {formatAge(ageSec)}
        </span>
      </div>

      {s.totals ? (
        <div className="grid grid-cols-2 gap-2 lg:grid-cols-6">
          <StatTile label="Equity (all collateral)" value={formatUSD(s.totals.equity_usd)} />
          <StatTile label="Margin balance" value={formatUSD(s.totals.margin_balance_usd)} />
          <StatTile label="Initial margin" value={formatUSD(s.totals.initial_margin_usd)} />
          <StatTile label="Maintenance margin" value={formatUSD(s.totals.maintenance_margin_usd)} />
          <div className="col-span-2 space-y-2 rounded-lg border border-line bg-panel px-3 py-2">
            <MarginBar label="IM used" pct={s.totals.im_pct} />
            <MarginBar label="MM used (liquidation at 100 %)" pct={s.totals.mm_pct} />
          </div>
        </div>
      ) : null}

      <div className="overflow-x-auto">
        <table className="w-full min-w-[900px] text-sm">
          <thead className="text-left text-xs uppercase text-muted">
            <tr className="border-b border-line">
              <th className="px-2 py-1">Asset</th>
              <th className="px-2 py-1 text-right">Balance</th>
              <th className="px-2 py-1 text-right">Equity</th>
              <th className="px-2 py-1 text-right" title="Collateral counted against margin">Margin balance</th>
              <th className="px-2 py-1 text-right">Available</th>
              <th className="px-2 py-1 text-right">Withdrawable</th>
              <th className="px-2 py-1 text-right" title="Initial margin (projected: without the nearest expiry)">IM (proj.)</th>
              <th className="px-2 py-1 text-right" title="Maintenance margin (projected: without the nearest expiry)">MM (proj.)</th>
              <th className="px-2 py-1 text-right">IM %</th>
              <th className="px-2 py-1 text-right">MM %</th>
              <th className="px-2 py-1 text-right" title="Reserved by open spot orders">Reserved</th>
            </tr>
          </thead>
          <tbody>
            {s.assets.map((a) => (
              <tr key={a.currency} className="border-b border-line/50">
                <td className="px-2 py-1 font-semibold">{a.currency}</td>
                <td className="px-2 py-1 text-right tabular-nums">{amount(a.balance)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{amount(a.equity)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{amount(a.margin_balance)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{amount(a.available_funds)}</td>
                <td className="px-2 py-1 text-right tabular-nums">{amount(a.available_withdrawal_funds)}</td>
                <td className="px-2 py-1 text-right tabular-nums">
                  {amount(a.initial_margin)} <span className="text-xs text-muted">({amount(a.projected_initial_margin)})</span>
                </td>
                <td className="px-2 py-1 text-right tabular-nums">
                  {amount(a.maintenance_margin)} <span className="text-xs text-muted">({amount(a.projected_maintenance_margin)})</span>
                </td>
                <td className={clsx("px-2 py-1 text-right tabular-nums", levelClass(a.im_pct))}>{formatPct(a.im_pct, 1)}</td>
                <td className={clsx("px-2 py-1 text-right tabular-nums", levelClass(a.mm_pct))}>{formatPct(a.mm_pct, 1)}</td>
                <td className="px-2 py-1 text-right tabular-nums text-muted">{amount(a.spot_reserve)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-muted">
        Figures are Deribit&apos;s own (per-asset amounts in that asset; totals in USD). IM % / MM % are margin as a share of
        margin balance; Deribit starts liquidating when maintenance margin reaches {LIQUIDATION_MM_PCT} %.
      </p>
    </div>
  );
}

function levelClass(pct) {
  const level = marginLevel(pct);
  return level === "danger" ? "text-loss" : level === "warn" ? "text-warn" : undefined;
}
