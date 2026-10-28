// Pure monitor logic: flatten, filter, group and summarise what the bots
// report. No React, no fetching — everything here is unit-tested.

import { formatCoin, formatNumber, formatPct, formatPrice, formatUSD, slotLabel, isNumber } from "./format";
import { strangleLabel } from "./details";

/** Data older than this (seconds) is shown as stale. */
export const STALE_AFTER_SEC = 5;

/**
 * The gamma flip and its ±1σ band, from a bot's status. σ is the bot's own
 * shed buffer (flip_buffer_pct = gamma_flip_buffer_sd × DVOL ÷ √365): in a
 * negative regime a leg is shed once spot is below the band and repaired
 * once it is back above it. Null without a flip.
 */
export function flipBand(status) {
  const m = status?.market ?? {};
  if (!isNumber(m.gamma_flip) || m.gamma_flip <= 0) return null;
  const flip = m.gamma_flip;
  const buf = isNumber(status.flip_buffer_pct) && status.flip_buffer_pct > 0 ? status.flip_buffer_pct : 0;
  const low = flip * (1 - buf / 100);
  const high = flip * (1 + buf / 100);
  let zone = null;
  if (isNumber(m.spot) && buf > 0) zone = m.spot < low ? "below" : m.spot > high ? "above" : "inside";
  return { flip, spotToFlipPct: isNumber(m.spot_to_flip_pct) ? m.spot_to_flip_pct : null, bufferPct: buf, low: buf > 0 ? low : null, high: buf > 0 ? high : null, zone };
}

/**
 * The levels behind the SMA trend, from a bot's status (`sma`), each with
 * spot's distance to it in % ((level − spot) ÷ spot × 100: positive = the
 * level is above spot). A level of 0 is unknown and left out. Null without
 * levels.
 */
export function smaLevels(status) {
  const l = status?.sma;
  if (!l) return null;
  const spot = isNumber(status.market?.spot) && status.market.spot > 0 ? status.market.spot : null;
  const level = (key, label, date) => {
    const v = l[key];
    if (!isNumber(v) || v <= 0) return null;
    return { key, label, value: v, date: date || null, distancePct: spot ? ((v - spot) / spot) * 100 : null };
  };
  const levels = [
    level("sma9", "SMA9"),
    level("sma21", "SMA21"),
    level("swing_high", "Swing high", l.swing_high_date),
    level("swing_low", "Swing low", l.swing_low_date),
  ].filter(Boolean);
  if (levels.length === 0 && !l.trend) return null;
  return { trend: l.trend || null, reason: l.reason || null, levels };
}

/**
 * The bot answers but its decision loop has not completed a cycle for 3
 * cycle intervals (at least 2 minutes): no exits or stop-losses are being
 * checked. A halted bot (kill switch) is idle on purpose and never "stalled".
 */
export function loopStalled(status, now = Date.now()) {
  if (!status?.loop_at || status.halted) return false;
  const last = Date.parse(status.loop_at);
  if (!Number.isFinite(last) || last <= 0) return false; // no cycle yet (zero time)
  const limitMs = Math.max(3 * (status.eval_interval_ms || 0), 120_000);
  return now - last > limitMs;
}

/**
 * Flattens every bot's strangles into one row per open leg.
 * bots: [{ name, status, positions }] as returned by the bot APIs.
 */
export function legRows(bots) {
  const rows = [];
  for (const bot of bots) {
    const unit = bot.status?.underlying ?? bot.name.toUpperCase();
    const spot = bot.status?.market?.spot;
    const deltaExit = bot.summary?.delta_exit_threshold ?? null; // from /api/summary; null on an older bot
    for (const st of bot.positions?.strangles ?? []) {
      for (const leg of st.legs ?? []) {
        rows.push({
          key: `${bot.name}:${leg.position_id}`,
          bot: bot.name,
          positionIds: [leg.position_id],
          manualClose: leg.manual_close ?? null,
          unit,
          strategyId: bot.positions.strategy_id ?? bot.status?.strategy_id ?? "—",
          strangleId: st.id,
          slot: slotLabel(st.slot),
          dteTarget: st.slot?.dte,
          instrument: leg.instrument,
          type: leg.option_type,
          side: leg.side ?? "sell", // older bots: the strangle book only holds shorts
          strike: leg.strike,
          dte: leg.dte,
          qty: leg.qty,
          entry: leg.entry_price,
          mark: leg.mark,
          bid: leg.bid,
          ask: leg.ask,
          markSource: leg.mark_source ?? "last_cycle",
          markAsOf: leg.mark_as_of,
          pnl: leg.unrealised_pnl,
          spot,
          pnlUsd: isNumber(spot) && isNumber(leg.unrealised_pnl) ? leg.unrealised_pnl * spot : null,
          roiPct: leg.roi_pct,
          lossMultiple: leg.loss_multiple,
          stopMark: leg.stop_loss_mark,
          moneyness: leg.moneyness || "—",
          distancePct: leg.distance_to_strike_pct,
          delta: leg.greeks?.delta,
          gamma: leg.greeks?.gamma,
          theta: leg.greeks?.theta,
          vega: leg.greeks?.vega,
          deltaExit,
        });
      }
    }
  }
  return rows;
}

