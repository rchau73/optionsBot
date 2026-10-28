// The overview's top row (#404): the status line, the amber "held back"
// lines and the four tiles — coin first, USD second. Each tile has one line
// per bot in that bot's coin; different coins are never added together, only
// their ≈ USD on the footer. Missing data says "unknown: <reason>", never 0.
import { riskRows } from "./account";
import { MISSING, formatCoin, formatDateTime, formatPct, isNumber } from "./format";
import { clockHM, coinOf, plainFreeze, stateWords, worstCaseLine } from "./overview";
import { formatSignedPct, formatSignedUSD, toneOf } from "./summary";

/** Fixed decimals per coin (product owner, #404): BTC 5, ETH 4; any other coin 4. */
export const COIN_DECIMALS = { BTC: 5, ETH: 4 };

export const coinDigits = (coin) => COIN_DECIMALS[coin] ?? 4;

/** A signed coin amount with its coin's fixed decimals: "+0.00400 BTC", "−0.0120 ETH". */
export const fmtCoin = (v, coin) => formatCoin(v, coin, coinDigits(coin));

/** Why a bot's summary is missing, or null. */
function summaryUnknown(bot) {
  const failure = bot.summaryError ?? bot.error;
  if (failure) return `unknown: ${failure}`;
  return bot.summary ? null : "unknown: loading the summary";
}

/**
 * One tile line per bot: { bot, coin, value (coin), usd, text, tone, note, unknown }.
 * text is the coin figure, or the reason it is unknown.
 */
function line(bot, value, usd, extra = {}) {
  return { bot: bot.name, coin: coinOf(bot), value, usd, text: fmtCoin(value, coinOf(bot)), tone: toneOf(value), unknown: null, ...extra };
}
const unknownLine = (bot, why) => ({ bot: bot.name, coin: coinOf(bot), value: null, usd: null, text: why, tone: "muted", unknown: why });

/** Coins earned this month: realised, net of fees, from the journal's closes. */
export function earnedLines(bots) {
  return bots.map((b) => {
    const why = summaryUnknown(b);
    if (why) return unknownLine(b, why);
    const m = b.summary.income?.this_month;
    if (!(m?.closes > 0)) return { ...line(b, 0, 0), text: "not yet: no close this month", tone: "muted", note: "nothing closed yet" };
    return line(b, m.realised_coin, m.realised_usd, { note: `${m.closes} close${m.closes === 1 ? "" : "s"}` });
  });
}

/** Open now: unrealised, net of opening fees. */
export function openLines(bots) {
  return bots.map((b) => {
    const why = summaryUnknown(b);
    if (why) return unknownLine(b, why);
    const v = b.summary.income?.unrealised_coin;
    if (!isNumber(v)) return unknownLine(b, "unknown: no mark yet");
    return line(b, v, b.summary.income.unrealised_usd, { note: "not realised" });
  });
}

/** Worst case: one very bad day, in coin, with its % of the bot's account as the note. */
export function worstLines(bots) {
  return bots.map((b) => {
    const why = summaryUnknown(b);
    if (why) return unknownLine(b, why);
    const w = worstCaseLine(b.summary.worst_case);
    if (w.unknown) return unknownLine(b, `unknown: ${w.unknown}`);
    const pct = isNumber(w.pct) ? `${formatSignedPct(-Math.abs(w.pct))} of account` : "% of account unknown";
    return { ...line(b, -Math.abs(w.lossCoin), -Math.abs(w.lossUSD), { note: pct }), tone: "loss", pct: w.pct };
  });
}

/**
 * Margin used per bot: { bot, coin, ratio (% of limit), imPct, limitPct, room (% of limit left), text, tone, unknown }.
 */
export function marginLines(bots) {
  const rows = Object.fromEntries(riskRows(bots).map((r) => [r.bot, r]));
  return bots.map((b) => {
    const r = rows[b.name];
    const coin = coinOf(b);
    const why = !r ? "unknown: no margin reading yet" : r.error ? `unknown: ${r.error}` : !isNumber(r.imPct) || !isNumber(r.limitIMPct) || r.limitIMPct <= 0 ? "unknown: no margin reading yet" : null;
    if (why) return { bot: b.name, coin, ratio: null, room: null, text: why, tone: "muted", unknown: why };
    const ratio = (r.imPct / r.limitIMPct) * 100;
    return {
      bot: b.name,
      coin,
      ratio,
      imPct: r.imPct,
      limitPct: r.limitIMPct,
      room: Math.max(0, 100 - ratio),
      text: `${formatPct(ratio, 0)} of limit`,
      tone: ratio > 100 ? "loss" : undefined,
      note: `IM ${formatPct(r.imPct, 1)} of ${formatPct(r.limitIMPct, 0)}`,
      unknown: null,
    };
  });
}

