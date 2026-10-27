// Pure helpers for the "How it works" page (#322): a short strangle in three
// pictures, from the bot's nearest strangle (/api/summary → strangle) or, when
// it holds none, from a labelled example. Every live number comes from the API.
import { formatDate, isNumber } from "./format";
import { stopWord } from "./worstcase";

/**
 * Example strangles, one per coin: about a 0.16-delta pair 30 days out at
 * IV 50 %, exit lines at |Δ| 0.30. Shown only under an "Example" label.
 */
const EXAMPLES = {
  BTC: { spot: 100000, put: { strike: 88000, premium: 0.01, exit: 93900 }, call: { strike: 116000, premium: 0.008, exit: 106500 } },
  ETH: { spot: 4000, put: { strike: 3500, premium: 0.012, exit: 3750 }, call: { strike: 4650, premium: 0.009, exit: 4260 } },
};

/** The coin's price at expiry where a coin-settled strangle neither earns nor loses (as internal/api BreakEvens). */
export function breakEvens(put, call, premium) {
  const low = put.qty > 0 ? (put.qty * put.strike) / (premium + put.qty) : null;
  const high = call.qty > premium ? (call.qty * call.strike) / (call.qty - premium) : null;
  return { low, high };
}

/** What the strangle pays at expiry (coin) if the coin ends at price s. */
export function payoffAt(fig, s) {
  const put = (fig.put.qty * Math.max(fig.put.strike - s, 0)) / s;
  const call = (fig.call.qty * Math.max(s - fig.call.strike, 0)) / s;
  return fig.premium - put - call;
}

/**
 * The page's figures: { live, coin, spot, expiryText, put, call, premium,
 * low, high, exitLines, exitReason, note }. put/call: { strike, qty, premium, exit }.
 * Live when the bot holds a two-legged strangle; otherwise the coin's example
 * (note says why), never zeros.
 */
export function strangleFigures(summary) {
  const coin = summary?.underlying || "BTC";
  const st = summary?.strangle;
  if (st && st.put?.strike > 0 && st.call?.strike > 0 && st.spot > 0) {
    const leg = (l) => ({ strike: l.strike, qty: l.qty, premium: l.premium_coin, exit: isNumber(l.exit_line) ? l.exit_line : null });
    return {
      live: true,
      coin,
      spot: st.spot,
      expiryText: formatDate(st.expiry),
      put: leg(st.put),
      call: leg(st.call),
      premium: st.premium_coin,
      low: isNumber(st.break_even_low) ? st.break_even_low : null,
      high: isNumber(st.break_even_high) ? st.break_even_high : null,
      exitLines: Boolean(st.exit_lines),
      exitReason: st.exit_lines ? null : st.exit_lines_reason || "not computed",
      note: null,
    };
  }
  const ex = EXAMPLES[coin] ?? EXAMPLES.BTC;
  const exCoin = EXAMPLES[coin] ? coin : "BTC";
  const put = { strike: ex.put.strike, qty: 1, premium: ex.put.premium, exit: ex.put.exit };
  const call = { strike: ex.call.strike, qty: 1, premium: ex.call.premium, exit: ex.call.exit };
  const premium = ex.put.premium + ex.call.premium;
  const { low, high } = breakEvens(put, call, premium);
  return {
    live: false,
    coin: exCoin,
    spot: ex.spot,
    expiryText: "a date 30 days away",
    put,
    call,
    premium,
    low,
    high,
    exitLines: true,
    exitReason: null,
    note: summary?.holding?.legs
      ? "The bot holds no complete strangle right now (one leg only), so the numbers below are an example."
      : summary
        ? "The bot holds nothing right now, so the numbers below are an example."
        : "No answer from the bot yet, so the numbers below are an example.",
  };
}

/** "Example: a BTC strangle like the ones the bot sells"; "an ETH …". */
export function exampleLabel(coin) {
  return `Example: ${/^[AEIOU]/.test(coin) ? "an" : "a"} ${coin} strangle like the ones the bot sells`;
}

/** Days from repair_cooldown_hours: "3 days", "1 day", "36 hours"; null when unknown. */
export function cooldownWords(hours) {
  if (!isNumber(hours) || hours <= 0) return null;
  if (hours % 24 === 0) return `${hours / 24} day${hours === 24 ? "" : "s"}`;
  return `${hours} hours`;
}

/** The stop-loss words for picture 3: "twice", "3 times"; null when unknown. */
export { stopWord };

/**
 * Picture 2's curve: n points { s, y } over a price range around the strikes
 * and break-evens, y = payoffAt (coin). Range = break-evens ± 40 % of the
 * distance between the strikes.
 */
export function payoffPoints(fig, n = 61) {
  const width = fig.call.strike - fig.put.strike;
  const pad = Math.max(width, fig.spot * 0.05) * 0.4;
  const lo = Math.max((fig.low ?? fig.put.strike) - pad, fig.put.strike * 0.5);
  const hi = (fig.high ?? fig.call.strike * 1.1) + pad;
  const out = [];
  for (let i = 0; i < n; i++) {
    const s = lo + ((hi - lo) * i) / (n - 1);
    out.push({ s, y: payoffAt(fig, s) });
  }
  return out;
}
