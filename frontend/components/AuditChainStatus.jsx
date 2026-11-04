"use client";

import { useEffect, useState } from "react";
import { fetchAuditChain } from "@/lib/api/admin";
import { auditChainSummary } from "@/lib/audit";
import { formatDateTime } from "@/lib/format";

const BUTTON =
  "rounded border border-line px-3 py-1 hover:bg-line/40 disabled:opacity-40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/**
 * The Audit page's chain line (#619, under the filters): whether the audit
 * trail's hash chain is intact, or the first line where it breaks, with
 * "Check again". Read-only; a broken chain hides no line of the trail below.
 */
export default function AuditChainStatus() {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  const [round, setRound] = useState(0); // bumped by "Check again"

  useEffect(() => {
    const ctrl = new AbortController();
    fetchAuditChain(ctrl.signal).then(setResult, (err) => {
      if (!ctrl.signal.aborted) setError(err.message);
    });
    return () => ctrl.abort();
  }, [round]);

  function again() {
    setResult(null);
    setError(null);
    setRound(round + 1);
  }

  const summary = auditChainSummary(result, formatDateTime);
  if (result?.off) return null; // the trail below already says it is off

  const checking = !result && !error;
  let line;
  if (error) {
    line = (
      <p role="alert" className="text-loss">
        The chain could not be checked: {error}
      </p>
    );
  } else if (checking) {
    line = <p className="text-muted">Checking the audit chain…</p>;
  } else {
    line = (
      <p role={summary.broken ? "alert" : "status"} className={summary.broken ? "font-semibold text-loss" : "text-profit"}>
        <span aria-hidden="true">{summary.broken ? "✗ " : "✓ "}</span>
        {summary.text}
        {summary.legacy ? <span className="text-muted"> {summary.legacy}</span> : null}
      </p>
    );
  }

  return (
    <div data-testid="audit-chain" className="flex flex-wrap items-center gap-3 rounded border border-line bg-panel px-3 py-2 text-sm">
      <div className="min-w-0 flex-1">{line}</div>
      <button type="button" className={BUTTON} disabled={checking} onClick={again}>
        Check again
      </button>
    </div>
  );
}
