// Server-side only: one question through the assistant (#191). The help
// passages that match it and, when a bot is named, that bot's live page data
// (#427: its open legs and the market now) and its journal facts of the last
// JOURNAL_DAYS plus the whole history of the legs it holds (#417, #427: every
// open, close and fill with the market it recorded, plus the other facts most
// relevant to the question, within a budget) go to the model; its answer passes the guard before anyone sees it,
// and the question, the sources, the model's text and what was shown are
// logged for review. An answer with no citation becomes "I don't know"; an
// answer rejected for a number or a citation from nowhere, or for sending the
// person to a document, or for not being in English (#565), is asked once
// more with what failed (#534); any other
// rejected answer, or a second rejection, becomes the fallback reply.
import { REASON_LANGUAGE, REASON_NO_CITATION, REASON_POINTS_TO_DOCUMENT, REASON_UNGROUNDED_NUMBER, REASON_UNKNOWN_CITATION, check, isCanonical, isEnglish } from "./guard";
import { appendLine, newAnswerId } from "./log";
import { AI_NOTICE, DONT_KNOW, FALLBACK_REPLY, REFUSAL } from "./phrases";
import { REQUEST_ANSWERED, REQUEST_DRAFT, REQUEST_NONE, checkRequest, emptyChecklist, looksLikeReport } from "./request";
import { HelpIndex, noAnswer } from "./search";
import { DRAFT_TEXT, NEEDS_INFO_TEXT } from "@/lib/assistantRequests";
import { formatAge, formatCoin, formatDateTime, formatPct, formatPrice } from "@/lib/format";
import { plainFreeze } from "@/lib/overview";
import { gexFreshness } from "@/lib/regime";
import { formatSignedPct, formatSignedUSD } from "@/lib/summary";

export const MAX_QUESTION = 500;
// Passages of the help sent with a question: enough for an answer, few enough to keep the call small.
export const HELP_PASSAGES = 6;
// The bot's journal read for a question about it: its last days (/api/journal,
// repeats collapsed by the bot), read once a minute per bot.
export const JOURNAL_DAYS = 7;
export const JOURNAL_CACHE_MS = 60_000;
// Journal facts sent with one question: at most this many passages and about
// this many tokens (≈ 4 characters each) — with the help, ≲ 8k input tokens.
export const JOURNAL_PASSAGES = 60;
export const JOURNAL_TOKENS = 6000;
// Events that change the book: every one of the window goes to the model.
const TRADE_EVENTS = new Set(["filled", "closed"]);
// A decision this close to a trade sent is sent with it, budget allowing.
const AROUND_MS = 30 * 60_000;

// Outcomes, logged and returned.
export const OUTCOME_ANSWERED = "answered";
export const OUTCOME_DONT_KNOW = "dont_know"; // the model said it, or no passage matched (no model call)
export const OUTCOME_REFUSED = "refused";
export const OUTCOME_NO_CITATION = "no_citation"; // replaced by "I don't know"
export const OUTCOME_REJECTED = "rejected"; // replaced by the fallback reply
export const OUTCOME_MODEL_ERROR = "model_error";
// The request check's outcomes (#409); its "answered" is OUTCOME_ANSWERED with a checklist.
export const OUTCOME_NEEDS_INFO = "needs_info";
export const OUTCOME_DRAFT = "draft";

/** The question of a request body, or an error naming what is wrong. */
export function validQuestion(body) {
  const q = typeof body?.question === "string" ? body.question.trim() : "";
  if (!q) return { error: 'body must be {"question": "…", "bot": "btc"?}' };
  if (q.length > MAX_QUESTION) return { error: `question longer than ${MAX_QUESTION} characters` };
  return { question: q };
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n) => String(n).padStart(2, "0");

/** "20 Oct 08:00" (UTC) for an ISO time; the text as given when it is not one. */
export function shortTime(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return String(iso);
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}

const UNKNOWN = "unknown";
const isNum = (v) => typeof v === "number" && Number.isFinite(v);
/** v through f, or "unknown" when it is not a number: a missing figure is never guessed. */
const fig = (v, f) => (isNum(v) ? f(v) : UNKNOWN);
/** "84,498" (at most `digits` decimals), as the monitor groups thousands. */
const grouped = (digits) => (v) => v.toLocaleString("en-US", { maximumFractionDigits: digits });

/**
 * "Market recorded at 21 Oct 14:30 UTC: spot 84,498, DVOL 37.2, IV percentile
 * 15 %, regime POSITIVE/PINNING." — the market a fill or a close's journal
 * line holds (/api/journal item.market, #427), as words; "" without one.
 */
function recordedMarket(e) {
  const m = e.market;
  if (!m) return "";
  const regime = m.gex_regime || UNKNOWN;
  return (
    `Market recorded at ${shortTime(e.at)} UTC: spot ${fig(m.spot, grouped(0))}, DVOL ${fig(m.dvol, grouped(1))}, ` +
    `IV percentile ${fig(m.iv_percentile, grouped(1))} %, regime ${regime}.`
  );
}

/**
 * A bot's journal items (/api/journal, oldest first) as citable passages: id
 * "journal:<bot>/<seq>" (the last occurrence's seq), the plain-words sentence
 * and its time — the facts the journal recorded, nothing recomputed. A
 * collapsed item reads "×42, 20 Oct 08:00 → 21 Oct 14:30: <plain>"; a fill or
 * a close adds the market its line recorded (#427). `trade` marks an open,
 * close or fill; `reason` is the rule word, for the search; `held` marks an
 * event of an instrument the bot holds now (`held`, the answer's list, #427).
 */
