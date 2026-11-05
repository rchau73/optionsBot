import { formatCoin, isNumber } from "./format";
import { formatSignedPct, formatSignedUSD, monthLabel } from "./summary";
// Pure helpers for the income page (/api/income, #319). Amounts keep their
// sign; costs (buy-backs, fees) are shown as negatives.

const MONTH_NAMES = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

/** The unit toggle is remembered per browser under this key. */
export const UNIT_STORAGE_KEY = "income.unit";

/** The toggle's choices: the coin, USD, or % of the start balance (#629). */
export const INCOME_UNITS = ["coin", "usd", "pct"];

/** A signed amount in the chosen unit: "coin" → "+0.01200 BTC", "usd" → "+$1,200". */
export function formatAmount(v, unit, coin) {
  return unit === "usd" ? formatSignedUSD(v) : formatCoin(v, coin);
}

/**
 * The table rows, newest first. Each money column in the chosen unit; costs
 * negative; a month without closes says so (no numbers). Holding alone is 0 in
 * coin (a held coin stays one coin) and the equity's USD change in USD — null
 * with its reason when the history does not have it.
 */
export function incomeTableRows(income, unit) {
  const months = income?.months ?? [];
  const last = months.length - 1;
  const usd = unit === "usd";
  return months
    .map((m, i) => {
      const traded = monthTraded(m);
      return {
        month: m.month,
        label: monthLabel(m.month),
        current: i === last,
        closes: m.closes ?? 0,
        traded,
        loss: traded && (usd ? m.realised_usd : m.realised_coin) < 0,
        premium: usd ? m.premium_usd : m.premium_coin,
        buyback: negate(usd ? m.buyback_usd : m.buyback_coin),
        fees: negate(usd ? m.fees_usd : m.fees_coin),
        hedge: (usd ? m.hedge_usd : m.hedge_coin) ?? 0,
        result: usd ? m.realised_usd : m.realised_coin,
        hold: usd ? (m.hold_usd ?? null) : 0,
        holdReason: usd && !isNumber(m.hold_usd) ? m.hold_reason || "not recorded" : null,
      };
    })
    .reverse();
}

/** The futures hedge made or cost something in this month (#665). */
function monthHedged(m) {
  return (m.hedge_fills ?? 0) > 0 || (isNumber(m.hedge_coin) && m.hedge_coin !== 0);
}

/** The month has a result: a close, or futures hedge activity. */
export function monthTraded(m) {
  return (m.closes ?? 0) > 0 || monthHedged(m);
}

/** True when a month shown has a futures hedge figure: the table then shows that source. */
export function hasHedgeIncome(income) {
  return (income?.months ?? []).some(monthHedged);
}

function negate(v) {
  return isNumber(v) ? (v === 0 ? 0 : -v) : null;
}

/** "July: BTC fell 14.0 % in 5 days"; null when the move was not recorded. */
export function worstMonthMove(worst, coin) {
  if (!worst || !isNumber(worst.spot_move_pct)) return null;
  const m = /^(\d{4})-(\d{2})$/.exec(worst.month ?? "");
  const name = m ? MONTH_NAMES[Number(m[2]) - 1] : worst.month;
  const pct = worst.spot_move_pct;
  const verb = pct < 0 ? "fell" : pct > 0 ? "rose" : "was flat";
  const size = pct === 0 ? "" : ` ${Math.abs(pct).toFixed(1)} %`;
  const days = isNumber(worst.spot_days) ? ` in ${Math.max(1, Math.round(worst.spot_days))} day${Math.round(worst.spot_days) === 1 ? "" : "s"}` : "";
  return `${name}: ${coin} ${verb}${size}${days}`;
}

/** "3 months up, 1 down". */
export function upDownLine(s) {
  const up = s?.up_months ?? 0;
  const down = s?.down_months ?? 0;
  return `${up} month${up === 1 ? "" : "s"} up, ${down} down`;
}

/** "+1.2 % more BTC than holding", "1.2 % less BTC than holding", or the reason the % is unknown. */
export function returnLine(s, coin) {
  if (!isNumber(s?.return_pct)) return `% of the account unknown: ${s?.equity_note || "no balance recorded"}`;
  if (Number(s.return_pct.toFixed(1)) < 0) return `${Math.abs(s.return_pct).toFixed(1)} % less ${coin} than holding`;
  return `${formatSignedPct(s.return_pct).replace("%", " %")} more ${coin} than holding`;
}

/** The worst week's % of the account, or why unknown. */
export function worstWeekPct(ww) {
  if (!ww?.known) return null;
  if (isNumber(ww.pct)) return `${formatSignedPct(ww.pct).replace("%", " %")} of the account`;
  return ww.pct_note ? `% of the account unknown: ${ww.pct_note}` : "% of the account unknown";
}

/** A signed % with a space before the sign: "+1.25 %", "−0.65 %"; "—" when missing. */
export function formatStartPct(v) {
  return formatSignedPct(v, 2).replace("%", " %");
}

/**
 * The "% of start" table rows (#629), newest first: each month from its own
 * first balance, "partial: from <date>" when that balance came after the month
 * began, and the reason with no number when the month has none.
 */
export function startTableRows(income) {
  const months = income?.start?.months ?? [];
  const last = months.length - 1;
  return months
    .map((m, i) => ({
      month: m.month,
      label: monthLabel(m.month),
      current: i === last,
      partialFrom: m.partial ? m.from : null,
      reason: m.reason || (isNumber(m.result_pct) ? null : "no balance recorded"),
      balanceCoin: m.balance_coin ?? null,
      balanceUSD: m.balance_usd ?? null,
      closes: m.closes ?? 0,
      result: m.result_pct ?? null,
      hold: m.hold_pct ?? null,
      combined: m.combined_pct ?? null,
      loss: isNumber(m.result_pct) && m.result_pct < 0,
    }))
    .reverse();
}

/** The amount of a tile in the chosen unit; null when missing. */
export function tileAmount(amount, unit) {
  if (!amount) return null;
  return unit === "usd" ? amount.usd : amount.coin;
}
