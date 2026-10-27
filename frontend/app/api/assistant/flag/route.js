import { authDbPath, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { roleAtLeast } from "@/lib/auth/roles";
import { appendLine, assistantLogPath, readAnswers } from "@/lib/assistant/log";

export const dynamic = "force-dynamic";

const MAX_BODY = 2048;
const MAX_NOTE = 500;

/**
 * Marks an assistant answer wrong (#191): POST /api/assistant/flag {"id",
 * "note"?}. The person who asked may flag their answer, a manager or an admin
 * any answer. Appends a "flag" line to data/monitor/assistant.jsonl (the
 * answer line itself is never rewritten). 204 when stored, 404 for an id the
 * log does not hold.
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
  if (text.length > MAX_BODY) return Response.json({ error: "flag too large" }, { status: 413 });
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    body = null;
  }
  const id = typeof body?.id === "string" ? body.id : "";
  const note = body?.note ?? "";
  if (!id || typeof note !== "string" || note.length > MAX_NOTE) {
    return Response.json({ error: `body must be {"id": "…", "note": "…"?} (note up to ${MAX_NOTE} characters)` }, { status: 400 });
  }

  const answer = readAnswers(file).get(id);
  if (!answer) return Response.json({ error: "unknown answer" }, { status: 404 });
  if (answer.user !== user.username && !roleAtLeast(user.role, "manager")) {
    return Response.json({ error: "only the person who asked, a manager or an admin may flag this answer" }, { status: 403 });
  }

  const entry = { kind: "flag", id, ts: new Date().toISOString(), user: user.username, role: user.role, note: note.trim() };
  if (!appendLine(file, entry)) return Response.json({ error: "flag not stored: see the monitor's logs" }, { status: 500 });
  authLog("INFO", "assistant answer flagged", { component: "assistant", username: user.username, id });
  return new Response(null, { status: 204, headers: { "Cache-Control": "no-store" } });
}