export function journalPassages(bot, items, held = []) {
  const holds = new Set(held ?? []);
  return (items ?? [])
    .filter((e) => e && typeof e.plain === "string" && e.plain && e.seq != null)
    .map((e) => {
      const said = e.count > 1 ? `×${e.count}, ${shortTime(e.first)} → ${shortTime(e.last)}: ${e.plain}` : e.plain;
      const market = recordedMarket(e);
      const instrument = typeof e.instrument === "string" ? e.instrument : "";
      // The instrument by name (#534): an answer naming it is grounded in the passage it cites.
      const text = [said, instrument ? `Instrument ${instrument}.` : "", market].filter(Boolean).join(" ");
      return {
        id: `journal:${bot}/${e.seq}`,
        source: `journal:${bot}`,
        title: `${bot} journal: ${e.event} at ${e.at}`,
        text,
        trade: TRADE_EVENTS.has(e.event),
        reason: typeof e.reason === "string" ? e.reason : "",
        event: e.event,
        slot: e.slot ?? null,
        lastAt: e.last ?? e.at,
        instrument,
        held: instrument !== "" && holds.has(instrument),
        seq: Number(e.seq),
        atMs: Date.parse(e.at),
        firstMs: Date.parse(e.first ?? e.at),
      };
    });
}

// The bot's live page data (#427): its open legs and the market now, read
// for a question (GET /api/status, /api/positions, /api/strangles/payoff),
// kept LIVE_CACHE_MS per bot; a full book (8 legs + the market) stays under
// LIVE_TOKENS.
export const LIVE_CACHE_MS = 10_000;
export const LIVE_TOKENS = 1500;

