// The admin reports: usage and delivery analytics, the audit trail (admins only).

import { apiFetch, getJSON, jsonOrNull } from "./http.js";

/**
 * The weekly usage report of an environment (admins only, #445) → the
 * route's body; a week without a report answers 404 with the same fields, so
 * that body is returned with `missing: true` instead of an error.
 */
export async function fetchUsageReport(env, week, signal) {
  const q = new URLSearchParams({ env });
  if (week) q.set("week", week);
  const res = await apiFetch(`/api/admin/analytics?${q}`, { cache: "no-store", signal });
  const body = await jsonOrNull(res);
  if (res.status === 404 && Array.isArray(body?.weeks)) return { ...body, missing: true };
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
  return body;
}

/** The delivery (DORA) report (admins only, #473) → { report (null before cmd/dora's first run), file, written_at }. */
export async function fetchDeliveryReport(signal) {
  return getJSON("/api/admin/analytics/delivery", signal);
}

/** One page of the audit trail (admins only) → { off, entries, skipped, next_before }; `query` from lib/audit.js auditQueryString. */
export async function fetchAudit(query, signal) {
  return getJSON(`/api/admin/audit${query ? `?${query}` : ""}`, signal);
}

/**
 * The lines matching `query` as a CSV file (admins only, #702) →
 * { blob, filename, lines, cut, skipped }; throws with the server's reason.
 */
export async function downloadAudit(query) {
  const res = await apiFetch(`/api/admin/audit/export${query ? `?${query}` : ""}`, { cache: "no-store" });
  if (!res.ok) throw new Error((await jsonOrNull(res))?.error ?? `${res.status}`);
  return {
    blob: await res.blob(),
    filename: res.headers.get("content-disposition")?.match(/filename="(.+)"/)?.[1] ?? "audit-trail.csv",
    lines: Number(res.headers.get("x-audit-lines")),
    cut: res.headers.get("x-audit-cut") === "true",
    skipped: Number(res.headers.get("x-audit-skipped")),
  };
}

/** The audit trail's hash-chain check (admins only, #619) → { off, ok, checked, legacy, legacy_before, first_break }. */
export async function fetchAuditChain(signal) {
  return getJSON("/api/admin/audit/verify", signal);
}
