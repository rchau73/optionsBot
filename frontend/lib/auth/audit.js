// Server-side only: the monitor's audit trail, data/monitor/audit.jsonl (one
// JSON line per sign-in, failed sign-in, lockout, user change and manual
// action, with the person and time). Append-only; never a password, hash,
// token or AUTH_SECRET.
import fs from "node:fs";
import path from "node:path";
import { authLog } from "./log.js"; // explicit extension: scripts/analytics-user.mjs loads this with plain node

/** An audit sink appending one JSON line per entry to `file`; null file = off (tests in memory). */
export function fileAudit(file) {
  if (!file) return () => {};
  return (entry) => {
    try {
      fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
      fs.appendFileSync(file, `${JSON.stringify(entry)}\n`, { mode: 0o600 });
    } catch (err) {
      // The action itself already happened; losing its audit line must be loud, not fatal.
      authLog("ERROR", "audit write failed", { file, error: err.message });
    }
  };
}

/** AUDIT_PATH, default audit.jsonl beside the user database; off when the database is in memory. */
export function auditPath(dbPath, raw = process.env.AUDIT_PATH) {
  if (raw) return raw;
  return dbPath === ":memory:" ? null : path.join(path.dirname(dbPath), "audit.jsonl");
}
