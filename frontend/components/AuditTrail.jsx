"use client";

import { useEffect, useState } from "react";
import Panel from "./Panel";
import { fetchAudit } from "@/lib/api/admin";
import { AUDIT_ACTIONS, auditDetail, auditQueryString } from "@/lib/audit";
import { formatDateTime } from "@/lib/format";

const FIELD = "rounded border border-line bg-panel px-2 py-1 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const BUTTON =
  "rounded border border-line px-3 py-1 hover:bg-line/40 disabled:opacity-40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const EMPTY = { user: "", action: "", from: "", to: "" };

/**
 * The Audit page (#308, #249; admins only — GET /api/admin/audit enforces
 * it): who did what, newest first, filtered by person, action and UTC day,
 * 100 lines at a time with "Older" for the next page. Read-only.
 */
export default function AuditTrail() {
  const [form, setForm] = useState(EMPTY); // the filters being typed
  const [filters, setFilters] = useState(EMPTY); // the filters applied
  const [page, setPage] = useState(null); // { off, entries, skipped, next_before }
  const [error, setError] = useState(null);
  const [loadingMore, setLoadingMore] = useState(false);

  useEffect(() => {
    const ctrl = new AbortController();
    fetchAudit(auditQueryString(filters), ctrl.signal).then(setPage, (err) => {
      if (!ctrl.signal.aborted) setError(err.message);
    });
    return () => ctrl.abort();
  }, [filters]);

  async function older() {
    setLoadingMore(true);
    try {
      const next = await fetchAudit(auditQueryString({ ...filters, before: page.next_before }));
      setPage({ ...next, entries: [...page.entries, ...next.entries], skipped: page.skipped + next.skipped });
    } catch (err) {
      setError(err.message);
    } finally {
      setLoadingMore(false);
    }
  }

  const set = (field) => (e) => setForm({ ...form, [field]: e.target.value });

  /** Shows "loading" and asks for the first page of `next` filters. */
  function apply(next) {
    setPage(null);
    setError(null);
    setFilters({ ...next });
  }

  return (
    <div className="space-y-4">
      <Panel title="Filters">
        <form
          className="flex flex-wrap items-end gap-3 text-sm"
          onSubmit={(e) => {
            e.preventDefault();
            apply(form);
          }}
        >
          <label className="flex flex-col">
            <span className="text-muted">Person</span>
            <input value={form.user} onChange={set("user")} placeholder="username" className={FIELD} />
          </label>
          <label className="flex flex-col">
            <span className="text-muted">Action</span>
            <select value={form.action} onChange={set("action")} className={FIELD}>
              <option value="">any</option>
              {AUDIT_ACTIONS.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col">
            <span className="text-muted">From (UTC day)</span>
            <input type="date" value={form.from} onChange={set("from")} className={FIELD} />
          </label>
          <label className="flex flex-col">
            <span className="text-muted">To (UTC day)</span>
            <input type="date" value={form.to} onChange={set("to")} className={FIELD} />
          </label>
          <button type="submit" className={BUTTON}>
            Apply
          </button>
          <button
            type="button"
            className={BUTTON}
            onClick={() => {
              setForm(EMPTY);
              apply(EMPTY);
            }}
          >
            Clear
          </button>
        </form>
      </Panel>

      <Panel title="Audit trail" right={page && !page.off ? <span className="text-xs text-muted">{page.entries.length} shown</span> : null}>
        {error ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            The audit trail could not be loaded: {error}
          </p>
        ) : null}
        {!page && !error ? <p className="text-muted">Loading the audit trail…</p> : null}
        {page?.off ? (
          <p role="status" className="rounded bg-warn/15 px-3 py-2 text-warn">
            The audit trail is off: this monitor keeps its users in memory, so nothing is recorded.
          </p>
        ) : null}
        {page && !page.off ? (
          <>
            {page.skipped > 0 ? (
              <p role="status" className="mb-2 rounded bg-warn/15 px-3 py-1 text-warn">
                {page.skipped} damaged {page.skipped === 1 ? "line was" : "lines were"} skipped.
              </p>
            ) : null}
            {page.entries.length === 0 ? (
              <p className="text-muted">No audit lines match these filters.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="text-xs uppercase text-muted">
                    <tr>
                      <th className="px-2 py-1">Time</th>
                      <th className="px-2 py-1">Person</th>
                      <th className="px-2 py-1">Role</th>
                      <th className="px-2 py-1">Action</th>
                      <th className="px-2 py-1">Target / bot</th>
                      <th className="px-2 py-1">Detail</th>
                    </tr>
                  </thead>
                  <tbody>
                    {page.entries.map((e, i) => (
                      <tr key={i} className="border-t border-line align-top" data-testid="audit-row">
                        <td className="whitespace-nowrap px-2 py-1 tabular-nums text-muted">{formatDateTime(e.time)}</td>
                        <td className="px-2 py-1 font-semibold">{e.username ?? "—"}</td>
                        <td className="px-2 py-1">{e.role ?? "—"}</td>
                        <td className="px-2 py-1 font-mono text-xs">{e.action ?? "—"}</td>
                        <td className="px-2 py-1">{e.target ?? e.bot ?? "—"}</td>
                        <td className="break-all px-2 py-1 font-mono text-xs text-muted">{auditDetail(e) || "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {page.next_before !== null ? (
              <button type="button" className={`${BUTTON} mt-3`} disabled={loadingMore} onClick={older}>
                {loadingMore ? "Loading…" : "Older"}
              </button>
            ) : null}
          </>
        ) : null}
      </Panel>
    </div>
  );
}
