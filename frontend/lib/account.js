// Pure helpers for the account / collateral panel.

import { ageSeconds } from "./format";

/** Deribit starts liquidation when maintenance margin reaches 100 % of margin balance. */
export const LIQUIDATION_MM_PCT = 100;
export const WARN_PCT = 50;
export const DANGER_PCT = 80;

/** "ok" below 50 %, "warn" from 50 %, "danger" from 80 % of margin balance. */
export function marginLevel(pct) {
  if (typeof pct !== "number" || !Number.isFinite(pct)) return "unknown";
  if (pct >= DANGER_PCT) return "danger";
  if (pct >= WARN_PCT) return "warn";
  return "ok";
}

const MODEL_LABELS = {
  cross_pm: "Cross · Portfolio margin",
  cross_sm: "Cross · Standard margin",
  segregated_pm: "Segregated · Portfolio margin",
  segregated_sm: "Segregated · Standard margin",
};

export function marginModelLabel(model) {
  if (!model) return "Unknown model";
  return MODEL_LABELS[model] ?? model;
}

/**
 * The bots share one Deribit account, so the panel shows a single account:
 * the most recently updated snapshot any bot reported. Returns
 * { snapshot, bot, ageSec, error } or null when no bot has account data.
 */
export function pickAccount(bots, now = Date.now()) {
  let best = null;
  for (const b of bots) {
    const snap = b.account?.snapshot;
    if (!snap) continue;
    const t = Date.parse(snap.as_of);
    if (!best || t > best.t) best = { t, snapshot: snap, bot: b.name, error: b.account.error || null };
  }
  if (!best) return null;
  return { snapshot: best.snapshot, bot: best.bot, error: best.error, ageSec: ageSeconds(best.snapshot.as_of, now) };
}

/** The highest maintenance-margin usage across the totals and every asset. */
export function worstMMPct(snapshot) {
  const values = [snapshot?.totals?.mm_pct, ...(snapshot?.assets ?? []).map((a) => a.mm_pct)].filter((v) => typeof v === "number");
  return values.length ? Math.max(...values) : null;
}

const RULE_LABELS = { dvol_band: "DVOL", gamma_regime: "Gamma regime" };

/** "DVOL → band ≥70 (IM 50%): 1 of 2 daily closes" */
export function pendingLabel(p) {
  return `${RULE_LABELS[p.rule] ?? p.rule} → ${p.to}: ${p.days} of ${p.need} daily closes`;
}

/**
 * One row per bot with the margin policy it applies (from /api/status `risk`).
 * Bots without policy data (older builds, not started yet) are left out.
 */
/**
 * The one-line state shown while the account panel is collapsed: IM vs limit
 * per bot, the worst MM, and what needs attention.
 */
export function accountSummary(account, risk = []) {
  const pct = (v) => (typeof v === "number" && Number.isFinite(v) ? `${v.toFixed(1)}%` : "—");
  const parts = risk.map((r) => `${r.bot.toUpperCase()} IM ${pct(r.imPct)} / ${r.limitIMPct}%`);
  const mm = account ? worstMMPct(account.snapshot) : null;
  parts.push(`MM ${pct(mm)}`);
  return {
    text: parts.join(" · "),
    level: marginLevel(mm),
    frozen: risk.filter((r) => r.frozen).map((r) => r.bot.toUpperCase()),
    missing: !account,
  };
}

export function riskRows(bots) {
  return bots
    .filter((b) => b.status?.risk?.status)
    .map((b) => {
      const r = b.status.risk;
      return {
        bot: b.name,
        limitIMPct: r.status.limit_im_pct,
        maxMMPct: r.status.max_mm_pct,
        reason: r.status.reason,
        frozen: r.status.frozen,
        freezeReason: r.status.freeze_reason || null,
        pending: (r.status.pending ?? []).map(pendingLabel),
        imPct: r.error ? null : r.im_pct,
        mmPct: r.error ? null : r.mm_pct,
        unit: r.unit,
        error: r.error || null,
      };
    });
}

/**
 * Positions on the account that no bot manages: every position Deribit
 * reports (the freshest account snapshot) minus the legs in the bots' books.
 * They are not traded by any bot but still use margin.
 * Returns { rows, error, reported } — reported is false for bots too old to
 * send positions, so "none" is never shown when the list is unknown.
 */
export function unmanagedPositions(account, bots) {
  const snap = account?.snapshot;
  if (!snap || snap.positions === undefined) return { rows: [], error: null, reported: false };
  if (snap.positions === null) return { rows: [], error: snap.positions_error || "positions unavailable", reported: true };
  const managed = new Set(
    bots.flatMap((b) => (b.positions?.strangles ?? []).flatMap((s) => (s.legs ?? []).map((l) => l.instrument))),
  );
  const rows = snap.positions.filter((p) => !managed.has(p.instrument));
  return { rows, error: snap.positions_error || null, reported: true };
}

/** Sum of a field per currency: { BTC: −0.106, … }. */
export function sumByCurrency(rows, field) {
  const out = {};
  for (const r of rows) {
    if (typeof r[field] === "number") out[r.currency] = (out[r.currency] ?? 0) + r[field];
  }
  return out;
}
