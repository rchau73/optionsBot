import { formatNumber, isNumber } from "./format";
import { loopStalled } from "./monitor";
import { localParts } from "./time";
// Pure helpers for the phone overview (#320) and the first-day panel (#321).
// Every figure comes from /api/summary and /api/status; a missing one says
// "unknown" or "not yet", never 0.

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const MONTH_NAMES = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

/** The bot's coin: the status's underlying, else its name. */
export function coinOf(bot) {
  return bot?.summary?.underlying || bot?.status?.underlying || (bot?.name ?? "").toUpperCase();
}

/** The bot to show on a phone: the one named, else the first. */
export function pickBot(bots, name) {
  return bots.find((b) => b.name === name) ?? bots[0] ?? null;
}

/**
 * A brand-new bot (#321): its summary is in, it holds nothing and its journal
 * has no trade yet. A bot that is flat after trading is not a first day.
 */
export function isFirstDay(bot) {
  const s = bot?.summary;
  if (!s || bot.summaryError || bot.error) return false;
  return !(s.holding?.legs > 0) && !s.last_action;
}

/** "20 minutes", "1 hour", "3 days"; null when unknown. */
export function startedAgo(sec) {
  if (!isNumber(sec) || sec < 0) return null;
  const unit = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
  if (sec < 60) return "less than a minute";
  if (sec < 3600) return unit(Math.floor(sec / 60), "minute");
  if (sec < 48 * 3600) return unit(Math.floor(sec / 3600), "hour");
  return unit(Math.floor(sec / 86400), "day");
}

/**
 * A freeze reason (internal/risk) in plain words: what the bot waits for.
 * Unknown reasons are shown as given.
 */
export function plainFreeze(reason) {
  if (!reason) return "waiting: new trades are paused";
  const days = /needs (\d+) daily closes/.exec(reason);
  if (/gamma regime not confirmed/.test(reason)) return `waiting: the market's mood must hold for ${days ? days[1] : "a few"} days`;
  if (/gamma regime turned/.test(reason)) return "waiting: the market's mood changed and must hold for a few days";
  if (/DVOL moved/.test(reason)) return "waiting: the expected swing changed and must hold for a few days";
  return `waiting: ${reason}`;
}

/**
 * The four first-day steps (#321): { key, state: done | now | next | then,
 * when, title, text }. Connected is done (the bot answers); reading the
 * market is done once a GEX snapshot exists and new risk is not frozen.
 */
export function firstDaySteps(bot) {
  const st = bot?.status ?? {};
  const coin = coinOf(bot);
  const testnet = st.environment !== "live";
  const equity = st.account?.equity;
  const balance = isNumber(equity) && equity > 0 ? `Balance ${formatNumber(equity, 4)} ${coin}${testnet ? " (practice on testnet)" : ""}.` : "Balance not read yet.";
  const gexReady = Boolean(st.market?.gex_regime);
  const frozen = Boolean(st.risk?.status?.frozen);
  const reading = gexReady && !frozen;

  let readingWhen = "Done";
  if (!gexReady) readingWhen = "Now · about 1 more minute";
  else if (frozen) readingWhen = `Now · ${plainFreeze(st.risk.status.freeze_reason)}`;

  return [
    {
      key: "connected",
      state: "done",
      when: "Done",
      title: "Connected to your account",
      text: `${balance} It can trade options only; it can't move money.`,
    },
    {
      key: "reading",
      state: reading ? "done" : "now",
      when: readingWhen,
      title: "Reading the market",
      text: `It checks how big option traders are positioned and how much ${coin} is expected to swing. No trade until it knows.`,
    },
    {
      key: "positions",
      state: reading ? "now" : "next",
      when: reading ? "Now · usually within the hour" : "Next · usually within the hour",
      title: "First positions",
      text: "“What it holds” shows its floor and ceiling, and what you were paid for them.",
    },
    {
      key: "worst",
      state: "then",
      when: "Then · from the first position",
      title: "Worst case, in money",
      text: "What a very bad day would cost, updated every minute.",
    },
  ];
}

/** "1–19 Oct": the current UTC month up to the summary's as_of; null when unknown. */
export function monthSoFar(asOf) {
  const t = Date.parse(asOf ?? "");
  if (Number.isNaN(t)) return null;
  const d = new Date(t);
  const day = d.getUTCDate();
  const mon = MONTHS[d.getUTCMonth()];
  return day === 1 ? `1 ${mon}` : `1–${day} ${mon}`;
}

/**
 * The bot's state in a few words for the headline: the header chip's words
 * when something is wrong, else "running normally".
 */
export function stateWords(bot, now = Date.now()) {
  const s = bot?.status;
  if (s?.halted) return "halted: kill switch";
  if (bot?.stale) return bot.error ? "offline" : "stale: no update in the last seconds";
  if (loopStalled(s, now)) return "loop stalled";
  if (s?.risk?.im_over_limit) return "IM over limit";
  if (s?.risk?.status?.frozen) return "new trades paused";
  if (!s) return "state unknown";
  return "running normally";
}

/** Warning chips for the phone header, as the desktop header shows them. */
export function warningChips(bot, now = Date.now()) {
  const s = bot?.status;
  const chips = [];
  if (s?.halted) chips.push("HALTED");
  if (bot?.stale) chips.push(bot.error ? "OFFLINE" : "STALE");
  else if (loopStalled(s, now)) chips.push("LOOP STALLED");
  if (s?.risk?.im_over_limit) chips.push("IM over limit");
  return chips;
}

/**
 * The range bar of the nearest strangle: positions in % of the bar for the
 * band (put → call) and the spot marker. One-legged → { missing: "put" | "call",
 * strike }. null when nothing is held or spot is unknown.
 */