/** "27 Nov 2026" (UTC), or "unknown". */
function day(iso) {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime()) || d.getUTCFullYear() < 2000) return UNKNOWN;
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]} ${d.getUTCFullYear()}`;
}

/** The market now as words, from /api/status (its own fields; nothing recomputed). */
function marketPassage(bot, status) {
  const m = status.market ?? {};
  const risk = status.risk ?? {};
  const policy = risk.status;
  let confirmed = "confirmation unknown";
  if (policy) {
    if (!policy.regime_known) confirmed = "no regime confirmed yet";
    else confirmed = policy.regime_negative ? "confirmed regime negative" : "confirmed regime not negative";
  }
  const trend = status.trend ? `${status.trend}${status.direction ? ` (from ${status.direction})` : ""}` : UNKNOWN;
  const band = fig(status.flip_buffer_pct, (v) => (v > 0 ? `±${grouped(2)(v)} % of spot` : UNKNOWN));
  const frozen = policy?.frozen ? ` New risk is frozen: ${policy.freeze_reason || UNKNOWN}.` : "";
  return {
    id: `live:${bot}/market`,
    source: `live:${bot}`,
    title: `${bot} market now${status.as_of ? ` at ${status.as_of}` : ""}`,
    text:
      `Market now (${status.as_of ? `${shortTime(status.as_of)} UTC` : "time unknown"}): spot ${fig(m.spot, grouped(0))}, ` +
      `DVOL ${fig(m.dvol, grouped(1))}, IV percentile ${fig(m.iv_percentile, grouped(1))} %, ` +
      `gamma regime ${m.gex_regime || UNKNOWN} (${confirmed}), trend ${trend}, gamma flip ${fig(m.gamma_flip, grouped(0))}, ` +
      `shed band ${band} around the flip. IM ${fig(risk.im_pct, grouped(1))} % of margin balance, ` +
      `limit ${fig(policy?.limit_im_pct, grouped(1))} %.${frozen}`,
    live: true,
  };
}

/** One open leg as words, from /api/positions, its payoff chart and the held legs. */
function legPassage(bot, strangle, leg, chart, holds, unit) {
  const drawn = (chart?.legs ?? []).find((l) => l.position_id === leg.position_id);
  const type = leg.option_type || UNKNOWN;
  const side = leg.side === "sell" ? "short" : leg.side === "buy" ? "long" : UNKNOWN;
  const slot = strangle.slot ? `${strangle.slot.dte}-day ${strangle.slot.delta} delta` : UNKNOWN;
  const pnl = fig(leg.unrealised_pnl, (v) => formatCoin(v, unit, 5));
  const roi = fig(leg.roi_pct, (v) => formatPct(v, 0, { signed: true }));
  const otm = isNum(leg.distance_to_strike_pct) ? `${leg.moneyness || "distance"} ${leg.distance_to_strike_pct.toFixed(1)}%` : `distance to strike ${UNKNOWN}`;
  const missing = holds.map((h) => ` Its ${h.missing} is not held: ${h.reason || UNKNOWN}.`).join("");
  return {
    id: `live:${bot}/leg/${leg.instrument}`,
    source: `live:${bot}`,
    title: `${bot} open leg ${leg.instrument}`,
    text:
      `${leg.instrument}: ${side} ${type}, strike ${fig(leg.strike, grouped(0))}, slot ${slot}, expiry ${day(leg.expiry)}, ` +
      `${fig(leg.dte, (v) => v.toFixed(1))} days left. Qty ${fig(leg.qty, String)}, entry price ${fig(leg.entry_price, formatPrice)}, ` +
      `mark ${fig(leg.mark, formatPrice)}, P&L ${pnl}, ${roi} of its premium. ` +
      `Delta ${fig(leg.greeks?.delta, (v) => v.toFixed(2))}, ${otm}. ` +
      `Stop-loss at mark ${fig(leg.stop_loss_mark, formatPrice)} (spot ${fig(drawn?.stop_level, grouped(0))} on the model); ` +
      `delta-exit line at spot ${fig(drawn?.exit_line, grouped(0))}.${missing}`,
    live: true,
  };
}

/**
 * The bot's live page data as citable passages (#427): "live:<bot>/market"
 * (spot, DVOL, IV percentile, gamma regime and its confirmation, trend, flip
 * and shed band, IM against its limit) and one "live:<bot>/leg/<instrument>"
 * per open leg (side, strike, slot, expiry and days left, qty, entry price,
 * mark, P&L in coin and % of premium, delta, OTM %, the stop and the
 * delta-exit line, the missing leg's hold). Plain text from the API's own
 * fields, as the Positions page shows them; a missing figure reads "unknown".
 * status, positions, payoff: the routes' JSON (null when not read). [] when
 * neither status nor positions was read.
 */
export function livePassages(bot, { status = null, positions = null, payoff = null } = {}) {
  if (!status && !positions) return [];
  const out = status ? [marketPassage(bot, status)] : [];
  const unit = status?.underlying ?? positions?.underlying ?? "";
  const charts = new Map((payoff?.strangles ?? []).map((s) => [s.strangle_id, s]));
  for (const st of positions?.strangles ?? []) {
    const chart = charts.get(st.id);
    let holds = (status?.held_legs ?? []).filter((h) => h.strangle_id === st.id);
    if (!status) holds = (chart?.missing ?? []).map((m) => ({ missing: m.option_type, reason: m.reason }));
    for (const leg of st.legs ?? []) {
      if (leg?.instrument) out.push(legPassage(bot, st, leg, chart, holds, unit));
    }
  }
  return out;
}

// A question about the present book: why nothing is open, why the margin is
// not used, why no more trades (#719). Only these pay for the book passage.
const IDLE_PATTERNS = [
  /\bnothing\s+(is\s+)?(open|opened|held|happening|sold|trading)\b/i,
  /\b(no|zero)\s+(open\s+)?(trades?|positions?|strangles?)\b/i,
  /(\bnot|n't)\s+(being\s+)?(open(ing)?|trad(e|ing)|sell(ing)?|enter(ing)?|doing\s+anything|used|using)\b/i,
  /\bmargin\b.*\b(unused|idle|not\s+(being\s+)?used|so\s+low|spare)\b/i,
  /\b(unused|idle|spare)\s+(margin|slots?)\b/i,
  /\b(empty|idle)\s+(bot|book|slots?)\b/i,
  /\bmore\s+(trades|strangles|positions)\b/i,
  /\bvacant\b/i,
  /\bwhy\b.*\b(waiting|frozen|paused|stuck)\b/i,
];

/** Whether a question asks why the bot holds little or nothing now (#719). */
export function asksAboutIdle(question) {
  const q = String(question ?? "");
  return IDLE_PATTERNS.some((re) => re.test(q));
}

const slotKey = (s) => `${s.dte}/${s.delta}`;
const slotWords = (s) => `${s.dte}-day ${s.delta} delta`;

/** Whether entries may open now, and why not, in the monitor's words. */
function entriesLine(status) {
  if (status.halted) return "Entries: stopped: the kill switch fired, so the bot opens nothing until a person restarts it.";
  // The GEX age on the bot's own clock (its as_of), as the Regime page reads it.
  const asOf = Date.parse(status.as_of ?? "");
  if (!Number.isNaN(asOf)) {
    const gex = gexFreshness(status, asOf);
    if (gex.ageSec == null) return "Entries: waiting for the first fresh GEX reading: the bot sells nothing before it knows the market's gamma regime and trend.";
    if (gex.stale) return `Entries: waiting for a fresh GEX reading (the last one was read ${formatAge(gex.ageSec)}, more than 5 minutes): the bot sells nothing on an old reading.`;
  }
  const risk = status.risk ?? {};
  const policy = risk.status;
  if (policy?.frozen) {
    const why = plainFreeze(policy.freeze_reason).replace(/^waiting: /, "");
    return `Entries: frozen by the margin policy: ${why}${policy.freeze_reason ? ` (${policy.freeze_reason})` : ""}. Exits, rolls and repairs continue.`;
  }
  if (risk.error) return `Entries: blocked: the margin figures are unknown this cycle (${risk.error}), and the bot adds no risk without them.`;
  if (risk.entries_allowed === true) return "Entries: allowed.";
  if (risk.entries_allowed === false) return "Entries: held this cycle; each vacant slot's latest reason is below.";
  return "Entries: unknown: the status does not say.";
}

/** IM against its limit, its alerts and its 7-day use, as percentages only. */
function marginLine(status) {
  const risk = status.risk ?? {};
  if (risk.error) return `IM: unknown: ${risk.error}.`;
  const parts = [`IM ${fig(risk.im_pct, grouped(1))} % of margin balance, limit ${fig(risk.status?.limit_im_pct, grouped(1))} %.`];
  if (risk.im_over_limit) parts.push("IM is over its limit.");
  if (risk.im_under_use) parts.push("IM has stayed well under its limit while entries were allowed (the under-use alert).");
  const u = status.utilisation_7d;
  if (u && isNum(u.mean_pct) && isNum(u.allowed_share)) {
    parts.push(`Over the last 7 days IM averaged ${grouped(0)(u.mean_pct)} % of its limit while entries were allowed, which they were ${grouped(0)(u.allowed_share * 100)} % of the time.`);
  }
  return parts.join(" ");
}

/** The latest skip the journal read holds for each slot: slot key → passage. */
function latestSkips(journal) {
  const out = new Map();
  for (const p of journal ?? []) {
    if (p.event !== "skipped" || !p.slot) continue;
    const k = slotKey(p.slot);
    if (!out.has(k) || out.get(k).seq < p.seq) out.set(k, p);
  }
  return out;
}

/** The slots held against the matrix, and each vacant slot's latest skip. */
function slotLines(status, positions, journal) {
  const slots = (status.pnl ?? []).filter((r) => r?.slot && !r.retired).map((r) => r.slot);
  const working = isNum(status.pending) && status.pending > 0 ?` ${status.pending} entry or repair order${status.pending === 1 ? "" : "s"} working.` : "";
  if (!slots.length) return [`Slots: unknown: the status lists no slot.${working}`];
  if (!positions) {
    return [`Slots: ${fig(status.open_strangles, String)} open strangles for ${slots.length} slots; which slots are vacant is unknown: the positions route did not answer.${working}`];
  }
  const held = new Set((positions.strangles ?? []).filter((s) => s.slot).map((s) => slotKey(s.slot)));
  const vacant = slots.filter((s) => !held.has(slotKey(s)));
  if (!vacant.length) return [`Slots: all ${slots.length} hold an open strangle, so the bot opens a new one only when one of them frees.${working}`];
  const skips = latestSkips(journal);
  return [
    `Slots: ${slots.length - vacant.length} of ${slots.length} hold an open strangle, ${vacant.length} vacant.${working}`,
    ...vacant.map((s) => {
      const p = skips.get(slotKey(s));
      if (!p) return `Vacant slot ${slotWords(s)}: no skip for it in the journal of the last ${JOURNAL_DAYS} days.`;
      return `Vacant slot ${slotWords(s)}: latest skip (${shortTime(p.lastAt)} UTC): ${p.text.replace(/\.$/, "")}.`;
    }),
  ];
}

/** Each one-legged strangle's missing leg and what holds its repair. */
function heldLines(status) {
  return (status.held_legs ?? [])
    .filter((h) => h?.rule)
    .map((h) => `One-legged strangle in slot ${h.slot ? slotWords(h.slot) : UNKNOWN}: its ${h.missing || UNKNOWN} is not sold again yet: ${h.reason || UNKNOWN}.`);
}

/**
 * Why the bot holds what it holds now (#719), as the citable passage
 * "live:<bot>/book": whether entries may open and why not (halted, waiting
 * for a fresh GEX reading, frozen by the margin policy in the monitor's
 * words, margin unknown), IM against its limit with its alerts and 7-day use,
 * the open strangles against the slots, each vacant slot's latest skip from
 * the journal read (its plain words) and each held repair. Percentages and
 * counts only: no amount, no account detail. status, positions: the routes'
 * JSON; journal: journalPassages; error: why status could not be read, which
 * makes the whole passage "unknown: <error>".
 */
export function bookPassage(bot, { status = null, positions = null, journal = [], error = "" } = {}) {
  const base = { id: `live:${bot}/book`, source: `live:${bot}`, title: `${bot} book now`, live: true };
  if (!status) return { ...base, text: `Book now: unknown: ${error || "the bot's status could not be read"}. Why nothing is open, or why the margin is unused, cannot be said now.` };
  const lines = [
    `Book now (${status.as_of ? `${shortTime(status.as_of)} UTC` : "time unknown"}):`,
    entriesLine(status),
    marginLine(status),
    ...slotLines(status, positions, journal),
    ...heldLines(status),
  ];
  return { ...base, title: `${base.title}${status.as_of ? ` at ${status.as_of}` : ""}`, text: lines.join(" ") };
}

/** "BTC-27NOV26-75000-P" → { name, strike: 75000, type: "put" } (XRP's "2d5" strike = 2.5). */
function legOf(name) {
  const parts = name.split("-");
  const strike = Number((parts[2] ?? "").replace("d", "."));
  return { name, strike, type: parts[3] === "P" ? "put" : parts[3] === "C" ? "call" : "" };
}

/** The numbers a question writes: "75.000" and "75,000" = 75000, "75k" = 75000. */
function questionNumbers(question) {
  const out = new Set();
  for (const m of question.matchAll(/(\d{1,3}(?:[.,]\d{3})+|\d+(?:\.\d+)?)(\s*k\b)?/gi)) {
    let v = /^\d{1,3}(?:[.,]\d{3})+$/.test(m[1]) ? Number(m[1].replace(/[.,]/g, "")) : Number(m[1]);
    if (m[2]) v *= 1000;
    out.add(v);
  }
  return out;
}

/**
 * The held instruments a question names (#427): by name; else by strike and
 * side ("the put at 75.000"), each only when it matches a held leg; else
 * every one of them.
 */
export function namedInstruments(question, instruments) {
  const q = question.toUpperCase();
  const direct = instruments.filter((i) => q.includes(i.toUpperCase()));
  if (direct.length) return direct;
  let legs = instruments.map(legOf);
  const nums = questionNumbers(question);
  const byStrike = legs.filter((l) => nums.has(l.strike));
  if (byStrike.length) legs = byStrike;
  const puts = /\bputs?\b/i.test(question);
  if (puts !== /\bcalls?\b/i.test(question)) {
    const side = legs.filter((l) => l.type === (puts ? "put" : "call"));
    if (side.length) legs = side;
  }
  return legs.map((l) => l.name);
}

/** About how many tokens a passage costs in the prompt (≈ 4 characters a token). */
export function passageTokens(p) {
  return Math.ceil((p.id.length + p.title.length + p.text.length + 4) / 4);
}

/**
 * The journal passages one question sends (#417): every open, close and fill
 * (newest first if they alone fill the budget), then the other facts the
 * question matches best — the help's BM25 over each one's sentence and rule
 * word — and the decisions within 30 min of a trade sent, while the budget
 * lasts. The history of the held legs the question names (namedInstruments:
 * by instrument, strike or side, else every held leg; #427) goes before the
 * rest: their trades oldest first (the entry first, however old), then the
 * week's other trades, then those legs' other events newest first. A pinned
 * passage (the facts of the trade a "Why?" asks about, explainPassages)
 * always goes first, outside the budget. Oldest first, as the journal reads.
 */
export function selectJournal(question, passages, { maxPassages = JOURNAL_PASSAGES, maxTokens = JOURNAL_TOKENS } = {}) {
  const picked = [];
  const chosen = new Set();
  let tokens = 0;
  const take = (p) => {
    if (chosen.has(p)) return true;
    const cost = passageTokens(p);
    if (picked.length >= maxPassages || tokens + cost > maxTokens) return false;
    picked.push(p);
    chosen.add(p);
    tokens += cost;
    return true;
  };
  const pinned = (passages ?? []).filter((x) => x.pinned);
  for (const p of pinned) {
    picked.push(p);
    chosen.add(p);
    tokens += passageTokens(p);
  }
  const all = (passages ?? []).filter((x) => !x.pinned);
  const held = [...new Set(all.filter((x) => x.held).map((x) => x.instrument))];
  const named = new Set(namedInstruments(question, held));
  const ofNamed = all.filter((x) => x.held && named.has(x.instrument));
  const newest = (a, b) => b.seq - a.seq;
  for (const p of ofNamed.filter((x) => x.trade).sort((a, b) => a.seq - b.seq)) {
    if (!take(p)) break;
  }
  for (const p of all.filter((x) => x.trade).sort(newest)) {
    if (!take(p)) break;
  }
  for (const p of ofNamed.filter((x) => !x.trade).sort(newest)) {
    if (!take(p)) break;
  }
  const others = all.filter((x) => !x.trade && !chosen.has(x));
  if (others.length > 0) {
    const byId = new Map(others.map((p) => [p.id, p]));
    const index = new HelpIndex(others.map((p) => ({ ...p, text: `${p.text} ${(p.reason ?? "").replaceAll("_", " ")}` })));
    for (const hit of index.search(question, others.length)) {
      if (!take(byId.get(hit.passage.id))) break;
    }
    // Then the decisions around each trade taken (±30 min, as /api/explain's related).
    const tradeTimes = picked.filter((p) => p.trade).map((p) => p.atMs);
    for (const p of others) {
      if (chosen.has(p) || !tradeTimes.some((t) => p.firstMs - AROUND_MS <= t && t <= p.atMs + AROUND_MS)) continue;
      if (!take(p)) break;
    }
  }
  return picked.sort((a, b) => a.seq - b.seq);
}

/** "spot 67,000, DVOL 52.1, regime negative" — the market an explanation recorded, as words. */
function marketWords(m) {
  if (!m) return "";
  const parts = [
    ["spot", m.spot],
    ["DVOL", m.dvol],
    ["IV percentile", m.iv_percentile],
    ["GEX regime", m.gex_regime],
    ["gamma flip", m.gamma_flip],
    ["flip buffer %", m.flip_buffer_pct],
    ["trend", m.trend],
    ["delta", m.delta],
    ["bid", m.bid],
    ["ask", m.ask],
    ["mid", m.mid],
  ].filter(([, v]) => v !== undefined && v !== null && v !== "" && v !== 0);
  return parts.map(([k, v]) => `${k} ${v}`).join(", ");
}

/** "2026-10-29 20:47:54" (UTC, to the second) for an ISO time; "" when it is not one. */
function utcSecond(iso) {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "";
  const s = d.toISOString();
  return `${s.slice(0, 10)} ${s.slice(11, 19)}`;
}

/**
 * When an event happened, as the monitor shows it ("2026-10-29 17:47:54 BRT",
 * formatDateTime — the time a Why? question writes, whyQuestion) and in UTC
 * to the second; "" without a time.
 */
function eventTime(iso) {
  const utc = utcSecond(iso);
  return utc ? `Time: ${formatDateTime(iso)} (${utc} UTC).` : "";
}

/**
 * The facts of one journal event (/api/explain, #314) as citable passages:
 * the event itself — its plain sentence, its instrument and time (#534: the
 * facts the Why? question writes, so an answer repeating them is grounded in
 * the passage it cites), rule, detail and recorded market — then its related
 * events, each with the same "journal:<bot>/<seq>" id as journalPassages, all
 * pinned. [] for no explanation.
 */
export function explainPassages(bot, explanation) {
  const e = explanation?.event;
  if (!e || e.seq == null) return [];
  const market = marketWords(explanation.market);
  const text = [
    e.plain,
    typeof e.instrument === "string" && e.instrument ? `Instrument ${e.instrument}.` : "",
    eventTime(e.at),
    explanation.rule ? `Rule: ${explanation.rule}.` : "",
    explanation.detail ? `Detail: ${explanation.detail}.` : "",
    market ? `Market then: ${market}.` : "",
  ]
    .filter(Boolean)
    .join(" ");
  const own = {
    id: `journal:${bot}/${e.seq}`,
    source: `journal:${bot}`,
    title: `${bot} journal: ${e.event} at ${e.at}`,
    text,
    trade: TRADE_EVENTS.has(e.event),
    reason: typeof explanation.rule === "string" ? explanation.rule : "",
    seq: Number(e.seq),
    atMs: Date.parse(e.at),
    firstMs: Date.parse(e.at),
  };
  // Pinned (#417): selectJournal always sends the facts of the trade asked about.
  return [own, ...journalPassages(bot, explanation.related)].map((p) => ({ ...p, pinned: true }));
}

// P&L history points sent with a report about a bot (#409).
export const PNL_POINTS = 24;

const sig = (v) => (Number.isFinite(v) ? Number(v.toPrecision(6)) : null);

/**
 * A bot's P&L history (/api/pnl/history points, oldest first) as citable
 * passages for the request check (#409): id "pnl:<bot>/<time>", the
 * recorded totals in words. At most `max`: the two points around the
 * largest step of the total (a spike), then points evenly spaced, oldest first.
 */
export function pnlPassages(bot, points, max = PNL_POINTS) {
  const list = (points ?? []).filter((p) => p && typeof p.t === "string" && Number.isFinite(p.total));
  if (!list.length) return [];
  const keep = new Set();
  let jump = 0;
  let size = -1;
  for (let i = 1; i < list.length; i++) {
    const d = Math.abs(list[i].total - list[i - 1].total);
    if (d > size) [jump, size] = [i, d];
  }
  if (jump > 0) keep.add(jump - 1).add(jump);
  const step = Math.max(1, Math.ceil(list.length / Math.max(1, max - keep.size)));
  for (let i = list.length - 1; i >= 0 && keep.size < max; i -= step) keep.add(i);
  return [...keep]
    .sort((a, b) => a - b)
    .map((i) => {
      const p = list[i];
      const parts = [`total ${sig(p.total)}`, `realised ${sig(p.realised)}`, `unrealised ${sig(p.unrealised)}`];
      if (Number.isFinite(p.total_usd)) parts.push(`total USD ${Math.round(p.total_usd * 100) / 100}`);
      if (Number.isFinite(p.spot)) parts.push(`spot ${sig(p.spot)}`);
      const at = `${p.t.slice(0, 10)} ${p.t.slice(11, 16)} UTC`;
      return { id: `pnl:${bot}/${p.t}`, source: `pnl:${bot}`, title: `${bot} P&L history at ${p.t}`, text: `P&L recorded on ${at}: ${parts.join(", ")}.` };
    });
}

// The bot's performance over the last week and month (#648): its
// /api/performance?range=7d and ?range=30d, read together for a question
// about results only (asksAboutResults), kept PERF_CACHE_MS per bot; both
// passages stay under PERF_TOKENS.
export const PERF_RANGES = ["7d", "30d"];
export const PERF_CACHE_MS = 10_000;
export const PERF_TOKENS = 700;

// A question about the bot's results over a period: only these pay for the
// performance passages. "next week/month" is not one (a forecast is refused).
const RESULT_PATTERNS = [
  /\b(this|last|past|previous|the)\s+(week|month)\b/i,
  /\b(7|30|seven|thirty)[ -]days?\b/i,
  /\bworst\s+week\b/i,
  /\bdrawdowns?\b/i,
  /\b(vs\.?|versus|against|than|beat|beating|beaten)\s+(just\s+|simply\s+)?hold(ing)?\b/i,
  /\bhold(ing)?\s+(the|my)\s+coin\b/i,
  /\b(net\s+of|after)\s+fees\b/i,
  /\b(my|the\s+bot'?s|its|our)\s+(results?|returns?|performance|p&l|pnl|profits?|loss(es)?)\b/i,
  /\bhow\s+(did|have|has|am)\s+(i|we|it|the\s+bot)\s+(do|done|doing)\b/i,
  /\b(am|are|is)\s+(i|we|it|the\s+bot)\s+(up|down)\b/i,
];

/** Whether a question asks about the bot's results over a period (#648). */
export function asksAboutResults(question) {
  const q = String(question ?? "");
  return RESULT_PATTERNS.some((re) => re.test(q));
}

const PERF_WINDOWS = { "7d": "Last 7 days", "30d": "Last 30 days" };

/** "−1.3%", "+2.0%": a % as the monitor signs it; "unknown" when it is not a number. */
const signedPct = (v) => fig(v, (x) => formatSignedPct(x));

/** Net P&L after fees, in coin and in USD at the window's last spot. */
function perfNet(p, unit) {
  if (!p.net_pnl_known) return "Net P&L after fees: unknown: no P&L history point in the window.";
  const usd = isNum(p.net_pnl_usd) ? `${formatSignedUSD(p.net_pnl_usd)} at the window's last spot` : "USD unknown: no spot recorded in the window";
  return `Net P&L after fees: ${formatCoin(p.net_pnl, unit, 5)} (${usd}).`;
}

