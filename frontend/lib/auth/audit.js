// Server-side only: the monitor's audit trail, data/monitor/audit.jsonl (one
// JSON line per sign-in, failed sign-in, lockout, user change and manual
// action, with the person and time). Append-only; never a password, hash,
// token or AUTH_SECRET. Each line is linked to the one before by a hash
// (#619, lib/auth/auditchain.js); past AUDIT_MAX_BYTES the file is renamed to
// audit.<UTC time>.jsonl and the chain goes on in a new file. Nothing is deleted.
import fs from "node:fs";
import path from "node:path";
import { sealLine, sha256 } from "./auditchain.js";
import { authLog } from "./log.js"; // explicit extension: scripts/analytics-user.mjs loads this with plain node

/** The size past which the trail is rotated when AUDIT_MAX_BYTES is not set: 20 MB. */
export const DEFAULT_AUDIT_MAX_BYTES = 20 * 1024 * 1024;

const CHUNK = 64 * 1024;
const NEWLINE = 0x0a;

/** AUDIT_MAX_BYTES: a positive whole number of bytes, else the 20 MB default (a bad value is logged). */
export function auditMaxBytes(raw = process.env.AUDIT_MAX_BYTES) {
  if (raw === undefined || raw === "") return DEFAULT_AUDIT_MAX_BYTES;
  if (/^[1-9]\d{0,15}$/.test(raw)) return Number(raw);
  authLog("WARN", "AUDIT_MAX_BYTES is not a positive number of bytes; using the default", { value: raw, bytes: DEFAULT_AUDIT_MAX_BYTES });
  return DEFAULT_AUDIT_MAX_BYTES;
}

/**
 * An audit sink appending one chained JSON line per entry to `file`; null file
 * = off (tests in memory). The whole append (read the last line, maybe rotate,
 * write) is synchronous, so two actions of this process never take the same `prev`.
 */
export function fileAudit(file, { maxBytes = auditMaxBytes(), now = () => new Date() } = {}) {
  if (!file) return () => {};
  return (entry) => {
    try {
      fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
      appendChained(file, entry, maxBytes, now);
    } catch (err) {
      // The action itself already happened; losing its audit line must be loud, not fatal.
      authLog("ERROR", "audit write failed", { file, error: err.message });
    }
  };
}

function appendChained(file, entry, maxBytes, now) {
  let fd = fs.openSync(file, "a+", 0o600);
  try {
    const size = fs.fstatSync(fd).size;
    let last = size > 0 ? lastLine(fd, size) : null;
    if (size === 0) {
      // A new file goes on from the newest archive, so the chain crosses files.
      const archives = auditArchives(file);
      if (archives.length > 0) last = lastLineOfFile(archives.at(-1));
    }
    const prev = last === null ? "" : sha256(last);
    // A last line cut short (no newline) stays as it is: the new line starts on its own.
    let lead = size > 0 && !endsWithNewline(fd, size) ? "\n" : "";
    if (size >= maxBytes) {
      fs.closeSync(fd);
      fd = null;
      const archive = rotate(file, now());
      authLog("INFO", "audit trail rotated", { file, archive, bytes: size });
      fd = fs.openSync(file, "a", 0o600);
      lead = "";
    }
    fs.writeSync(fd, `${lead}${sealLine(entry, prev)}\n`);
  } finally {
    if (fd !== null) fs.closeSync(fd);
  }
}

/** Renames the trail to <name>.<UTC time>.jsonl, owner-only, never over an existing archive → its path. */
function rotate(file, at) {
  const taken = new Set(auditArchives(file));
  let t = at.getTime();
  let archive;
  do {
    archive = archiveName(file, new Date(t));
    t += 1;
  } while (taken.has(archive));
  fs.renameSync(file, archive);
  fs.chmodSync(archive, 0o600);
  return archive;
}

/** audit.jsonl at 2026-11-04T03:14:33.123Z → audit.20261104T031433123Z.jsonl, beside it. */
export function archiveName(file, at) {
  const { ext } = path.parse(file);
  return `${file.slice(0, file.length - ext.length)}.${at.toISOString().replace(/[-:.]/g, "")}${ext}`;
}

/** The trail's archives (rotated files), oldest first; none when the folder is missing. */
export function auditArchives(file) {
  const { dir, name, ext } = path.parse(file);
  const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(`^${escape(name)}\\.\\d{8}T\\d{9}Z${escape(ext)}$`);
  let names;
  try {
    names = fs.readdirSync(dir);
  } catch (err) {
    if (err.code === "ENOENT") return [];
    throw err;
  }
  return names
    .filter((n) => pattern.test(n))
    .sort()
    .map((n) => path.join(dir, n));
}

function endsWithNewline(fd, size) {
  const b = Buffer.alloc(1);
  fs.readSync(fd, b, 0, 1, size - 1);
  return b[0] === NEWLINE;
}

/** The bytes of the last non-blank line of fd's first `size` bytes, read backwards in chunks; null when there is none. */
function lastLine(fd, size) {
  let pos = size;
  let tail = Buffer.alloc(0);
  while (pos > 0) {
    const n = Math.min(CHUNK, pos);
    pos -= n;
    const buf = Buffer.alloc(n);
    fs.readSync(fd, buf, 0, n, pos);
    const data = Buffer.concat([buf, tail]);
    let stop = data.length;
    while (stop > 0) {
      const nl = data.lastIndexOf(NEWLINE, stop - 1);
      if (nl < 0) break;
      const line = data.subarray(nl + 1, stop);
      if (line.toString("utf8").trim()) return Buffer.from(line);
      stop = nl;
    }
    tail = Buffer.from(data.subarray(0, stop));
  }
  return tail.toString("utf8").trim() ? tail : null;
}

function lastLineOfFile(file) {
  const fd = fs.openSync(file, "r");
  try {
    const size = fs.fstatSync(fd).size;
    return size > 0 ? lastLine(fd, size) : null;
  } finally {
    fs.closeSync(fd);
  }
}

/** AUDIT_PATH, default audit.jsonl beside the user database; off when the database is in memory. */
export function auditPath(dbPath, raw = process.env.AUDIT_PATH) {
  if (raw) return raw;
  return dbPath === ":memory:" ? null : path.join(path.dirname(dbPath), "audit.jsonl");
}
