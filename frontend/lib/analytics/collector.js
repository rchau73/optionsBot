// Server-side only: the monitor's analytics collector (event plan §3 and §5,
// #193). Builds each event's envelope and appends it to
// data/monitor/events.jsonl. Nothing leaves the host; the bot never reads it.
import fs from "node:fs";
import path from "node:path";
import { authLog } from "@/lib/auth/log";
import { pseudonymousId } from "./config.js";

// Kept here too for the collector's callers; they live in config.js so the admin CLI can load them.
export { MIN_SALT, analyticsSalt, eventsPath, pseudonymousId } from "./config.js";

const ENVIRONMENTS = ["stable", "mvp"];
const VERSION = /^[A-Za-z0-9._-]{1,40}$/;

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