/** Return on equity, or why it is unknown (the route's equity_reason). */
function perfReturn(p, unit) {
  if (!p.equity_known || !isNum(p.return_pct)) return `Return on equity: unknown: ${p.equity_reason || "no equity recorded"}.`;
  return `Return on equity: ${signedPct(p.return_pct)} (equity ${fig(p.equity, (v) => v.toFixed(5))} ${unit} at the start).`;
}

/** The return of holding the coin over the same window, and the bot's against it. */
function perfHold(p, unit) {
  if (!p.hold_known) return "Holding the coin over the same window: unknown: no spot recorded at the window's start and end.";
  const bot = isNum(p.return_usd_pct)
    ? ` The bot's return in USD: ${signedPct(p.return_usd_pct)}, ${fig(p.excess_return_pct, (v) => formatSignedPct(v).replace("%", " percentage points"))} against holding.`
    : ` The bot's return in USD: unknown: ${p.equity_reason || "no equity recorded"}.`;
  return `Holding the coin over the same window: ${signedPct(p.hold_return_usd_pct)} in USD, ${formatPct(p.hold_return_coin_pct ?? 0)} in ${unit} (a held coin stays one coin).${bot}`;
}

/** Max drawdown and the worst week, each with its % of equity or why that is unknown. */
function perfRisk(p, unit) {
  const ofEquity = (pct, note) => (isNum(pct) ? `${signedPct(pct)} of equity` : `% of equity unknown: ${note || p.equity_reason || "no equity recorded"}`);
  const dd = p.max_drawdown ?? {};
  const drawdown = p.net_pnl_known ? `Max drawdown: ${fig(dd.coin, (v) => formatCoin(v, unit, 5))} (${ofEquity(dd.pct)}).` : "Max drawdown: unknown: no P&L history point in the window.";
  const ww = p.worst_week ?? {};
  const week = ww.known
    ? `Worst week: ${day(ww.start)} to ${day(ww.end)}, ${fig(ww.coin, (v) => formatCoin(v, unit, 5))} (${ofEquity(ww.pct, ww.pct_note)}).`
    : "Worst week: unknown: the window does not hold 7 days of P&L history yet.";
  return `${drawdown} ${week}`;
}