/**
 * One row closes all its positions: allowed only if every one is, and flagged
 * as rebuilt if any one would be (the reason shown is that one's).
 */
export function mergeManualClose(a, b) {
  if (!a || !b) return null;
  if (!a.allowed) return a;
  if (!b.allowed) return b;
  return b.rebuilt && !a.rebuilt ? b : a;
}

/**
 * Merges legs of the same bot, slot and instrument (strike, expiry and type)
 * into one row. Partial fills and rebalance top-ups book separate positions
 * on the same strike; the table shows them as one line with their count.
 * Qty, premium and P&L add up; entry is qty-weighted; the stop shown is the
 * position that would stop first (the lowest stop mark).
 */
export function consolidateRows(rows) {
  const byKey = new Map();
  for (const r of rows) {
    const k = `${r.bot}:${r.slot}:${r.instrument}:${r.side}`;
    const prev = byKey.get(k);
    if (!prev) {
      byKey.set(k, { ...r, key: k, positions: 1, premium: premiumOf(r), positionIds: [...(r.positionIds ?? [])] });
      continue;
    }
    const qty = prev.qty + r.qty;
    const premium = prev.premium + premiumOf(r);
    const pnl = addNullable(prev.pnl, r.pnl);
    byKey.set(k, {
      ...prev,
      positions: prev.positions + 1,
      positionIds: [...prev.positionIds, ...(r.positionIds ?? [])],
      manualClose: mergeManualClose(prev.manualClose, r.manualClose),
      qty: round(qty),
      premium,
      entry: qty > 0 ? premium / qty : prev.entry,
      pnl,
      pnlUsd: addNullable(prev.pnlUsd, r.pnlUsd),
      roiPct: isNumber(pnl) && premium > 0 ? (pnl / premium) * 100 : null,
      lossMultiple: isNumber(pnl) && premium > 0 ? -pnl / premium : null,
      stopMark: minNullable(prev.stopMark, r.stopMark),
      markSource: prev.markSource === "live" && r.markSource === "live" ? "live" : r.markSource,
    });
  }
  return [...byKey.values()];
}

const premiumOf = (r) => (isNumber(r.entry) && isNumber(r.qty) ? r.entry * r.qty : 0);
const addNullable = (a, b) => (isNumber(a) && isNumber(b) ? a + b : isNumber(a) ? a : b);
const minNullable = (a, b) => (isNumber(a) && isNumber(b) ? Math.min(a, b) : isNumber(a) ? a : b);
const round = (v) => Math.round(v * 1e8) / 1e8; // 0.1 + 0.2 shows as 0.3

/** Keeps rows matching every non-empty filter (bot, strategy, type, moneyness). */
export function filterRows(rows, filters = {}) {
  return rows.filter(
    (r) =>
      (!filters.bot || r.bot === filters.bot) &&
      (!filters.strategy || r.strategyId === filters.strategy) &&
      (!filters.type || r.type === filters.type) &&
      (!filters.moneyness || r.moneyness === filters.moneyness),
  );
}

export const GROUP_KEYS = {
  // One strangle (its slot and expiry): "BTC · 25-day slot · expires 13 Nov · 16 days" (Details (pro), #405).
  strangle: (r) => strangleLabel(r),
  strategy: (r) => `${r.bot.toUpperCase()} · ${r.strategyId}`,
  slot: (r) => `${r.bot.toUpperCase()} · ${r.slot}`,
  expiry: (r) => `${r.bot.toUpperCase()} · ${r.instrument.split("-")[1] ?? "?"}`,
  type: (r) => r.type,
  none: () => "All positions",
};

