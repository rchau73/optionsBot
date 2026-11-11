// The lab's confirm (#741, epic E9): a manager confirms the change a finished
// run's preview (lib/labpreview.js) showed, and the server records it, audited,
// only when the preview, recomputed now, still allows it and the token is the
// one it issued to this person, under 15 minutes old and never used.
// The record is the whole effect: how a recorded change reaches a running bot
// is not decided yet (a reviewed config PR and a restart, or a later
// hot-reload), so nothing here writes a config, restarts a bot or places an
// order, and every answer says `applied: false`.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { LAB_LABEL } from "@/lib/lab";
import { buildPreview, CONFIRM_TOKEN_MINUTES, confirmTokenMatches } from "@/lib/labpreview";
import { STATUS } from "@/lib/labrequests";

/** The audit action of a recorded change (auth.recordAction). */
export const RECORD_AUDIT_ACTION = "parameter_change_recorded";

/** What every confirm says, recorded or refused. */
export const NOT_APPLIED = "nothing was applied to a running bot: no config file was written, no bot was restarted and no order was placed";

/** What a recorded change still waits for. */
export const NEXT_STEP = "how a recorded change reaches the running bot is not decided yet: until then this record is the whole effect";

/** Who approves a core-policy default instead of a manager (PRD §3). */
export const APPROVER = "the product owner";

/** The folder of the records inside the comparison reports' folder. */
export const recordsDir = (compareDir) => path.join(compareDir, "applied");

/** A short id of a confirm token for the record and the audit line: never the token itself. */
export const tokenId = (token) => crypto.createHash("sha256").update(token).digest("hex").slice(0, 16);

/** The record of a run already confirmed, or null; a file that cannot be read still counts as recorded. */
export function readRecord(dir, runId) {
  const file = path.join(dir, `${runId}.json`);
  if (!fs.existsSync(file)) return null;
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return {};
  }
}

const refusal = (status, error, extra = {}) => ({ status, body: { error, ...extra, applied: false, recorded: false, nothing_applied: NOT_APPLIED } });

/**
 * Checks a confirm of `run` (readRun's run) by `person` with `token` at `now`
 * (ms): { preview } when it may be recorded, else { status, body } with the
 * reason in plain words. `requests` are the run requests (to tell a run still
 * running), `recorded` the run's record or null.
 */
export function checkConfirm({ run, person, token, key, now, requests, recorded }) {
  if (typeof token !== "string" || token === "") {
    return refusal(400, 'send {"confirm_token": "<the token the preview gave you>"}');
  }
  if (recorded) {
    const by = recorded.person ? ` by ${recorded.person} at ${recorded.recorded_at}` : "";
    return refusal(409, `this run's change was already recorded${by}: a confirm is used once`);
  }
  const unfinished = requests.find((r) => r.run_id === run.id && r.status !== STATUS.done);
  if (unfinished) return refusal(409, `the run is not finished (its request is ${unfinished.status}): wait until it is done`);
  const preview = buildPreview(run, person, now);
  if (!preview) return refusal(409, "this run changed no setting: there is nothing to confirm");
  if (preview.needs_product_owner) {
    const keys = preview.changes.filter((c) => c.core_policy).map((c) => c.key);
    return refusal(409, `${keys.join(", ")}: a core policy, so a new default needs ${APPROVER}, not a manager's confirm`, {
      approver: APPROVER,
      reasons: preview.reasons,
    });
  }
  if (!preview.confirmable) return refusal(409, `this run cannot be confirmed: ${preview.reasons.join("; ")}`, { reasons: preview.reasons });
  if (!confirmTokenMatches(token, preview, key, now)) {
    return refusal(
      409,
      `the confirm token does not match this run, your username and these values, or it is older than ${CONFIRM_TOKEN_MINUTES} minutes: open the preview again`,
    );
  }
  return { preview };
}

/** The risks beside the numbers, in plain words and percentages only. */
function riskLines(preview) {
  const lines = [];
  for (const [name, source] of [["backtest", preview.backtested.backtest], ["replay of recorded books", preview.backtested.replay]]) {
    const p = source?.proposed;
    if (!p) continue;
    const pct = (v) => (Number.isFinite(v) ? `${v.toFixed(2)} %` : "unknown");
    lines.push(`${name}: worst week ${pct(p.worst_week_pct)}, largest drawdown ${pct(p.max_drawdown_pct)}, fees ${pct(p.fees_pct)} of the starting equity`);
  }
  const f = preview.fragility;
  if (!f.checked) lines.push(f.note);
  else if (f.cliffs.length) lines.push(`${f.cliffs.length} fragile spot(s) near the proposed value: a small change around it moved the result sharply`);
  lines.push("past data only: the market can move in ways these days did not show");
  return lines;
}

/** The text of the audit line: the person, the account, the run, both values and the token's id. */
function recordText(record) {
  const { person, account, run_id: runId, changes, token_id: id } = record;
  const values = changes.map((c) => `${c.key} ${c.old} → ${c.new}`).join(", ");
  return `parameter change recorded: ${person}, account ${account.underlying} (${account.config ?? "no config"}), run ${runId}, ${values}, token ${id}; nothing applied to a running bot`;
}

/** The record of a confirmed change (data/compare/applied/<run id>.json). */
export function buildRecord(preview, user, token, now) {
  const record = {
    run_id: preview.run_id,
    recorded_at: new Date(now).toISOString(),
    person: user.username,
    role: user.role,
    account: preview.account,
    changes: preview.changes.map((c) => ({ key: c.key, title: c.title, old: c.current, new: c.proposed })),
    token_id: tokenId(token),
    label: LAB_LABEL,
    applied: false,
    recorded: true,
  };
  return { ...record, line: recordText(record) };
}

/** Writes the record once: an existing file throws EEXIST, so a confirm raced twice records once. */
export function writeRecord(dir, record) {
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  fs.writeFileSync(path.join(dir, `${record.run_id}.json`), `${JSON.stringify(record, null, 2)}\n`, { mode: 0o600, flag: "wx" });
}

/** Removes a run's record: its audit line could not be written, so the confirm did not happen and may be retried. */
export function removeRecord(dir, runId) {
  fs.rmSync(path.join(dir, `${runId}.json`), { force: true });
}

/** The answer of a recorded change: what was recorded, the numbers labelled as backtested and the risks next to them. */
export function recordedAnswer(record, preview) {
  const values = record.changes.map((c) => `${c.key} from ${c.old} to ${c.new}`).join(", ");
  return {
    applied: false,
    recorded: true,
    message: `Recorded: ${values} for ${record.account.underlying}. The result was ${LAB_LABEL}; ${NOT_APPLIED}.`,
    next_step: NEXT_STEP,
    run_id: record.run_id,
    recorded_at: record.recorded_at,
    person: record.person,
    account: record.account,
    changes: record.changes,
    token_id: record.token_id,
    backtested: preview.backtested,
    risks: riskLines(preview),
    audit_line: record.line,
  };
}