/** The days of the window with no P&L history, named, never filled in. */
function perfMissing(p) {
  const missing = p.missing_days ?? [];
  const of = isNum(p.days) ? ` of ${p.days}` : "";
  if (!missing.length) return `Days with no P&L history: none${of}.`;
  return `Days with no P&L history: ${missing.length}${of} (${missing.join(", ")}), not filled in.`;
}

/** One window's /api/performance answer as a citable passage. */
function perfPassage(bot, range, p, unit) {
  const label = p.label === "live" ? "live" : "testnet";
  const text = [
    `${PERF_WINDOWS[range]} (${day(p.from)} to ${day(p.to)}), ${label} figures, past results, not a forecast.`,
    perfNet(p, unit),
    perfReturn(p, unit),
    perfHold(p, unit),
    perfRisk(p, unit),
    perfMissing(p),
  ].join(" ");
  return { id: `perf:${bot}/${range}`, source: `perf:${bot}`, title: `${bot} performance, ${PERF_WINDOWS[range].toLowerCase()}`, text };
}

/**
 * The bot's performance over the last 7 and 30 days (/api/performance,
 * #232) as citable passages (#648): "perf:<bot>/7d" and "perf:<bot>/30d",
 * each the window's dates, its label (live or testnet), net P&L after fees in
 * coin and USD, return on equity, the return of holding the coin and the
 * bot's against it, max drawdown, the worst week with its dates and the days
 * with no history. The route's own figures, signed and with their unit as
 * the monitor shows them, nothing recomputed; an unknown figure reads
 * "unknown" with the route's reason. A window not read (null) is left out.
 * unit: the coin, the bot's name in capitals unless given.
 */
