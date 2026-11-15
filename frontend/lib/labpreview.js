// The lab's apply preview (#682, epic E9): what a manager's confirm would
// change for a finished run, and why it may not be confirmable. The confirm
// (lib/labapply.js, #741) only records the change: nothing here or there
// writes a config or a bot setting.
// Every doubt fails closed: a value that cannot be read gives no token.
import crypto from "node:crypto";
import labSettings from "@/content/lab-settings.json";
import { LAB_LABEL } from "@/lib/lab";
import { catalogueKey, validateProposal } from "@/lib/labvalidate";

/** A run past this age gives no token: its 7 days of data (cmd/labrun) no longer overlap today's window. */
export const PREVIEW_MAX_AGE_DAYS = 7;

/** The audit action of a preview read (auth.recordAction). */
export const PREVIEW_AUDIT_ACTION = "parameter_change_previewed";

/** What every preview says. */
export const NOTHING_APPLIED = "nothing was changed: a preview only, no config file or bot setting is touched";

const DAY_MS = 24 * 3600 * 1000;

/** A report value (always text) as the setting's own type: "0.12" → 0.12, "true" → true, "bias" stays. */
function parseValue(text) {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

/** The account's value of a report key: one element of a list key; null when it cannot be told which. */
function accountValue(current, key) {
  if (!Array.isArray(current)) return current;
  const indices = [...key.matchAll(/\[(\d+)\]/g)];
  return indices.length === 1 ? (current[Number(indices[0][1])] ?? null) : null;
}

/** True when the report's text says the same value as the account's ("0.10" and 0.1). */
function sameValue(current, reported) {
  const text = String(reported);
  return String(current) === text || (typeof current === "number" && text.trim() !== "" && Number(text) === current);
}

/** One changed setting: { change, staleReasons }. A key the catalogue refuses counts as core policy: nobody can say it is not. */
function previewChange(diff, underlying) {
  const { status, body } = validateProposal({ key: catalogueKey(diff.key), value: parseValue(diff.proposed), underlying });
  if (status !== 200) {
    const change = { key: diff.key, title: null, tip: null, current: null, base: diff.base, proposed: diff.proposed, core_policy: true, policy_note: null };
    return { change, staleReasons: [`${diff.key}: ${body.error}`] };
  }
  const current = accountValue(body.current, diff.key);
  const change = {
    key: diff.key,
    title: body.title,
    tip: body.tip,
    current,
    base: diff.base,
    proposed: diff.proposed,
    core_policy: body.core_policy,
    policy_note: body.policy_note,
  };
  const staleReasons = [];
  if (current === null) staleReasons.push(`${diff.key}: the account's value cannot be read`);
  else if (!sameValue(current, diff.base)) staleReasons.push(`${diff.key}: the run started from ${diff.base}, the account now has ${current}`);
  if (!body.valid) staleReasons.push(`${diff.key}: ${diff.proposed} ${body.reasons.join("; ")}`);
  return { change, staleReasons };
}

/** True when the run is older than the limit; a time that cannot be read counts as too old. */
function tooOld(time, now) {
  const age = now - Date.parse(time);
  return !(age <= PREVIEW_MAX_AGE_DAYS * DAY_MS);
}

/** The fragility check of a report: the cliffs flagged, or null cliffs and a note when it was not run. */
function fragility(report) {
  const n = report.neighbours;
  if (!n) return { checked: false, key: null, cliffs: null, note: "no fragility check was run for this setting" };
  return { checked: true, key: n.key ?? null, cliffs: Array.isArray(n.cliffs) ? n.cliffs : [], note: null };
}

/** The text of the audit line: the person, the account, the run and both values. */
function auditText({ person, account, run_id: runId, changes }) {
  const values = changes.map((c) => `${c.key} ${c.current ?? "unknown"} → ${c.proposed}`).join(", ");
  return `parameter change previewed: ${person}, account ${account.underlying} (${account.config ?? "no config"}), run ${runId}, ${values}`;
}

/**
 * The preview of one run (readRun's `run`) for `person` at `now` (ms). A run
 * that changed no setting → null: there is nothing to confirm. `reasons` says
 * in plain words why `confirmable` is false; the route adds the token only
 * when it is true.
 */
export function buildPreview(run, person, now) {
  const { report } = run;
  if (run.changed.length === 0) return null;
  const underlying = String(report.underlying ?? "").toUpperCase();
  const previewed = run.changed.map((d) => previewChange(d, underlying));
  const changes = previewed.map((p) => p.change);

  const staleReasons = previewed.flatMap((p) => p.staleReasons);
  if (tooOld(run.time, now)) staleReasons.unshift(`the run is older than ${PREVIEW_MAX_AGE_DAYS} days`);
  const coreKeys = changes.filter((c) => c.core_policy).map((c) => c.key);
  const hasNumbers = Boolean(run.backtest?.proposed || run.replay?.proposed);

  const reasons = [...staleReasons];
  if (coreKeys.length) reasons.push(`${coreKeys.join(", ")}: ${labSettings.policy_note}`);
  if (!hasNumbers) reasons.push("the run has no backtested numbers for the proposed value");

  const preview = {
    run_id: run.id,
    time: run.time,
    person,
    account: { underlying, config: labSettings.configs[underlying]?.file ?? null },
    changes,
    backtested: { label: LAB_LABEL, backtest: run.backtest, replay: run.replay, stress_ran: run.stress_ran, windows: run.windows },
    fragility: fragility(report),
    needs_product_owner: coreKeys.length > 0,
    stale: staleReasons.length > 0,
    confirmable: reasons.length === 0,
    reasons,
    confirm_token: null,
    analysis: run.analysis,
    applied: NOTHING_APPLIED,
  };
  return { ...preview, audit_line: auditText(preview) };
}

/** A confirm token is accepted for at most this long after the preview that issued it (and at least 10 minutes). */
export const CONFIRM_TOKEN_MINUTES = 15;
const TOKEN_WINDOW_MS = 5 * 60 * 1000;
const TOKEN_WINDOWS = CONFIRM_TOKEN_MINUTES / 5;

function tokenFor(preview, windowIndex, key) {
  const { run_id: runId, person, account, changes } = preview;
  const shown = { runId, person, account, changes: changes.map((c) => [c.key, c.current, c.proposed]) };
  return crypto.createHmac("sha256", key).update(`lab-apply-preview\n${windowIndex}\n${JSON.stringify(shown)}`).digest("hex");
}

/**
 * The confirm token: an HMAC of exactly what was shown to this person and of
 * the 5-minute window of `now` (ms), under the monitor's secret, so only this
 * server can issue one and it expires. The apply (lib/labapply.js) recomputes
 * it from the same run and values; it is no sign-in token.
 */
export function confirmToken(preview, key, now) {
  return tokenFor(preview, Math.floor(now / TOKEN_WINDOW_MS), key);
}

/**
 * True when `token` is the confirm token of `preview` issued in the 5-minute
 * window of `now` or one of the two before it: at most CONFIRM_TOKEN_MINUTES
 * old. Compared in constant time.
 */
export function confirmTokenMatches(token, preview, key, now) {
  if (typeof token !== "string" || !/^[0-9a-f]{64}$/.test(token)) return false;
  const given = Buffer.from(token, "hex");
  const window = Math.floor(now / TOKEN_WINDOW_MS);
  let match = false;
  for (let i = 0; i < TOKEN_WINDOWS; i++) {
    if (crypto.timingSafeEqual(given, Buffer.from(tokenFor(preview, window - i, key), "hex"))) match = true;
  }
  return match;
}
