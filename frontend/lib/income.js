import { formatCoin, isNumber } from "./format";
import { formatSignedPct, formatSignedUSD, monthLabel } from "./summary";
// Pure helpers for the income page (/api/income, #319). Amounts keep their
// sign; costs (buy-backs, fees) are shown as negatives.

const MONTH_NAMES = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

/** The unit toggle is remembered per browser under this key. */
export const UNIT_STORAGE_KEY = "income.unit";

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
      const closes = m.closes ?? 0;
      return {
        month: m.month,
        label: monthLabel(m.month),
        current: i === last,
        closes,
        loss: closes > 0 && (usd ? m.realised_usd : m.realised_coin) < 0,
        premium: usd ? m.premium_usd : m.premium_coin,
        buyback: negate(usd ? m.buyback_usd : m.buyback_coin),
        fees: negate(usd ? m.fees_usd : m.fees_coin),
        result: usd ? m.realised_usd : m.realised_coin,
        hold: usd ? (m.hold_usd ?? null) : 0,
        holdReason: usd && !isNumber(m.hold_usd) ? m.hold_reason || "not recorded" : null,
      };
    })
    .reverse();
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
  return isNumber(ww.pct) ? `${formatSignedPct(ww.pct).replace("%", " %")} of the account` : "% of the account unknown";
}

/** The amount of a tile in the chosen unit; null when missing. */
export function tileAmount(amount, unit) {
  if (!amount) return null;
  return unit === "usd" ? amount.usd : amount.coin;
}