export function perfPassages(bot, perf7, perf30, unit = String(bot ?? "").toUpperCase()) {
  return [
    ["7d", perf7],
    ["30d", perf30],
  ]
    .filter(([, p]) => p && typeof p === "object")
    .map(([range, p]) => perfPassage(bot, range, p, unit));
}

/** The passages of both lists, each id once: the first list's copy wins. */
export function mergePassages(first, second) {
  const ids = new Set(first.map((p) => p.id));
  return [...first, ...second.filter((p) => !ids.has(p.id))];
}

function outcomeOf(reply, verdict) {
  if (verdict.ok) {
    const t = reply.text.trim().split(/\s+/).join(" ");
    if (t === DONT_KNOW) return [OUTCOME_DONT_KNOW, DONT_KNOW];
    if (t === REFUSAL) return [OUTCOME_REFUSED, REFUSAL];
    // No fact answered, the advice part declined (#427).
    if (t === `${DONT_KNOW} ${REFUSAL}`) return [OUTCOME_DONT_KNOW, t];
    if (isCanonical(t)) return [OUTCOME_REJECTED, FALLBACK_REPLY];
    // Cited facts, with the advice or forecast part declined by the refusal or
    // not (#427): an answer — the guard has checked its citations.
    return [OUTCOME_ANSWERED, reply.text.trim()];
  }
  if (verdict.reasons.includes(REASON_NO_CITATION)) return [OUTCOME_NO_CITATION, DONT_KNOW];
  return [OUTCOME_REJECTED, FALLBACK_REPLY];
}

