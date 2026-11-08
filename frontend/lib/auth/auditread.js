// Server-side only: reads the audit trail (audit.jsonl, lib/auth/audit.js)
// for GET /api/admin/audit (#308, #249), GET /api/admin/audit/verify (#619)
// and the export (#702, lib/auth/auditexport.js). Read-only. The trail is the
// archives (rotated files, oldest first) then the current file, seen as one
// sequence of bytes: a cursor is an offset into that sequence, which rotation
// never moves. The files can grow, so they are read in chunks — never loaded
// whole — and a page stops once it is full or the lines are older than `from`.
import fs from "node:fs";
import path from "node:path";
import { auditMatches } from "../audit";
import { auditArchives } from "./audit";
import { verifyChain } from "./auditchain";

const CHUNK = 64 * 1024;
const NEWLINE = 0x0a;

/** The lines of fh's bytes [0, end), last first: { offset (byte where the line starts), text }. */
async function* linesFromEnd(fh, end) {
  let pos = end;
  let tail = Buffer.alloc(0); // the start of a line whose beginning is in an earlier chunk
  while (pos > 0) {
    const size = Math.min(CHUNK, pos);
    pos -= size;
    const buf = Buffer.alloc(size);
    await fh.read(buf, 0, size, pos);
    const data = Buffer.concat([buf, tail]);
    let stop = data.length;
    // UTF-8 never uses 0x0a inside a character, so splitting bytes on it is safe.
    while (stop > 0) {
      const nl = data.lastIndexOf(NEWLINE, stop - 1);
      if (nl < 0) break;
      if (nl + 1 < stop) yield { offset: pos + nl + 1, text: data.subarray(nl + 1, stop).toString("utf8") };
      stop = nl;
    }
    tail = Buffer.from(data.subarray(0, stop));
  }
  if (tail.length > 0) yield { offset: 0, text: tail.toString("utf8") };
}

/** The lines of fh, first first: { line (1-based), bytes }, the last one even without a newline. */
async function* linesFromStart(fh) {
  let pos = 0;
  let line = 0;
  let head = Buffer.alloc(0); // the start of a line that goes on in the next chunk
  for (;;) {
    const buf = Buffer.alloc(CHUNK);
    const { bytesRead } = await fh.read(buf, 0, CHUNK, pos);
    if (bytesRead === 0) break;
    pos += bytesRead;
    const data = Buffer.concat([head, buf.subarray(0, bytesRead)]);
    let start = 0;
    for (let nl = data.indexOf(NEWLINE, start); nl >= 0; nl = data.indexOf(NEWLINE, start)) {
      line++;
      yield { line, bytes: Buffer.from(data.subarray(start, nl)) };
      start = nl + 1;
    }
    head = Buffer.from(data.subarray(start));
  }
  if (head.length > 0) yield { line: line + 1, bytes: head };
}

/** The trail's files oldest first, the current one last: { path, archive }. */
function trailFiles(file) {
  return [...auditArchives(file).map((p) => ({ path: p, archive: true })), { path: file, archive: false }];
}

/** Opens a file to read; null when it does not exist. */
async function openOrNull(p) {
  try {
    return await fs.promises.open(p, "r");
  } catch (err) {
    if (err.code === "ENOENT") return null;
    throw err;
  }
}

/** The lines of the whole trail before the trail offset `end` (null = all), last first: { offset, text }. */
async function* trailFromEnd(file, end) {
  const handles = [];
  try {
    let start = 0;
    for (const f of trailFiles(file)) {
      const fh = await openOrNull(f.path);
      if (!fh) continue;
      const size = (await fh.stat()).size;
      handles.push({ fh, start, size });
      start += size;
    }
    for (const { fh, start, size } of handles.reverse()) {
      const stop = end === null ? size : Math.min(size, end - start);
      if (stop <= 0) continue;
      for await (const { offset, text } of linesFromEnd(fh, stop)) yield { offset: start + offset, text };
    }
  } finally {
    await Promise.all(handles.map((h) => h.fh.close()));
  }
}

/** The lines of the whole trail, oldest first: { file (name), archive, line, bytes }. */
async function* trailFromStart(file) {
  for (const f of trailFiles(file)) {
    const fh = await openOrNull(f.path);
    if (!fh) continue;
    try {
      for await (const { line, bytes } of linesFromStart(fh)) yield { file: path.basename(f.path), archive: f.archive, line, bytes };
    } finally {
      await fh.close();
    }
  }
}

/** The size of the whole trail in bytes: the offset just past its last line, which later lines and rotation never move. */
export async function trailSize(file) {
  let total = 0;
  for (const f of trailFiles(file)) {
    const fh = await openOrNull(f.path);
    if (!fh) continue;
    try {
      total += (await fh.stat()).size;
    } finally {
      await fh.close();
    }
  }
  return total;
}

/**
 * The lines before the trail offset `query.before` (null = all) that pass
 * `query`, newest first: { offset, entry }, or { damaged: true } for a line
 * that is not a JSON object. Stops at the first line older than `query.from`.
 */
export async function* trailEntries(file, query) {
  for await (const { offset, text } of trailFromEnd(file, query.before)) {
    if (!text.trim()) continue;
    let entry;
    try {
      entry = JSON.parse(text);
    } catch {
      entry = null;
    }
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      yield { damaged: true };
      continue;
    }
    // Lines are appended in time order: past `from`, nothing older can match.
    if (query.from && typeof entry.time === "string" && entry.time.slice(0, 10) < query.from) return;
    if (auditMatches(entry, query)) yield { offset, entry };
  }
}

/**
 * One page of the audit trail, newest first, filtered by `query` (parsed by
 * lib/audit.js parseAuditQuery), the current file then its archives newest
 * first. A line that is not a JSON object is skipped and counted, never an
 * error. `next_before` is the cursor of the next (older) page, null when
 * there is none. A missing file is an empty trail.
 * → { entries, skipped, next_before }
 */
export async function readAudit(file, query) {
  const entries = [];
  let skipped = 0;
  let nextBefore = null;
  for await (const { offset, entry, damaged } of trailEntries(file, query)) {
    if (damaged) {
      skipped++;
      continue;
    }
    if (entries.length === query.limit) {
      // One more match exists: the next page starts below the last line listed.
      nextBefore = entries.at(-1).offset;
      break;
    }
    entries.push({ offset, entry });
  }
  return { entries: entries.map((e) => e.entry), skipped, next_before: nextBefore };
}

/** Checks the hash chain of the whole trail, archives first (lib/auth/auditchain.js verifyChain); a missing trail is intact with nothing checked. */
export async function verifyAudit(file) {
  return verifyChain(trailFromStart(file));
}
