import { authDbPath, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { appendLine, assistantLogPath, readAnswers } from "@/lib/assistant/log";
import { OUTCOME_ANSWERED } from "@/lib/assistant/service";

export const dynamic = "force-dynamic";

const MAX_BODY = 512;
// One choice per answer, changed once (#579): at most two feedback lines per
// answer, so the writes stay bounded by the 30 questions an hour. The check
// reads then appends, not atomically: two clicks racing on one answer can
// leave a third line. Accepted — the review counts the last line per answer,
// so the figures stay right and the writes stay small.
const MAX_FEEDBACK_LINES = 2;

/**
 * "Did this help?" under an assistant answer (#579): POST
 * /api/assistant/feedback {"id", "helped": true|false}. Only the person who
 * asked, only on a reply of outcome "answered" (an "I don't know", a refusal,
 * the fallback and a request draft have their own paths). Appends a
 * "feedback" line to data/monitor/assistant.jsonl — the id, the time, the
 * person and the choice, no text. 204 when stored; 404 unknown id; 409 not an
 * answer, or the choice already changed once.
 */
export async function POST(request) {
  const { user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  const file = assistantLogPath(authDbPath());
  if (!file) return Response.json({ error: "the assistant is off: no log file (ASSISTANT_LOG_PATH)" }, { status: 503 });

  const text = await request.text();
  if (text.length > MAX_BODY) return Response.json({ error: "feedback too large" }, { status: 413 });
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    body = null;
  }
  const id = typeof body?.id === "string" ? body.id : "";
  if (!id || typeof body?.helped !== "boolean") {
    return Response.json({ error: 'body must be {"id": "…", "helped": true|false}' }, { status: 400 });
  }

  const answer = readAnswers(file).get(id);
  if (!answer) return Response.json({ error: "unknown answer" }, { status: 404 });
  if (answer.user !== user.username) {
    return Response.json({ error: "only the person who asked may say whether this answer helped" }, { status: 403 });
  }
  if (answer.outcome !== OUTCOME_ANSWERED) {
    return Response.json({ error: "this reply takes no feedback" }, { status: 409 });
  }
  if (answer.feedback.length >= MAX_FEEDBACK_LINES) {
    return Response.json({ error: "your choice was already changed once" }, { status: 409 });
  }

  const entry = { kind: "feedback", id, ts: new Date().toISOString(), user: user.username, role: user.role, helped: body.helped };
  if (!appendLine(file, entry)) return Response.json({ error: "feedback not stored: see the monitor's logs" }, { status: 500 });
  authLog("INFO", "assistant answer feedback", { component: "assistant", username: user.username, id, helped: body.helped });
  return new Response(null, { status: 204, headers: { "Cache-Control": "no-store" } });
}