// The rejections worth one more try (#534): a slip the model can fix from the
// same sources, or an answer not in English (#565), which the advice and
// forecast checks cannot read. Advice and forecasts are never retried, nor an
// answer citing nothing.
const RETRIED = [REASON_UNGROUNDED_NUMBER, REASON_UNKNOWN_CITATION, REASON_POINTS_TO_DOCUMENT, REASON_LANGUAGE];

/** The findings of one reason in a verdict's details ("ungrounded_number: 27" → "27"). */
const findings = (verdict, reason) => verdict.details.filter((d) => d.startsWith(`${reason}: `)).map((d) => d.slice(reason.length + 2));
const isAre = (list) => (list.length > 1 ? "aren't" : "isn't");

/**
 * What to tell the model when its answer is asked once more (#534), or null
 * when the verdict is not retried: passed, or any reason outside RETRIED.
 */
export function retryFeedback(verdict) {
  if (verdict.ok || verdict.reasons.some((r) => !RETRIED.includes(r))) return null;
  const said = [];
  const numbers = findings(verdict, REASON_UNGROUNDED_NUMBER);
  if (numbers.length) said.push(`${numbers.join(", ")} ${isAre(numbers)} in the sources you cited: cite the source that has them, or leave them out.`);
  const unknown = findings(verdict, REASON_UNKNOWN_CITATION);
  if (unknown.length) said.push(`${unknown.join(", ")} ${isAre(unknown)} among the listed sources: cite only the ids in square brackets.`);
  const pointers = findings(verdict, REASON_POINTS_TO_DOCUMENT);
  if (pointers.length) said.push(`Don't send the person to a document, guide or section ("${pointers.join('", "')}"): say what the source says, in your own words.`);
  if (verdict.reasons.includes(REASON_LANGUAGE)) said.push("Your answer was not in English: answer in English, whatever language the question is in.");
  return said.join(" ");
}

/** A model reply with its citations resolved, and the guard's verdict on it. */
function judged(reply, passages) {
  const r = { ...reply, citations: reply.citations.map((c) => resolveCitation(c, passages)) };
  return { reply: r, verdict: check(r, passages) };
}

/**
 * A citation as the passage id it can only mean: listed as is (an id or a
 * source), else the one listed id — or, failing that, the one source — that
 * ends with it after a ":": a model may drop the id's first part
 * ("closed:delta_exit/1" for "explain:closed:delta_exit/1", #412). Anything else stays, for the guard
 * to reject.
 */
export function resolveCitation(c, passages) {
  if (passages.some((p) => p.id === c || p.source === c)) return c;
  const ids = passages.filter((p) => p.id.endsWith(`:${c}`)).map((p) => p.id);
  if (ids.length === 1) return ids[0];
  const sources = [...new Set(passages.filter((p) => p.source?.endsWith(`:${c}`)).map((p) => p.source))];
  return ids.length === 0 && sources.length === 1 ? sources[0] : c;
}

/**
 * Fills the log entry from a request check's verdict (#409): resolves its
 * citations, then request.js decides the outcome. Not a report → the verdict's
 * answer as any other; answered → its answer through the guard, with the
 * checklist (and an enhancement draft when one may be offered); needs_info →
 * the checklist; draft → the card, citing its evidence. The draft is logged
 * with the answer (every AI output is reviewable), never queued here: only the
 * person's Send (POST /api/assistant/requests) puts it in the private queue.
 */
function applyRequestCheck(entry, verdict, passages) {
  const resolve = (c) => resolveCitation(c, passages);
  const v = {
    ...verdict,
    citations: verdict.citations.map(resolve),
    explained_by: verdict.explained_by.map(resolve),
    evidence: verdict.evidence.map((e) => ({ ...e, citation: e.citation ? resolve(e.citation) : "" })),
  };
  const logged = { ...v };
  delete logged.stop;
  entry.model_answer = JSON.stringify(logged);
  const r = checkRequest(v, passages);

  if (r.outcome === REQUEST_NONE || r.outcome === REQUEST_ANSWERED) {
    const cites = [...new Set([...v.citations, ...r.explained_by])];
    const g = check({ text: v.text, citations: cites }, passages);
    entry.citations = cites;
    entry.verdict = [...g.details, ...r.details];
    [entry.outcome, entry.answer] = outcomeOf({ text: v.text }, g);
    if (r.outcome === REQUEST_ANSWERED && entry.outcome === OUTCOME_ANSWERED) entry.request = { checklist: r.checklist, draft: r.draft };
    return;
  }
  entry.verdict = r.details;
  entry.request = { checklist: r.checklist, draft: r.draft };
  if (r.outcome === REQUEST_DRAFT) {
    entry.outcome = OUTCOME_DRAFT;
    entry.answer = DRAFT_TEXT;
    entry.citations = [...new Set(r.draft.evidence.map((e) => e.citation))];
  } else {
    entry.outcome = OUTCOME_NEEDS_INFO;
    entry.answer = NEEDS_INFO_TEXT;
  }
}