/**
 * Groups rows and totals them. P&L in USD sums across underlyings; coin P&L
 * is only summed when the group has a single unit.
 */
export function groupRows(rows, by = "strategy") {
  const keyOf = GROUP_KEYS[by] ?? GROUP_KEYS.strategy;
  const groups = new Map();
  for (const r of rows) {
    const k = keyOf(r);
    if (!groups.has(k)) groups.set(k, { key: k, rows: [], units: new Set() });
    const g = groups.get(k);
    g.rows.push(r);
    g.units.add(r.unit);
  }
  return [...groups.values()].map((g) => {
    const single = g.units.size === 1 ? [...g.units][0] : null;
    const premiumUsd = g.rows.reduce((acc, r) => acc + (isNumber(r.spot) ? premiumOf(r) * r.spot : 0), 0);
    const pnlUsd = sum(g.rows, "pnlUsd");
    return {
      key: g.key,
      rows: g.rows,
      legs: g.rows.length,
      unit: single,
      pnl: single ? sum(g.rows, "pnl") : null,
      pnlByCoin: pnlByCoin(g.rows), // { BTC: …, ETH: … }: coins are never added together
      bot: g.rows[0].bot,
      strangleId: g.rows[0].strangleId,
      pnlUsd,
      roiPct: premiumUsd > 0 ? (pnlUsd / premiumUsd) * 100 : null,
      greeks: positionGreeks(g.rows, single),
    };
  });
}

/** Headline numbers across all bots for the KPI strip. */
export function summarise(bots) {
  const kpi = {
    bots: bots.length,
    online: 0,
    openLegs: 0,
    pending: 0,
    submitted: 0,
    filled: 0,
    closed: 0,
    skipped: 0,
    realisedUsd: 0,
    unrealisedUsd: 0,
    totalUsd: 0,
    halted: false,
    since: null, // earliest journal history across bots (ISO), null when unknown
  };
  for (const b of bots) {
    const s = b.status;
    if (!s) continue;
    kpi.online += b.stale ? 0 : 1;
    kpi.openLegs += s.open_legs ?? 0;
    kpi.pending += s.pending ?? 0;
    const c = s.event_counts ?? {};
    kpi.submitted += c.submitted ?? 0;
    kpi.filled += c.filled ?? 0;
    kpi.closed += c.closed ?? 0;
    kpi.skipped += c.skipped ?? 0;
    kpi.halted ||= Boolean(s.halted);
    if (s.history_since && (!kpi.since || Date.parse(s.history_since) < Date.parse(kpi.since))) kpi.since = s.history_since;
    const total = (s.pnl ?? []).find((p) => p.slot == null);
    const spot = s.market?.spot;
    if (total && isNumber(spot)) {
      kpi.realisedUsd += total.realised * spot;
      kpi.unrealisedUsd += total.unrealised * spot;
      kpi.totalUsd += total.total * spot;
    }
  }
  return kpi;
}

const EVENT_STYLE = {
  submitted: { label: "Order sent", tone: "neutral" },
  amended: { label: "Re-priced", tone: "neutral" },
  cancelled: { label: "Cancelled", tone: "warn" },
  filled: { label: "Opened", tone: "good" },
  closed: { label: "Closed", tone: "neutral" },
  reconciled: { label: "Loaded", tone: "neutral" },
  skipped: { label: "Skipped", tone: "muted" },
  risk_limit: { label: "Margin policy", tone: "neutral" },
  trend_changed: { label: "SMA trend", tone: "neutral" },
};

const RISK_CHANGES = {
  frozen: { label: "Entries frozen", tone: "warn" },
  unfrozen: { label: "Unfrozen", tone: "neutral" },
  limit_changed: { label: "IM limit", tone: "neutral" },
  rebalance: { label: "Rebalance", tone: "neutral" },
  mm_breach: { label: "MM breach", tone: "bad" },
  im_over_limit: { label: "IM over limit", tone: "warn" },
  im_over_limit_cleared: { label: "IM back under limit", tone: "neutral" },
};

const CLOSE_LABELS = {
  stop_loss: "Stop-loss",
  dte_rollout: "Roll (time)",
  delta_drift: "Roll (delta drift)",
  delta_exit: "Delta exit",
  roi_target: "Take-profit",
  gamma_regime: "GEX shed",
  kill_switch: "Kill switch",
  manual_close: "Manual close",
  rebalance_downsize: "Rebalance",
  margin_mm_limit: "MM limit",
  rebalance_legs: "Balance legs",
};

