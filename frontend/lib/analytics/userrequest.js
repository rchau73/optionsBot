// Server-side only: the admin's answer to one person's access or deletion
// request (event plan §5, LGPD art. 18 / GDPR arts. 15 and 17; #296) — the
// logic of scripts/analytics-user.mjs, kept here so tests call it directly.
// Reads auth.db (never writes it: the running monitor holds its own copy) and
// prints counts and paths only, never an event's contents. An export also
// carries the person's survey answers from auth.db (codes and free text, one
// {"record": "survey_answer", …} line each, after the events; #363).
// Explicit extensions: the CLI loads this with plain `node`.
import fs from "node:fs";
import path from "node:path";
import { AuthStore } from "../auth/store.js";
import { auditPath, fileAudit } from "../auth/audit.js";
import { analyticsSalt, eventsPath, MIN_SALT, pseudonymousId } from "./config.js";
import { deleteEventsOf, exportEventsOf, writeExport } from "./retention.js";

export const USAGE = `usage: node scripts/analytics-user.mjs export|delete <username> [--out FILE]
       node scripts/analytics-user.mjs export|delete --row-id <n> [--out FILE]   (a user already deleted)
environment: AUTH_DB_PATH (required), ANALYTICS_SALT (required), EVENTS_PATH, AUDIT_PATH (default beside auth.db)`;

/** The request in argv → { action, username, rowId, out } or { error }. */
export function parseArgs(argv) {
  const [action, ...rest] = argv;
  if (action !== "export" && action !== "delete") return { error: "first argument: export or delete" };
  const req = { action, username: null, rowId: null, out: null };
  for (let i = 0; i < rest.length; i++) {
    const a = rest[i];
    if (a === "--row-id") {
      const n = Number(rest[++i]);
      if (!Number.isInteger(n) || n <= 0) return { error: "--row-id: a positive whole number" };
      req.rowId = n;
    } else if (a === "--out") {
      if (!rest[i + 1]) return { error: "--out: a file name" };
      req.out = rest[++i];
    } else if (!a.startsWith("--") && req.username === null) {
      req.username = a;
    } else {
      return { error: `unexpected argument: ${a}` };
    }
  }
  if ((req.username === null) === (req.rowId === null)) return { error: "give either a username or --row-id" };
  if (req.out && action !== "export") return { error: "--out is for export only" };
  return req;
}

/** A user's survey answers, or [] when the file predates the survey_answers table (#363). */
function surveyAnswersOf(store, rowId) {
  if (!store.one("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'survey_answers'")) return [];
  return store.surveyAnswers(rowId);
}

function stamp(nowMs) {
  return new Date(nowMs).toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
}

/**
 * Runs one request. env: the process environment · cwd: where relative paths
 * start · by: who runs it (the host account), for the audit line.
 * → { code: 0 | 1, out: [printable lines] }
 */
export async function runUserRequest({ argv, env = process.env, cwd = process.cwd(), nowMs = Date.now(), by = "cli" }) {
  const req = parseArgs(argv);
  if (req.error) return { code: 1, out: [req.error, USAGE] };

  // Both are required: a wrong default would read another environment's users.
  if (!env.AUTH_DB_PATH) return { code: 1, out: ["AUTH_DB_PATH is not set: the environment's auth.db (stable and mvp each have one)", USAGE] };
  const salt = analyticsSalt(env.ANALYTICS_SALT);
  if (!salt) return { code: 1, out: [`ANALYTICS_SALT is not set (≥ ${MIN_SALT} chars): use the environment's .env value`, USAGE] };
  const dbPath = path.resolve(cwd, env.AUTH_DB_PATH);
  if (!fs.existsSync(dbPath)) return { code: 1, out: [`no user database at ${dbPath}`] };
  const file = path.resolve(cwd, eventsPath(dbPath, env.EVENTS_PATH));
  const audit = fileAudit(path.resolve(cwd, auditPath(dbPath, env.AUDIT_PATH)));

  let rowId = req.rowId;
  let target = rowId ? `row ${rowId}` : req.username;
  const store = await AuthStore.openReadOnly(dbPath);
  let answers;
  try {
    if (!rowId) {
      const user = store.userByName(req.username);
      if (!user) return { code: 1, out: [`no user ${req.username} in ${dbPath} (already deleted? use --row-id from the deletion's log line)`] };
      rowId = user.id;
      target = user.username;
    }
    // Survey answers live in auth.db beside the user (#363): deleted with the
    // user by the monitor, exported here. A file from before #363 has none.
    answers = req.action === "export" ? surveyAnswersOf(store, rowId) : [];
  } finally {
    store.db.close();
  }
  const userId = pseudonymousId(salt, rowId);
  const time = new Date(nowMs).toISOString();
  const who = { username: by, role: "admin", target, target_row_id: rowId, reason: "request" };

  try {
    if (req.action === "export") {
      const lines = exportEventsOf(file, userId);
      const out = path.resolve(cwd, req.out ?? path.join(path.dirname(file), `analytics-export-${target.replace(/\W+/g, "-")}-${stamp(nowMs)}.jsonl`));
      writeExport(out, [...lines, ...answers.map((a) => JSON.stringify({ record: "survey_answer", ...a }))]);
      audit({ time, action: "events_exported", ...who, lines: lines.length, survey_answers: answers.length, file: out });
      return { code: 0, out: [`exported ${lines.length} events and ${answers.length} survey answers of ${target} to ${out}`] };
    }
    const { removed } = deleteEventsOf(file, userId);
    audit({ time, action: "events_deleted", ...who, removed });
    return { code: 0, out: [`deleted ${removed} events of ${target} from ${file}`] };
  } catch (err) {
    audit({ time, action: `events_${req.action}_failed`, ...who, error: err.message });
    return { code: 1, out: [`${req.action} failed: ${err.message}`] };
  }
}
