import { authDbPath, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { assistantLogPath } from "@/lib/assistant/log";
import { QUESTIONS_PER_HOUR } from "@/lib/assistant/limit";
import { assistantIndex, assistantLimiter, assistantModel, botExplain, botJournal } from "@/lib/assistant/server";
import { answerQuestion, mergePassages, validQuestion } from "@/lib/assistant/service";

export const dynamic = "force-dynamic";

const MAX_BODY = 2048;
const NO_STORE = { "Cache-Control": "no-store" };

/** Why the assistant is off in this environment, or null when it is on. */
function offReason() {
  if (!assistantModel()) return "the assistant is off: ANTHROPIC_API_KEY is not set";
  if (!assistantLogPath(authDbPath())) return "the assistant is off: no log file (ASSISTANT_LOG_PATH)";
  return null;
}

/**
 * The assistant panel's state (#408): GET /api/assistant → { enabled,
 * remaining, per_hour } for the signed-in person; 503 { enabled: false,
 * error } while the assistant is off, so the Ask button can say so before
 * anyone asks. Counts nothing.
 */
export async function GET(request) {
  const { user, response } = await requireRole(request, "user");
  if (response) return response;
  const off = offReason();
  if (off) return Response.json({ enabled: false, error: off }, { status: 503, headers: NO_STORE });
  const remaining = assistantLimiter().remaining(user.id, Date.now());
  return Response.json({ enabled: true, remaining, per_hour: QUESTIONS_PER_HOUR }, { headers: NO_STORE });
}

/**
 * The help assistant (#191, E10): POST /api/assistant {"question", "bot"?,
 * "seq"?} from a signed-in person (any role, re-read from the user database;
 * 401 without a session). Answers from the help index and, with "bot", that
 * bot's latest journal facts; with "seq" too (the panel's "Why?" on a trade,
 * #408) also the facts the journal recorded about that event
 * (GET /api/explain). The answer must cite them, else it is replaced by
 * "I don't know" (no citation) or the fallback reply (advice, forecast, a
 * number from nowhere). Every question is logged in data/monitor/assistant.jsonl.
 * Off (503) until ANTHROPIC_API_KEY is in the monitor's .env; a person may
 * ask QUESTIONS_PER_HOUR an hour (429 with Retry-After). Every answer carries
 * `remaining`, the questions left this hour.
 */
export async function POST(request) {
  const { user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }

  const off = offReason();
  if (off) return Response.json({ error: off }, { status: 503 });
  const model = assistantModel();
  const file = assistantLogPath(authDbPath());

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
  const seq = body.seq ?? null;
  if (seq !== null && (bot === null || !Number.isSafeInteger(seq) || seq <= 0)) {
    return Response.json({ error: "seq must be a journal sequence number, with bot" }, { status: 400 });
  }

  const limiter = assistantLimiter();
  const wait = limiter.take(user.id, Date.now());
  if (wait > 0) {
    return Response.json(
      { error: "too many questions: try again later", retry_after: wait, remaining: 0 },
      { status: 429, headers: { "Retry-After": String(wait), ...NO_STORE } },
    );
  }
  const remaining = limiter.remaining(user.id, Date.now());

  let journal = [];
  if (bot !== null) {
    journal = await botJournal(bot);
    if (journal === null) return Response.json({ error: "unknown bot" }, { status: 404 });
    if (seq !== null) journal = mergePassages(await botExplain(bot, seq), journal);
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
    seq,
    outcome: out.outcome ?? null,
    status,
  });
  return Response.json({ ...out, remaining }, { status, headers: NO_STORE });
}