/**
 * The money tiles' footer: "≈ +$410 at today's prices", the sum of each
 * bot's USD figure (USD is one unit, so it may be summed; coins never are).
 * A bot without a figure is named, so the sum is never silently partial.
 */
export function usdFooter(lines) {
  const known = lines.filter((l) => !l.unknown && isNumber(l.usd));
  const missing = lines.filter((l) => l.unknown).map((l) => l.coin);
  if (!known.length) return "≈ USD unknown";
  const sum = known.reduce((a, l) => a + l.usd, 0);
  return `≈ ${formatSignedUSD(sum)} at today's prices${missing.length ? ` · without ${missing.join(", ")}` : ""}`;
}

/** The margin tile's footer: "room left: BTC 29% · ETH 10% of limit". */
export function roomFooter(lines) {
  const parts = lines.map((l) => `${l.coin} ${l.unknown ? "unknown" : formatPct(l.room, 0)}`);
  return parts.length ? `room left: ${parts.join(" · ")} of limit` : "room left: unknown";
}

/** "Both trading", "BTC trading", "All 3 trading"; null when not every bot trades normally. */
function allTrading(n, bots) {
  if (n === 1) return `${coinOf(bots[0])} trading`;
  if (n === 2) return "Both trading";
  return `All ${n} trading`;
}

/**
 * The status line: { tone: ok | warn | bad, text } — "Both trading · updated 14:02",
 * or each bot's state when one is not running normally
 * ("BTC trading · ETH new trades paused · updated 14:02").
 */
export function statusLine(bots, now = Date.now()) {
  if (!bots?.length) return { tone: "warn", text: "No bot answered yet" };
  const states = bots.map((b) => ({ coin: coinOf(b), words: stateWords(b, now) }));
  const latest = bots
    .map((b) => Date.parse(b.status?.as_of ?? ""))
    .filter((t) => !Number.isNaN(t))
    .sort((a, b) => b - a)[0];
  const updated = latest ? `updated ${clockHM(new Date(latest).toISOString())}` : "not updated yet";
  const normal = states.every((s) => s.words === "running normally");
  if (normal) return { tone: "ok", text: `${allTrading(bots.length, bots)} · ${updated}` };
  const bad = states.some((s) => /^(halted|offline|loop stalled|stale|state unknown)/.test(s.words));
  const words = states.map((s) => `${s.coin} ${s.words === "running normally" ? "trading" : s.words}`);
  return { tone: bad ? "bad" : "warn", text: `${words.join(" · ")} · ${updated}` };
}

/** One held leg in plain words: what the bot does not sell and why. */
export function heldSentence(coin, h) {
  const side = h.missing === "call" ? "call" : "put";
  switch (h.rule) {
    case "regime_side":
      return `${coin} sells no new ${side}s: negative gamma regime in a ${h.trend || (side === "put" ? "bear" : "bull")} trend`;
    case "gex_shed":
    case "gex_shedding":
      return `${coin} sells no new ${side}s: the price is past the gamma flip (GEX shed)`;
    case "stopped": {
      const until = formatDateTime(h.cooldown_until);
      return `${coin} waits to re-sell the ${side}: it was stopped out${until !== MISSING ? `, cooldown until ${until}` : ""}`;
    }
    case "mm_breach":
      return `${coin} sells nothing new: maintenance margin is at its limit`;
    case "stop_deferred":
      return `${coin} sells nothing new: a stop-loss waits for a normal spread`;
    case "gex_wait":
      return `${coin} sells nothing new: waiting for a fresh GEX reading`;
    case "churn_paused":
      return `${coin} pauses the ${side}: too many buy-backs and re-sales in an hour`;
    default:
      return `${coin} holds back the ${side}: ${h.reason || h.rule}`;
  }
}

/** Rules that hold nothing back (repair may sell, or is already working on it). */
const NOT_HELD = new Set(["", "repair_working", "rollout_window", "unconfirmed_order"]);

/**
 * One amber line per bot that holds something back: { bot, text } — its held
 * legs' sentences (distinct, joined) and a freeze of new risk.
 */
export function heldBackLines(bots) {
  const out = [];
  for (const b of bots ?? []) {
    const coin = coinOf(b);
    const parts = [];
    if (b.status?.risk?.status?.frozen) parts.push(`${coin} opens nothing new: ${plainFreeze(b.status.risk.status.freeze_reason).replace(/^waiting: /, "")}`);
    for (const h of b.status?.held_legs ?? []) {
      if (NOT_HELD.has(h.rule ?? "")) continue;
      const s = heldSentence(coin, h);
      if (!parts.includes(s)) parts.push(s);
    }
    if (parts.length) out.push({ bot: b.name, text: parts.join("; ") });
  }
  return out;
}
