import { authDbPath, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { appendLine, assistantLogPath, readAnswers } from "@/lib/assistant/log";
import { environmentOf, myRequests, nextRequestId, readLines, requestsPath, sentToday, statusPath } from "@/lib/assistant/requests";
import { KINDS, MAX_ITEMS, REQUESTS_PER_DAY, requestFields } from "@/lib/assistantRequests";

export const dynamic = "force-dynamic";

const MAX_BODY = 16384;
const NO_STORE = { "Cache-Control": "no-store" };
const OFF = "requests are off: no queue file (ASSISTANT_REQUESTS_PATH)";

/** The citations that point at the bot's own records (journal entries, P&L history points). */
const isRecordRef = (c) => c.startsWith("journal:") || c.startsWith("pnl:");

/**
 * My requests (#409): GET /api/assistant/requests → { requests: [{ id, ts,
 * kind, title, status, note, issue_url }], per_day } — the signed-in
 * person's own requests only, newest first, with the review's status from
 * requests_status.jsonl.
 */
export async function GET(request) {
  const { user, response } = await requireRole(request, "user");
  if (response) return response;
  const file = requestsPath(authDbPath());
  if (!file) return Response.json({ error: OFF }, { status: 503, headers: NO_STORE });
  const requests = myRequests(readLines(file), readLines(statusPath(file)), user.username);
  return Response.json({ requests, per_day: REQUESTS_PER_DAY }, { headers: NO_STORE });
}

/**
 * Sends a draft for review (#409): POST /api/assistant/requests
 * {"answer_id", "kind", "fields"} — the person's own click on a draft card,
 * never the assistant. The answer must be the person's own and carry a draft
 * in the review log; the citations come from that log line, not from the
 * browser (an edited evidence item keeps its logged source). Appends one line
 * to the private queue data/monitor/requests.jsonl (id R-NNNN, time, user,
 * role, environment, kind, fields, citations, journal refs, answer id) — never
 * to GitHub. Signed in, same origin, REQUESTS_PER_DAY a UTC day per person
 * (429), one request per draft (409). 201 { id, status: "in_review" }.
 */
export async function POST(request) {
  const { user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  const file = requestsPath(authDbPath());
  const log = assistantLogPath(authDbPath());
  if (!file || !log) return Response.json({ error: OFF }, { status: 503 });

  const text = await request.text();
  if (text.length > MAX_BODY) return Response.json({ error: "request too large" }, { status: 413 });
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    body = null;
  }
  const answerId = typeof body?.answer_id === "string" ? body.answer_id : "";
  const kind = body?.kind;
  if (!answerId || !KINDS.includes(kind) || typeof body?.fields !== "object" || body.fields === null) {
    return Response.json({ error: 'body must be {"answer_id": "…", "kind": "bug"|"enhancement", "fields": {…}}' }, { status: 400 });
  }
  const answer = readAnswers(log).get(answerId);
  const draft = answer?.request?.draft;
  if (!answer || answer.user !== user.username || !draft) {
    return Response.json({ error: "no draft of yours with this answer id" }, { status: 404 });
  }
  const fields = requestFields(kind, body.fields);
  if (!fields.title || !fields.what) return Response.json({ error: "a request needs a title and a what" }, { status: 400 });
  if ((body.fields.evidence?.length ?? 0) > MAX_ITEMS || (body.fields.steps?.length ?? 0) > MAX_ITEMS) {
    return Response.json({ error: `at most ${MAX_ITEMS} evidence items and steps` }, { status: 400 });
  }
  const logged = new Set((draft.evidence ?? []).map((e) => e.citation));
  if (fields.evidence.some((e) => !logged.has(e.citation))) {
    return Response.json({ error: "evidence must keep the sources the draft cited" }, { status: 400 });
  }

  const requests = readLines(file);
  if (requests.some((r) => r.answer_id === answerId)) {
    return Response.json({ error: "this draft was already sent" }, { status: 409 });
  }
  const now = Date.now();
  if (sentToday(requests, user.username, now) >= REQUESTS_PER_DAY) {
    return Response.json({ error: `${REQUESTS_PER_DAY} requests a day: try again tomorrow (UTC)` }, { status: 429, headers: NO_STORE });
  }
  const citations = [...logged];
  const entry = {
    id: nextRequestId(requests),
    ts: new Date(now).toISOString(),
    user: user.username,
    role: user.role,
    environment: environmentOf(),
    kind,
    fields,
    citations,
    journal_refs: citations.filter(isRecordRef),
    answer_id: answerId,
  };
  if (!appendLine(file, entry)) return Response.json({ error: "request not stored: see the monitor's logs" }, { status: 500 });
  // Ids only: the fields stay in the private queue.
  authLog("INFO", "assistant request sent", { component: "assistant", username: user.username, id: entry.id, kind, answer: answerId });
  return Response.json({ id: entry.id, status: "in_review" }, { status: 201, headers: NO_STORE });
}
