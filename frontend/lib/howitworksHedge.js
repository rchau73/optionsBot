// The futures hedge on "How it works" (#696): which story the page tells and
// the delta picture's figures. No React, no fetching. `h` is the `hedge` object
// of GET /api/bots/<bot>/hedge, null while it is unknown. Every number comes
// from it: the band and the order cap are fixed.go's, through the hedge view.
import { formatCoin, isNumber } from "./format";
import { PERP_FLAT_USD, hedgeState } from "./futuresHedge";

/**
 * "on", "paused", "not_holding" (on, but it cannot hold the book: the shed is
 * back), "off_open", "off", "idle" (the kill switch fired); null without a reading.
 */
export function hedgeStory(h) {
  if (!h) return null;
  if (h.kill_switch) return "idle";
  const state = hedgeState(h);
  return state === "on" && h.not_holding ? "not_holding" : state;
}

export const STORY_BADGE = {
  on: { tone: "good", text: "ON" },
  paused: { tone: "warn", text: "PAUSED" },
  not_holding: { tone: "warn", text: "NOT HOLDING" },
  off_open: { tone: "warn", text: "OFF · position still open" },
  off: { tone: "muted", text: "OFF" },
  idle: { tone: "warn", text: "KILL SWITCH" },
};

const SHED = "the bot buys back the side the market moves towards";

/** Picture 3's "Market turns" card, following the hedge; `h` null keeps it general. */
export function marketTurnsWords(h) {
  switch (hedgeStory(h)) {
    case "on":
      return "In a negative regime the bot keeps both legs: the perpetual cancels the lean, so the GEX shed buys nothing back.";
    case "paused":
    case "not_holding":
      return `The hedge cannot trade right now (${h.halt_reason || h.not_holding}), so the GEX shed protects the book until the hedge can trade: ${SHED}.`;
    case "off":
    case "off_open":
      return `The GEX shed: in a negative regime ${SHED}. ${h.available && h.testnet ? "You can turn the futures hedge on in Account instead." : "The futures hedge is a testnet experiment, so it cannot be turned on for this bot."}`;
    case "idle":
      return "The kill switch has fired: the bot is idle, so neither the hedge nor the GEX shed acts.";
    default:
      return `The GEX shed: in a negative regime ${SHED}. With the futures hedge on, both legs stay and the perpetual cancels the lean.`;
  }
}

/**
 * The delta picture's numbers in coin: perp is null with no position, band
 * null while unknown, scale the half-width of the axis. Null with nothing to
 * draw: no reading, or neither options nor a perpetual held.
 */
export function deltaFigures(h) {
  if (!h || !isNumber(h.options_delta_coin)) return null;
  const perp = isNumber(h.perp_delta_coin) && Math.abs(h.perp_usd ?? 0) >= PERP_FLAT_USD ? h.perp_delta_coin : null;
  if (perp == null && h.options_delta_coin === 0) return null;
  const options = h.options_delta_coin;
  const net = isNumber(h.net_delta_coin) ? h.net_delta_coin : options + (perp ?? 0);
  const band = h.band_coin > 0 ? h.band_coin : null;
  return { options, perp, net, band, scale: Math.max(Math.abs(options), Math.abs(perp ?? 0), Math.abs(net), band ?? 0) * 1.1 };
}

/** The picture in words: its text alternative and the line under it. */
export function deltaWords(f, unit) {
  const coin = (v) => formatCoin(v, unit, 4);
  const perp = f.perp == null ? "no perpetual held" : `perpetual ${coin(f.perp)}`;
  const band = f.band == null ? "the hedge band is unknown right now" : `${Math.abs(f.net) < f.band ? "inside" : "outside"} the hedge band of ±${f.band.toFixed(4)} ${unit}`;
  return `Options' delta ${coin(f.options)}; ${perp}; net ${coin(f.net)}, ${band}.`;
}

/** "at most 12 orders an hour"; a bot without the figure gets the same rule in words. */
export const orderCapWords = (h) => (h?.max_orders_per_hour > 0 ? `at most ${h.max_orders_per_hour} orders an hour` : "a limited number of orders an hour");

/** " (±0.0578 BTC, 5 % of the margin balance)"; empty without a band. */
export function bandSize(h, unit) {
  if (!(h?.band_coin > 0)) return "";
  return ` (±${h.band_coin.toFixed(4)} ${unit}${h.band_pct > 0 ? `, ${h.band_pct} % of the margin balance` : ""})`;
}
