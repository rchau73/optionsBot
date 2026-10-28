"use client";

import clsx from "clsx";
import { useEffect, useState } from "react";
import { fetchAssistantRequests } from "@/lib/api";
import { KIND_LABELS, statusLabel } from "@/lib/assistantRequests";
import { formatDateTime } from "@/lib/format";

// My requests (#409): the requests the signed-in person sent from a draft
// card, newest first, with the review's status and note — their own only
// (the route filters). Read when the tab opens.

const STATUS_CLASS = {
  in_review: "text-testnet",
  filed: "text-testnet",
  fixed: "text-profit",
  not_a_bug: "text-muted",
  duplicate: "text-muted",
  needs_info: "text-warn",
};

export default function MyRequests() {
  const [state, setState] = useState({ loading: true, error: null, requests: [] });

  useEffect(() => {
    const controller = new AbortController();
    fetchAssistantRequests(controller.signal)
      .then((requests) => setState({ loading: false, error: null, requests }))
      .catch((e) => {
        if (!controller.signal.aborted) setState({ loading: false, error: e.message, requests: [] });
      });
    return () => controller.abort();
  }, []);

  if (state.loading) return <p className="text-sm text-muted">Loading your requests…</p>;
  if (state.error) {
    return (
      <p role="alert" className="text-sm text-warn">
        Your requests could not be read: {state.error}.
      </p>
    );
  }
  if (!state.requests.length) {
    return <p className="text-sm text-muted">No requests yet. When the assistant drafts one from your report, you send it from the conversation.</p>;
  }
  return (
    <ul aria-label="My requests" className="space-y-2">
      {state.requests.map((r) => (
        <li key={r.id} className="space-y-1 rounded-md border border-line px-3 py-2 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs">{r.id}</span>
            <span className="rounded-full bg-slate-800 px-2 py-0.5 text-xs">{KIND_LABELS[r.kind] ?? r.kind}</span>
            <span className={clsx("ml-auto text-xs font-semibold", STATUS_CLASS[r.status] ?? "text-testnet")}>{statusLabel(r.status)}</span>
          </div>
          <p>{r.title}</p>
          <p className="text-xs text-muted">Sent {formatDateTime(r.ts)}</p>
          {r.status === "filed" && r.issue_url ? (
            <a href={r.issue_url} target="_blank" rel="noreferrer" className="text-xs text-testnet underline">
              The public issue
            </a>
          ) : null}
          {r.note ? <p className="text-xs">Reviewer: {r.note}</p> : null}
        </li>
      ))}
    </ul>
  );
}
