import { authDbPath, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { assistantLogPath } from "@/lib/assistant/log";
import { assistantIndex, assistantLimiter, assistantModel, botJournal } from "@/lib/assistant/server";
import { answerQuestion, validQuestion } from "@/lib/assistant/service";

export const dynamic = "force-dynamic";

const MAX_BODY = 2048;

/**
 * The help assistant (#191, E10): POST /api/assistant {"question", "bot"?}
 * from a signed-in person (any role, re-read from the user database; 401
 * without a session). Answers from the help index and, with "bot", that bot's
 * latest journal facts; the answer must cite them, else it is replaced by
 * "I don't know" (no citation) or the fallback reply (advice, forecast, a
 * number from nowhere). Every question is logged in data/monitor/assistant.jsonl.
 * Off (503) until ANTHROPIC_API_KEY is in the monitor's .env; a person may
 * ask QUESTIONS_PER_HOUR an hour (429). No UI yet.
 */
export async function POST(request) {
  const { user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }

  const model = assistantModel();
  const file = assistantLogPath(authDbPath());
  if (!model) return Response.json({ error: "the assistant is off: ANTHROPIC_API_KEY is not set" }, { status: 503 });
  if (!file) return Response.json({ error: "the assistant is off: no log file (ASSISTANT_LOG_PATH)" }, { status: 503 });

  const text = await request.text();
  if (text.length > MAX_BODY) return Response.json({ error: "question too large" }, { status: 413 });
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    body = null;
  }
  const valid = validQuestion(body);
  if (valid.error) return Response.json({ error: valid.error }, { status: 400 });
  const bot = body.bot ?? null;
  if (bot !== null && typeof bot !== "string") return Response.json({ error: "bot must be a bot's name" }, { status: 400 });

  const wait = assistantLimiter().take(user.id, Date.now());
  if (wait > 0) {
    return Response.json({ error: "too many questions: try again later" }, { status: 429, headers: { "Retry-After": String(wait) } });
  }

  let journal = [];
  if (bot !== null) {
    journal = await botJournal(bot);
    if (journal === null) return Response.json({ error: "unknown bot" }, { status: 404 });
  }

  const { status, body: out } = await answerQuestion({
    question: valid.question,
    bot,
    journal,
    user,
    model,
    index: assistantIndex(),
    file,
  });
  // Ids and outcomes only: the question and the answer are in the review log.
  authLog(status === 200 ? "INFO" : "WARN", "assistant question", {
    component: "assistant",
    username: user.username,
    id: out.id ?? null,
    bot,
    outcome: out.outcome ?? null,
    status,
  });
  return Response.json(out, { status, headers: { "Cache-Control": "no-store" } });
}