export function rangeBar(nearest, spot) {
  if (!nearest || !isNumber(spot) || spot <= 0) return null;
  const put = nearest.put_strike;
  const call = nearest.call_strike;
  if (!isNumber(put) && !isNumber(call)) return null;
  if (!isNumber(put) || !isNumber(call)) {
    return { missing: isNumber(put) ? "call" : "put", strike: isNumber(put) ? put : call, spot, expiry: nearest.expiry };
  }
  const lo = Math.min(put, spot);
  const hi = Math.max(call, spot);
  const pad = Math.max((hi - lo) * 0.15, spot * 0.01);
  const min = lo - pad;
  const span = hi + pad - min;
  const at = (v) => ((v - min) / span) * 100;
  return {
    put,
    call,
    spot,
    expiry: nearest.expiry,
    bandLeft: at(put),
    bandWidth: at(call) - at(put),
    marker: at(spot),
    inside: spot >= put && spot <= call,
  };
}

/**
 * Six month bars, oldest first: { month, label, value, closes, loss, height }
 * (height in % of the largest |value|), and the aria label, e.g.
 * "Last 6 months: 5 up, July down".
 */
export function monthBars(income) {
  const months = income?.months ?? [];
  const biggest = Math.max(0, ...months.map((m) => Math.abs(m.realised_coin ?? 0)));
  const bars = months.map((m) => {
    const idx = Number((m.month ?? "").slice(5, 7)) - 1;
    const value = m.realised_coin;
    const closes = m.closes ?? 0;
    return {
      month: m.month,
      label: MONTHS[idx] ?? m.month,
      name: MONTH_NAMES[idx] ?? m.month,
      value,
      closes,
      loss: closes > 0 && value < 0,
      height: closes > 0 && biggest > 0 ? Math.max(6, (Math.abs(value) / biggest) * 100) : 0,
    };
  });
  const up = bars.filter((b) => b.closes > 0 && b.value > 0).length;
  const down = bars.filter((b) => b.loss).map((b) => b.name);
  const empty = bars.filter((b) => b.closes === 0).length;
  const parts = [`${up} up`, down.length ? `${down.join(" and ")} down` : "none down"];
  if (empty) parts.push(`${empty} without closes`);
  return { bars, label: `Last ${bars.length} months: ${parts.join(", ")}` };
}

/**
 * The worst-case card's figures: the worse of the two modelled moves, its
 * signed move %, loss in coin and USD, % of the account. null with the
 * reason when the model is not available.
 */
export function worstCaseLine(wc) {
  if (!wc?.available) return { unknown: wc?.reason || "not computed yet" };
  const down = wc.down ?? {};
  const up = wc.up ?? {};
  const worse = (up.loss_coin ?? -Infinity) > (down.loss_coin ?? -Infinity) ? { ...up, sign: 1 } : { ...down, sign: -1 };
  return {
    move: worse.sign * (wc.move_pct ?? 0),
    lossCoin: worse.loss_coin,
    lossUSD: worse.loss_usd,
    pct: isNumber(worse.pct_of_equity) ? worse.pct_of_equity : null,
  };
}

/** "14:02" in the display time zone; null when unknown. */
export function clockHM(iso) {
  const p = iso ? localParts(iso) : null;
  return p ? `${p.hour}:${p.minute}` : null;
}

// One-line summaries of the folded detail panels (#288): facts, no amount the
// investor summary does not already show.

/** "4 legs, 2 strangles" over every bot; "none open". */
export function positionsSummary(bots) {
  let legs = 0;
  let strangles = 0;
  for (const b of bots ?? []) {
    legs += b.status?.open_legs ?? 0;
    strangles += b.status?.open_strangles ?? 0;
  }
  if (!legs) return "none open";
  return `${legs} leg${legs === 1 ? "" : "s"}, ${strangles} strangle${strangles === 1 ? "" : "s"}`;
}

/** "last: 14:02 BTC Filled"; "nothing yet". */
export function activitySummary(feed) {
  const e = feed?.[0];
  if (!e) return "nothing yet";
  return `last: ${clockHM(e.at) ?? "—"} ${e.bot.toUpperCase()} ${e.label}`;
}

/** "2 working" over every bot; "none". */
export function ordersSummary(bots) {
  const n = (bots ?? []).reduce((acc, b) => acc + (b.status?.pending ?? 0), 0);
  return n ? `${n} working` : "none";
}

/**
 * The band's last-trade line (#489): { unknown } without the summary, { none }
 * before the first trade, else { when, what, reason, detail, pnlCoin, pnlUSD }
 * from the summary's last_action — the reason is the journal's own sentence.
 */
export function lastTradeLine(bot) {
  const s = bot?.summary;
  if (!s) return { unknown: bot?.summaryError || bot?.error || "not loaded yet" };
  const a = s.last_action;
  if (!a) return { none: true };
  const close = a.kind === "close";
  return {
    when: a.at,
    what: `${close ? "bought back" : "sold"} ${isNumber(a.qty) ? formatNumber(a.qty, 2) : "?"} × ${a.instrument || "a leg"}`,
    reason: a.reason || a.rule || "no reason recorded",
    detail: a.detail || null,
    pnlCoin: close && isNumber(a.pnl_coin) ? a.pnl_coin : null,
    pnlUSD: close && isNumber(a.pnl_usd) ? a.pnl_usd : null,
  };
}

/** The coin's move since a price, in %, signed; null when either is unknown. */
export function moveSince(then, now) {
  if (!isNumber(then) || !isNumber(now) || then <= 0) return null;
  return ((now - then) / then) * 100;
}
