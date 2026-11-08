// Server-side only: the audit trail as a CSV stream, for
// GET /api/admin/audit/export (#702). Read-only here: the route writes the
// audit_exported line. The cut must be known before the first byte, because
// the page reads it from the response's headers, so the export goes over the
// trail twice: once counting, once streaming. Both stop at the same trail
// offset, so they see the same lines and the export's own audit line is never
// in its file.
import { AUDIT_CSV_COLUMNS, AUDIT_EXPORT_MAX, auditCsvRow } from "../audit";
import { trailEntries, trailSize } from "./auditread";

/**
 * Counts what an export of `query` (lib/audit.js parseAuditQuery) will hold.
 * → { end (the trail offset both passes stop at), lines, cut, skipped }
 */
export async function planAuditExport(file, query, max = AUDIT_EXPORT_MAX) {
  const end = await trailSize(file);
  let lines = 0;
  let skipped = 0;
  let cut = false;
  for await (const { damaged } of trailEntries(file, { ...query, before: end })) {
    if (damaged) {
      skipped++;
    } else if (lines === max) {
      cut = true;
      break;
    } else {
      lines++;
    }
  }
  return { end, lines, cut, skipped };
}

/** The CSV rows of the plan's lines, newest first. */
async function* csvRows(file, query, { end, lines }) {
  let left = lines;
  for await (const { entry, damaged } of trailEntries(file, { ...query, before: end })) {
    if (damaged) continue;
    if (left === 0) return;
    left--;
    yield auditCsvRow(entry);
  }
}

/**
 * The CSV file of a plan as a ReadableStream: the header at once, each row when
 * the reader wants it, so no file is opened before then and a slow reader holds
 * one chunk of the trail, never all of it. `onDone({ lines, complete, error })`
 * runs once: at the end, or with complete false when the reader left or a read
 * failed, `lines` being the rows sent by then.
 */
export function auditCsvStream(file, query, plan, onDone) {
  const encoder = new TextEncoder();
  const rows = csvRows(file, query, plan);
  let sent = 0;
  let done = false;
  const finish = (complete, error = null) => {
    if (done) return;
    done = true;
    onDone({ lines: sent, complete, error });
  };
  return new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode(`${AUDIT_CSV_COLUMNS.join(",")}\r\n`));
    },
    async pull(controller) {
      try {
        const next = await rows.next();
        if (next.done) {
          finish(true);
          controller.close();
          return;
        }
        controller.enqueue(encoder.encode(next.value));
        sent++;
      } catch (err) {
        finish(false, err);
        controller.error(err);
      }
    },
    async cancel() {
      finish(false);
      await rows.return();
    },
  });
}
