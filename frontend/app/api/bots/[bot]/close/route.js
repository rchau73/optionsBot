import { configuredBots } from "@/lib/bots";
import { requireRole } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

// A market buy-back takes seconds; the bot answers within its own 60 s limit.
const TIMEOUT_MS = 65000;
const MAX_IDS = 20;

/**
 * Manual close: POST /api/bots/<bot>/close {"position_ids": [...]} →
 * <bot url>/api/positions/close with BOT_ADMIN_TOKEN. The token stays on the
 * server; the bot decides whether each position may be closed (regime-side
 * block only) and journals it. Same-origin JSON requests only, so another
 * site open in the browser cannot trigger it, and only by a manager or an
 * admin, re-read from the user database (#171: a demoted or deleted user, or
 * one signed out everywhere, is refused at once). The bot journals the person
 * as "actor"; the monitor audits the request in data/monitor/audit.jsonl.
 */
export async function POST(request, { params }) {
  const { auth, user, response } = await requireRole(request, "manager");
  if (response) return response;
  const { bot } = await params;
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return Response.json({ error: "unknown bot" }, { status: 404 });

  const token = process.env.BOT_ADMIN_TOKEN;
  if (!token) return Response.json({ error: "manual close is off: BOT_ADMIN_TOKEN is not set on the monitor" }, { status: 503 });

  const origin = request.headers.get("origin");
  const host = request.headers.get("host");
  if (!origin || !host || new URL(origin).host !== host) {
    return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  }
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }

  let ids;
  try {
    ids = (await request.json()).position_ids;
  } catch {
    ids = null;
  }
  if (!Array.isArray(ids) || ids.length === 0 || ids.length > MAX_IDS || !ids.every((id) => typeof id === "string" && id.length > 0 && id.length < 100)) {
    return Response.json({ error: `position_ids: 1 to ${MAX_IDS} ids` }, { status: 400 });
  }

  try {
    const res = await fetch(`${target.url}/api/positions/close`, {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify({ position_ids: ids, actor: { user: user.username, role: user.role } }),
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    const text = await res.text();
    auth.recordAction(user, "manual_close", { bot, position_ids: ids, status: res.status, results: closeResults(text) });
    return new Response(text, {
      status: res.status,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
    });
  } catch {
    auth.recordAction(user, "manual_close", { bot, position_ids: ids, status: 502 });
    return Response.json({ error: `bot ${bot} did not answer: check the activity feed before retrying` }, { status: 502 });
  }
}

/** What the bot said it closed (instrument, qty, filled, error) for the audit line; null when unreadable. */
function closeResults(text) {
  try {
    return JSON.parse(text).results?.map(({ instrument, qty, filled, error }) => ({ instrument, qty, filled, error })) ?? null;
  } catch {
    return null;
  }
}