/**
 * Answers one question. model: { model, ask({question, passages}) → {text,
 * citations, report}, checkRequest?({question, passages}) → a verdict };
 * index: a HelpIndex; journal: the bot's week of passages from journalPassages
 * plus its held legs' whole history (selectJournal picks what this question
 * sends, #417, #427); live: the bot's page data from livePassages, all sent
 * (#427); perf: the bot's 7- and 30-day performance from perfPassages, sent
 * only when the question asks about results (asksAboutResults, #648); book:
 * the bot's book now from bookPassage, sent only when the question asks why
 * little or nothing is open (asksAboutIdle, #719); reportPassages:
 * async () → more passages for a report (the bot's P&L history, #409);
 * takeRetry: () → true when the person's budget allows the one more try of a
 * rejected answer (#534; retryFeedback says which are retried); file: the
 * review log, where a retried answer's first try is `first_answer`; testRun:
 * the golden-set run's questions (#536), marked `test_run` so no review counts
 * them. A question that reads as a report (looksLikeReport, or the
 * model's report flag on its answer) runs the request check instead of the
 * plain answer. → { status, body } for the route: 200 with { id, answer,
 * citations, outcome, checklist?, draft?, ai_generated, notice }, 502 when
 * the model failed, 500 when the log could not be written (nothing is shown unlogged).
 */
export async function answerQuestion({
  question,
  bot = null,
  journal = [],
  live = [],
  perf = [],
  book = [],
  reportPassages = async () => [],
  takeRetry = () => true,
  user,
  model,
  index,
  file,
  testRun = false,
  nowMs = Date.now(),
}) {
  const hits = index.search(question, HELP_PASSAGES);
  const facts = selectJournal(question, journal);
  // The performance figures go only with a question about results (#648).
  const results = asksAboutResults(question) ? perf : [];
  // The book now goes only with a question about an idle book or unused margin (#719).
  const present = asksAboutIdle(question) ? book : [];
  let passages = [...hits.map((h) => h.passage), ...live, ...present, ...results, ...facts];
  const canCheck = typeof model.checkRequest === "function";
  let report = canCheck && looksLikeReport(question);
  const entry = {
    kind: "answer",
    id: newAnswerId(),
    ts: new Date(nowMs).toISOString(),
    user: user.username,
    role: user.role,
    bot,
    question,
    sources: passages.map((p) => p.id),
    model: model.model ?? null,
    model_answer: null,
    citations: [],
    verdict: [],
    outcome: null,
    answer: null,
    flag: false,
  };
  if (testRun) entry.test_run = true;

  let reply = null;
  let verdict = null;
  // A question not in English (#565) matches the English help poorly: its
  // best hits go to the model anyway, so an advice or forecast question gets
  // the refusal, not "I don't know"; the guard checks the answer as any other.
  const closeEnough = !noAnswer(hits) || (hits.length > 0 && !isEnglish(question));
  if (!closeEnough && facts.length === 0 && live.length === 0 && present.length === 0 && results.length === 0) {
    // Nothing to answer from: no model call. A report gets the checklist
    // (nothing to check it against), any other question the contract's reply.
    entry.model = null;
    if (report) {
      entry.outcome = OUTCOME_NEEDS_INFO;
      entry.answer = NEEDS_INFO_TEXT;
      entry.request = { checklist: emptyChecklist(), draft: null };
    } else {
      entry.outcome = OUTCOME_DONT_KNOW;
      entry.answer = DONT_KNOW;
    }
  } else {
    try {
      if (!report) {
        reply = await model.ask({ question, passages });
        // The model's intent flag: a report the words did not show.
        if (reply.report && canCheck) [report, reply] = [true, null];
      }
      if (report) {
        passages = mergePassages(passages, await reportPassages());
        entry.sources = passages.map((p) => p.id);
        verdict = await model.checkRequest({ question, passages });
      }
    } catch (err) {
      entry.outcome = OUTCOME_MODEL_ERROR;
      entry.verdict = [err.message];
    }
    if (verdict) {
      applyRequestCheck(entry, verdict, passages);
    } else if (reply) {
      let { reply: shown, verdict: v } = judged(reply, passages);
      const feedback = retryFeedback(v);
      // One more try (#534), counted in the person's budget: the first answer is logged with it.
      if (feedback && takeRetry()) {
        entry.first_answer = { model_answer: shown.text, citations: shown.citations, verdict: v.details };
        try {
          ({ reply: shown, verdict: v } = judged(await model.ask({ question, passages, retry: { text: shown.text, citations: shown.citations, feedback } }), passages));
        } catch (err) {
          // The retry failed: the first answer's verdict stands (the fallback), the error logged.
          entry.retry_error = err.message;
        }
      }
      entry.model_answer = shown.text;
      entry.citations = shown.citations;
      entry.verdict = v.details;
      [entry.outcome, entry.answer] = outcomeOf(shown, v);
    }
  }

  if (!appendLine(file, entry)) {
    return { status: 500, body: { error: "the answer could not be logged, so it is not shown: see the monitor's logs" } };
  }
  if (entry.outcome === OUTCOME_MODEL_ERROR) {
    return { status: 502, body: { id: entry.id, error: "the assistant did not answer: try again later" } };
  }
  const shown = entry.outcome === OUTCOME_ANSWERED || entry.outcome === OUTCOME_DRAFT ? entry.citations : [];
  const citations = [...new Set(shown)].map((c) => {
    const p = passages.find((x) => x.id === c || x.source === c);
    return { id: c, source: p.source, title: p.title };
  });
  const body = { id: entry.id, answer: entry.answer, citations, outcome: entry.outcome, ai_generated: true, notice: AI_NOTICE };
  if (entry.request) {
    body.checklist = entry.request.checklist;
    if (entry.request.draft) body.draft = entry.request.draft;
  }
  return { status: 200, body };
}
