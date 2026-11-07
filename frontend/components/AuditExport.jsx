"use client";

import { useState } from "react";
import { downloadAudit } from "@/lib/api/admin";
import { auditExportSummary, auditQueryString } from "@/lib/audit";

const BUTTON =
  "rounded border border-line px-3 py-1 hover:bg-line/40 disabled:opacity-40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const OFF_NOTE = "audit-export-off";
const REVOKE_AFTER_MS = 1000; // a browser reads the file after the click returns

/** Hands a downloaded file to the browser. */
function saveFile(blob, filename) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), REVOKE_AFTER_MS);
}

/**
 * Export CSV on the Audit page (#702): downloads the lines of the filters
 * applied and says how many, or that the cap cut the file. `loaded`: the list
 * has answered; `off`: the trail is off, so there is nothing to export.
 */
export default function AuditExport({ filters, loaded, off }) {
  const [exporting, setExporting] = useState(false);
  const [summary, setSummary] = useState(null);
  const [error, setError] = useState(null);

  async function exportCsv() {
    setExporting(true);
    setSummary(null);
    setError(null);
    try {
      const { blob, filename, ...result } = await downloadAudit(auditQueryString(filters));
      saveFile(blob, filename);
      setSummary(auditExportSummary(result));
    } catch (err) {
      setError(err.message);
    } finally {
      setExporting(false);
    }
  }

  return (
    <>
      <button type="button" className={BUTTON} disabled={!loaded || off || exporting} aria-describedby={off ? OFF_NOTE : undefined} onClick={exportCsv}>
        {exporting ? "Exporting…" : "Export CSV"}
      </button>
      {off ? (
        <p id={OFF_NOTE} className="w-full text-muted">
          Export is not available: the trail is off.
        </p>
      ) : null}
      {summary ? (
        <p role="status" className={summary.cut ? "w-full rounded bg-warn/15 px-3 py-1 text-warn" : "w-full text-muted"}>
          {summary.text}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="w-full rounded bg-loss/15 px-2 py-1 text-loss">
          The export failed: {error}
        </p>
      ) : null}
    </>
  );
}
