// Server-side only: what event plan §5 promises about stored analytics events
// (#296) — raw events are kept EVENT_RETENTION_DAYS and then pruned daily, a
// deleted user's events go with them, and an admin can export or delete one
// person's events on request (scripts/analytics-user.mjs).
//
// Every change rewrites events.jsonl whole and atomically: the kept lines go
// to events.jsonl.tmp (mode 0600, flushed to disk), which is then renamed over
// the file, so a crash leaves the old or the new full file, never a partial
// one. The monitor appends events synchronously, as these functions read and
// write, so inside the monitor nothing can land in between; another process
// (the admin CLI) re-reads when the file grew while it worked.
// Explicit extensions: the admin CLI loads this with plain `node`.
import fs from "node:fs";
import { pseudonymousId } from "./config.js";
import { authLog } from "../auth/log.js";

/** How long raw events are kept: 13 months (event plan §5; needs legal review — change it here only). */
export const EVENT_RETENTION_DAYS = 395;

export const DAY_MS = 24 * 60 * 60 * 1000;
const USER_ID = /^[0-9a-f]{16}$/;
const MAX_ATTEMPTS = 3;

/** The stored lines of an events file (no empty ones); [] when it does not exist. */
function readLines(file) {
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (err) {
    if (err.code === "ENOENT") return { text: "", lines: [] };
    throw err;
  }
  return { text, lines: text.split("\n").filter((l) => l.trim() !== "") };
}

/** The line as an object, or null when it is not one JSON object. */
function parse(line) {
  try {
    const v = JSON.parse(line);
    return v && typeof v === "object" && !Array.isArray(v) ? v : null;
  } catch {
    return null;
  }
}

/** Writes `text` to `tmp` with mode 0600 and flushes it to disk before returning. */
function writeDurable(tmp, text) {
  const fd = fs.openSync(tmp, "w", 0o600);
  try {
    fs.writeSync(fd, text);
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  // A tmp file left by a crash keeps its old mode when reopened.
  fs.chmodSync(tmp, 0o600);
}

/**
 * Rewrites `file` without the lines `keep(line, index)` refuses, atomically (temp file +
 * rename, mode 0600). Untouched when nothing is removed or the file does not
 * exist. → { kept, removed }
 */
export function rewriteEvents(file, keep) {
  const tmp = `${file}.tmp`;
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    const { text, lines } = readLines(file);
    const kept = lines.filter(keep);
    const removed = lines.length - kept.length;
    if (removed === 0) return { kept: kept.length, removed: 0 };

    try {
      writeDurable(tmp, kept.length ? `${kept.join("\n")}\n` : "");
      // Another process appended meanwhile: start again from the longer file.
      if (fs.statSync(file).size !== Buffer.byteLength(text)) continue;
      fs.renameSync(tmp, file);
      return { kept: kept.length, removed };
    } finally {
      fs.rmSync(tmp, { force: true });
    }
  }
  throw new Error(`${file} kept changing during the rewrite: try again`);
}

/**
 * Drops the events older than `days` before nowMs. A line without a readable
 * `ts` is kept (never deleted on a guess) and counted as `unreadable`.
 * → { kept, removed, unreadable }
 */
export function pruneEvents(file, nowMs, days = EVENT_RETENTION_DAYS) {
  const cutoff = nowMs - days * DAY_MS;
  let unreadable = 0;
  const r = rewriteEvents(file, (line, i) => {
    if (i === 0) unreadable = 0; // a new pass over the file (rewriteEvents may read it again)
    const ts = Date.parse(parse(line)?.ts ?? "");
    if (!Number.isFinite(ts)) {
      unreadable++;
      return true;
    }
    return ts >= cutoff;
  });
  return { ...r, unreadable };
}

function checkUserId(userId) {
  if (!USER_ID.test(userId ?? "")) throw new Error("user id: 16 hex characters (pseudonymousId)");
}

/** Deletes every event of a pseudonymous user id (pseudonymousId(salt, userRowId)). → { kept, removed } */
export function deleteEventsOf(file, userId) {
  checkUserId(userId);
  return rewriteEvents(file, (line) => parse(line)?.user_id !== userId);
}

/** The stored lines (as written) of a pseudonymous user id, oldest first. */
export function exportEventsOf(file, userId) {
  checkUserId(userId);
  return readLines(file).lines.filter((line) => parse(line)?.user_id === userId);
}

/** Writes exported lines to a new file (mode 0600); never overwrites one. */
export function writeExport(outFile, lines) {
  fs.writeFileSync(outFile, lines.length ? `${lines.join("\n")}\n` : "", { mode: 0o600, flag: "wx" });
}

/**
 * The step AuthService.deleteUser runs after deleting a user: removes the
 * events of that user's row id. Throws when it cannot (the caller logs and
 * reports it); { removed: 0 } when analytics has no file.
 */
export function userEventsEraser(file, salt) {
  return (userRowId) => {
    if (!file || !fs.existsSync(file)) return { removed: 0 };
    if (!salt) throw new Error("ANALYTICS_SALT is not set: the user's events cannot be found");
    return { removed: deleteEventsOf(file, pseudonymousId(salt, userRowId)).removed };
  };
}

/** One prune with its audit line and log; never throws (a failure is an ERROR log and an audit line). */
export function pruneAndAudit(file, nowMs, audit, days = EVENT_RETENTION_DAYS) {
  const time = new Date(nowMs).toISOString();
  try {
    const r = pruneEvents(file, nowMs, days);
    authLog("INFO", "analytics events pruned", { component: "analytics", file, retention_days: days, ...r });
    if (r.unreadable > 0) {
      authLog("WARN", "analytics events without a readable time kept", { component: "analytics", file, unreadable: r.unreadable });
    }
    audit({ time, action: "events_pruned", username: "system", retention_days: days, ...r });
    return { ok: true, ...r };
  } catch (err) {
    authLog("ERROR", "analytics events prune failed", { component: "analytics", file, error: err.message });
    audit({ time, action: "events_prune_failed", username: "system", retention_days: days, error: err.message });
    return { ok: false, error: err.message };
  }
}

// One job per server process (dev-mode reloads re-run instrumentation).
const JOB = Symbol.for("optionsbot.monitor.retention");

/**
 * Prunes now and then every intervalMs (a day). Nothing when analytics has no
 * file. The timer does not keep the process alive. → the timer or null.
 */
export function startRetentionJob({ file, audit, now = () => Date.now(), intervalMs = DAY_MS, days = EVENT_RETENTION_DAYS }) {
  if (!file) return null;
  if (globalThis[JOB]) clearInterval(globalThis[JOB]);
  pruneAndAudit(file, now(), audit, days);
  const timer = setInterval(() => pruneAndAudit(file, now(), audit, days), intervalMs);
  timer.unref?.();
  globalThis[JOB] = timer;
  return timer;
}

/** Stops the job (tests). */
export function stopRetentionJob() {
  if (globalThis[JOB]) clearInterval(globalThis[JOB]);
  globalThis[JOB] = undefined;
}
