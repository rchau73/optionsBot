// Server-side only: the monitor's analytics collector (event plan §3 and §5,
// #193). Builds each event's envelope and appends it to
// data/monitor/events.jsonl. Nothing leaves the host; the bot never reads it.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { authLog } from "@/lib/auth/log";

export const MIN_SALT = 16;
const ENVIRONMENTS = ["stable", "mvp"];
const VERSION = /^[A-Za-z0-9._-]{1,40}$/;

/** EVENTS_PATH, default events.jsonl beside the user database; null (off) when the database is in memory. */
export function eventsPath(dbPath, raw = process.env.EVENTS_PATH) {
  if (raw) return raw;
  return dbPath === ":memory:" ? null : path.join(path.dirname(dbPath), "events.jsonl");
}

/** ANALYTICS_SALT when it is long enough, else null (collection off). */
export function analyticsSalt(raw = process.env.ANALYTICS_SALT) {
  return raw && raw.length >= MIN_SALT ? raw : null;
}

/** The pseudonymous user id: HMAC-SHA256 of the user's row id with the salt, first 16 hex characters. */
export function pseudonymousId(salt, userRowId) {
  return crypto.createHmac("sha256", salt).update(String(userRowId)).digest("hex").slice(0, 16);
}

/**
 * One stored line: the envelope (§3) around a validated event. Only the
 * environment and version the deployment set, when well-formed; never the
 * IP, user agent, referrer, username or email.
 */
export function buildEntry({ valid, user, salt, nowMs, env = process.env }) {
  const entry = {
    event: valid.event,
    ts: new Date(Math.floor(nowMs / 1000) * 1000).toISOString().replace(".000Z", "Z"),
    user_id: pseudonymousId(salt, user.id),
    session_id: valid.session_id,
    role: user.role,
  };
  if (ENVIRONMENTS.includes(env.MONITOR_DEPLOYMENT)) entry.environment = env.MONITOR_DEPLOYMENT;
  if (VERSION.test(env.MONITOR_VERSION ?? "")) entry.app_version = env.MONITOR_VERSION;
  return { ...entry, ...valid.props };
}

/** Appends one JSON line; false (and an ERROR log) when the write fails. */
export function appendEvent(file, entry) {
  try {
    fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
    fs.appendFileSync(file, `${JSON.stringify(entry)}\n`, { mode: 0o600 });
    return true;
  } catch (err) {
    authLog("ERROR", "analytics event write failed", { component: "analytics", file, error: err.message });
    return false;
  }
}
