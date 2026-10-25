// Server-side only: reads the audit trail (audit.jsonl, lib/auth/audit.js)
// for GET /api/admin/audit (#308, #249). Read-only. The file is append-only
// and can grow, so it is read backwards in chunks from the end — never loaded
// whole — and reading stops once a page is full or the lines are older than
// `from`.
import fs from "node:fs";
import { auditMatches } from "../audit";

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

/**
 * One page of the audit trail, newest first, filtered by `query` (parsed by
 * lib/audit.js parseAuditQuery). A line that is not a JSON object is skipped
 * and counted, never an error. `next_before` is the cursor of the next (older)
 * page, null when there is none. A missing file is an empty trail.
 * → { entries, skipped, next_before }
 */
export async function readAudit(file, query) {
  let fh;
  try {
    fh = await fs.promises.open(file, "r");
  } catch (err) {
    if (err.code === "ENOENT") return { entries: [], skipped: 0, next_before: null };
    throw err;
  }
  try {
    const size = (await fh.stat()).size;
    const end = query.before === null ? size : Math.min(query.before, size);
    const entries = [];
    let skipped = 0;
    let nextBefore = null;
    for await (const { offset, text } of linesFromEnd(fh, end)) {
      if (!text.trim()) continue;
      let entry;
      try {
        entry = JSON.parse(text);
      } catch {
        entry = null;
      }
      if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
        skipped++;
        continue;
      }
      // Lines are appended in time order: past `from`, nothing older can match.
      if (query.from && typeof entry.time === "string" && entry.time.slice(0, 10) < query.from) break;
      if (!auditMatches(entry, query)) continue;
      if (entries.length === query.limit) {
        // One more match exists: the next page starts below the last line listed.
        nextBefore = entries.at(-1).offset;
        break;
      }
      entries.push({ offset, entry });
    }
    return { entries: entries.map((e) => e.entry), skipped, next_before: nextBefore };
  } finally {
    await fh.close();
  }
}
