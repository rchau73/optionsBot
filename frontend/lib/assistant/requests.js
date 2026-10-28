// Server-side only: the assistant's private request queue (#409, E10).
// data/monitor/requests.jsonl holds one line per request a person sent from
// a draft card (never a draft nobody sent); data/monitor/requests_status.jsonl
// holds the review's lines (written by the PM agent: { id, status, note?,
// issue_url?, ts }), the latest line of an id winning. Both are append-only,
// gitignored (data/) and in the nightly backup; nothing here reaches GitHub —
// the repository is public, and filing a public issue is a reviewer's step.
import fs from "node:fs";
import path from "node:path";
import { STATUSES } from "@/lib/assistantRequests";

const ENVIRONMENTS = ["stable", "mvp"];

/** ASSISTANT_REQUESTS_PATH, default requests.jsonl beside the user database; null when the database is in memory. */
export function requestsPath(dbPath, raw = process.env.ASSISTANT_REQUESTS_PATH) {
  if (raw) return raw;
  return dbPath === ":memory:" ? null : path.join(path.dirname(dbPath), "requests.jsonl");
}

/** The review's status file, beside the queue. */
export function statusPath(requestsFile) {
  return path.join(path.dirname(requestsFile), "requests_status.jsonl");
}

/** The JSON objects of a JSONL file, oldest first; [] when it does not exist (unreadable lines skipped). */
export function readLines(file) {
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch {
    return [];
  }
  const out = [];
  for (const line of text.split("\n")) {
    if (!line.trim()) continue;
    try {
      const e = JSON.parse(line);
      if (e && typeof e === "object") out.push(e);
    } catch {
      // a torn line: skipped
    }
  }
  return out;
}

/** The next id: "R-0001" for an empty queue, else one more than the highest. */
export function nextRequestId(requests) {
  let max = 0;
  for (const r of requests) {
    const m = /^R-(\d+)$/.exec(r.id ?? "");
    if (m) max = Math.max(max, Number(m[1]));
  }
  return `R-${String(max + 1).padStart(4, "0")}`;
}

/** Requests the person sent on the UTC day of nowMs. */
export function sentToday(requests, username, nowMs) {
  const day = new Date(nowMs).toISOString().slice(0, 10);
  return requests.filter((r) => r.user === username && String(r.ts ?? "").startsWith(day)).length;
}

/** The environment the monitor runs in (MONITOR_DEPLOYMENT), when well-formed; else null. */
export function environmentOf(env = process.env) {
  return ENVIRONMENTS.includes(env.MONITOR_DEPLOYMENT) ? env.MONITOR_DEPLOYMENT : null;
}

/** An issue link the review may give: https only, so a status line cannot carry a script URL. */
function issueUrl(v) {
  return typeof v === "string" && /^https:\/\/[^\s]+$/.test(v) ? v : null;
}

/**
 * The person's own requests for My requests, newest first: { id, ts, kind,
 * title, status, note, issue_url } — the queue's lines merged with the
 * review's latest status line for each (in_review without one; a status
 * not in STATUSES is ignored).
 */
export function myRequests(requests, statuses, username) {
  const latest = new Map();
  for (const s of statuses) {
    if (typeof s.id === "string" && STATUSES.includes(s.status)) latest.set(s.id, s);
  }
  return requests
    .filter((r) => r.user === username)
    .map((r) => {
      const s = latest.get(r.id);
      return {
        id: r.id,
        ts: r.ts,
        kind: r.kind,
        title: r.fields?.title ?? "",
        status: s?.status ?? "in_review",
        note: typeof s?.note === "string" ? s.note : null,
        issue_url: issueUrl(s?.issue_url),
      };
    })
    .reverse();
}