/**
 * Turns a journal event into a feed line: { id, at, bot, label, tone, text,
 * context, plain } — plain is the bot's plain-words sentence (#201), null
 * from an older bot that does not serve it.
 * Returns null for events the feed does not show (periodic P&L lines).
 */
export function describeEvent(bot, ev, unit = bot.toUpperCase()) {
  const style = EVENT_STYLE[ev.event];
  if (!style) return null;
  const d = ev.data ?? {};
  const m = d.market ?? {};
  const where = d.slot ? ` [${slotLabel(d.slot)}]` : "";
  let label = style.label;
  let tone = style.tone;
  let text;

  switch (ev.event) {
    case "submitted":
      text = `Sell ${d.qty} ${d.instrument} @ ${formatPrice(d.limit_price)}${where}`;
      break;
    case "amended":
      text = `${d.instrument} ${formatPrice(d.previous_price)} → ${formatPrice(d.limit_price)}${where}`;
      break;
    case "cancelled":
      text = `${d.instrument} (${d.trigger_reason ?? "cancel"})${where}`;
      break;
    case "filled":
      text = `Sold ${d.qty} ${d.instrument} @ ${formatPrice(d.fill_price)}${where}`;
      break;
    case "closed":
      label = CLOSE_LABELS[d.close_reason] ?? "Closed";
      tone = ["stop_loss", "kill_switch", "margin_mm_limit"].includes(d.close_reason) ? "bad" : (d.pnl ?? 0) >= 0 ? "good" : "bad";
      text = `Bought back ${d.qty} ${d.instrument} @ ${formatPrice(d.fill_price)} · P&L ${formatCoin(d.pnl, unit)} (${formatUSD(d.pnl_usd)})${where} · Δ ${formatNumber(d.delta, 3)}${d.detail ? ` — ${d.detail}` : ""}`;
      break;
    case "reconciled":
      text = `${d.qty} ${d.instrument} from exchange${where}`;
      break;
    case "skipped":
      text = `${where.trim() || "Slot"} not entered — ${d.skip_reason ?? "unknown reason"}`;
      break;
    case "risk_limit": {
      const change = RISK_CHANGES[d.change] ?? { label: style.label, tone: style.tone };
      label = d.change === "limit_changed" ? `IM limit ${formatPct(d.limit_im_pct, 0)}` : change.label;
      tone = change.tone;
      const usage = isNumber(d.im_pct) && d.unit ? ` · IM ${formatPct(d.im_pct, 1)} / MM ${formatPct(d.mm_pct, 1)}` : "";
      text = `${d.detail ?? d.change}${usage}`;
      break;
    }
    case "trend_changed": {
      label = `SMA trend ${d.to ?? "?"}`;
      tone = d.to === "neutral" ? "neutral" : "warn";
      const source = d.direction === "bias" ? " (recorded; rules use the GEX bias)" : "";
      text = `${d.from ?? "?"} → ${d.to ?? "?"} — ${d.reason ?? "unknown reason"}${source}`;
      break;
    }
    default:
      text = ev.event;
  }
  const context = [
    isNumber(m.spot) ? `spot ${formatUSD(m.spot)}` : null,
    isNumber(m.dvol) && m.dvol > 0 ? `DVOL ${m.dvol.toFixed(1)}` : null,
    m.moneyness ? `${m.moneyness} ${isNumber(m.distance_to_strike_pct) ? m.distance_to_strike_pct.toFixed(1) + "%" : ""}`.trim() : null,
    isNumber(m.strike_oi) && m.strike_oi > 0 ? `OI ${Math.round(m.strike_oi)}${m.strike_oi_rank ? ` (#${m.strike_oi_rank})` : ""}` : null,
    m.gex_regime || null,
    ev.event === "risk_limit" && isNumber(d.dvol) && d.dvol > 0 ? `DVOL ${d.dvol.toFixed(1)} (p${Math.round(d.iv_percentile ?? 0)})` : null,
    ev.event === "risk_limit" ? d.regime || null : null,
    ...(ev.event === "trend_changed" ? trendContext(d) : []),
  ].filter(Boolean);

  const plain = typeof ev.plain === "string" && ev.plain.trim() ? ev.plain : null;
  return { id: `${bot}:${ev.seq}`, seq: ev.seq, at: ev.at, bot, event: ev.event, label, tone, text, context, plain };
}

/** The levels a trend_changed event carries, as feed context items. */
function trendContext(d) {
  const swing = (name, v, date) => (isNumber(v) && v > 0 ? `${name} ${formatUSD(v)}${date ? ` (${date})` : ""}` : null);
  return [
    isNumber(d.spot) && d.spot > 0 ? `spot ${formatUSD(d.spot)}` : null,
    isNumber(d.sma9) && d.sma9 > 0 ? `SMA9 ${formatUSD(d.sma9)}` : null,
    isNumber(d.sma21) && d.sma21 > 0 ? `SMA21 ${formatUSD(d.sma21)}` : null,
    swing("swing high", d.swing_high, d.swing_high_date),
    swing("swing low", d.swing_low, d.swing_low_date),
  ];
}

/** Merges new feed lines into the list, newest first, capped at max. */
export function mergeFeed(current, incoming, max = 300) {
  const seen = new Set(current.map((e) => e.id));
  const fresh = incoming.filter((e) => e && !seen.has(e.id));
  return [...fresh, ...current].sort((a, b) => Date.parse(b.at) - Date.parse(a.at)).slice(0, max);
}

/**
 * Position greeks of a group: each leg's per-option greek × qty, negated for
 * shorts (a short call has negative delta), as the bot sums its net greeks.
 * Delta and gamma are in the underlying, so they are only summed within one
 * unit; theta and vega are in USD and always add up.
 */
export function positionGreeks(rows, unit) {
  const total = (field) =>
    rows.reduce((acc, r) => {
      if (!isNumber(r[field]) || !isNumber(r.qty)) return acc;
      const sign = r.side === "buy" ? 1 : -1;
      return acc + sign * r[field] * r.qty;
    }, 0);
  return {
    delta: unit ? total("delta") : null,
    gamma: unit ? total("gamma") : null,
    theta: total("theta"),
    vega: total("vega"),
  };
}

function pnlByCoin(rows) {
  const out = {};
  for (const r of rows) if (isNumber(r.pnl)) out[r.unit] = (out[r.unit] ?? 0) + r.pnl;
  return out;
}

function sum(rows, field) {
  return rows.reduce((acc, r) => acc + (isNumber(r[field]) ? r[field] : 0), 0);
}

/**
 * Sums several bots' P&L histories into one USD series, keeping each bot's
 * own total under `bots` (series entries name their bot with `bot`). Bucket
 * times are aligned to the clock by the bots, so equal timestamps mean the same bucket;
 * a bot with no point in a bucket contributes its previous value (P&L is a level).
 * series: [{ points: [{ t, total_usd, realised_usd }] }]
 * Returns [{ t (ms), totalUsd, realisedUsd }] sorted by time.
 */
/**
 * The Live view: stored history before this session's first sample, then the
 * session's own samples — so a reload shows real progress at once instead of
 * an empty chart.
 */
export function prefillLive(stored, live) {
  const first = live.length ? live[0].t : Infinity;
  return [...stored.filter((p) => p.t < first), ...live];
}

/**
 * One bot's stored history in its own coin (BTC, ETH): P&L with the coin's
 * own price moves taken out — what the strategy adds to the coin stack.
 */
export function coinHistory(points) {
  return (points ?? []).map((p) => ({ t: Date.parse(p.t), total: p.total ?? 0, realised: p.realised ?? 0 }));
}

export function combineHistories(series) {
  const times = [...new Set(series.flatMap((s) => (s.points ?? []).map((p) => Date.parse(p.t))))].sort((a, b) => a - b);
  const cursors = series.map((s, n) => ({ bot: s.bot ?? `bot${n + 1}`, points: s.points ?? [], i: 0, last: null }));
  return times.map((t) => {
    let totalUsd = 0;
    let realisedUsd = 0;
    const bots = {};
    for (const c of cursors) {
      while (c.i < c.points.length && Date.parse(c.points[c.i].t) <= t) {
        c.last = c.points[c.i];
        c.i++;
      }
      if (c.last) {
        totalUsd += c.last.total_usd ?? 0;
        realisedUsd += c.last.realised_usd ?? 0;
        bots[c.bot] = c.last.total_usd ?? 0;
      }
    }
    return { t, totalUsd, realisedUsd, bots };
  });
}

/** Each bot's own total P&L in USD: { btc: 305, eth: -12 }. */
export function totalsByBot(bots) {
  return Object.fromEntries(bots.map((b) => [b.name, summarise([b]).totalUsd]));
}
