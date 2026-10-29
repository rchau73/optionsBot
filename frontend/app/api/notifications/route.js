import { requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { botParam } from "@/lib/menu";
import { ITEM_ID, MAX_READ_IDS, OPTIONAL_KINDS, RETENTION_DAYS } from "@/lib/notifications";
import { collectItems, notificationsView } from "@/lib/notificationsServer";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };
const ACTIONS = ["read", "read_all", "setting"];

const botOf = (request) => botParam(new URL(request.url).searchParams.get("bot"));

/**
 * GET /api/notifications?bot=<bot> → { items, needs_you, settings, errors }
 * (#447): the signed-in person's bell — Needs you (counted in needs_you) and
 * Updates (never counted), from the last 90 days, each with read from their
 * marks in auth.db. Every role gets every kind. errors.bot_events names a
 * bot whose events could not be read; the other items are still listed.
 */
export async function GET(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  const view = await notificationsView({ store: auth.store, user, bot: botOf(request), nowMs: Date.now() });
  return Response.json(view, { headers: NO_STORE });
}

/** Why a POST body is refused, or null. */
function bodyProblem(body) {
  if (!body || typeof body !== "object" || Array.isArray(body) || !ACTIONS.includes(body.action)) {
    return `body: {"action": one of ${ACTIONS.join(", ")}}`;
  }
  if (body.action === "read") {
    const ids = body.ids;
    if (!Array.isArray(ids) || ids.length === 0 || ids.length > MAX_READ_IDS || !ids.every((id) => typeof id === "string" && ITEM_ID.test(id))) {
      return `ids: 1 to ${MAX_READ_IDS} notification ids`;
    }
  }
  if (body.action === "setting") {
    if (!OPTIONAL_KINDS.includes(body.kind)) return `kind: one of ${OPTIONAL_KINDS.join(", ")} (Needs you cannot be turned off)`;
    if (typeof body.enabled !== "boolean") return "enabled: true or false";
  }
  return null;
}

/**
 * POST /api/notifications?bot=<bot> → the view above, after one of:
 * {"action": "read", "ids": [...]} (an item opened or followed),
 * {"action": "read_all"} (every item of the list as it is now, the page's
 * bot's included) or {"action": "setting", "kind", "enabled"} (an Updates
 * kind on or off; a Needs you kind is refused). Signed in, same-origin JSON.
 */
export async function POST(request) {
  const { auth, user, response } = await requireRole(request, "user");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  let body;
  try {
    body = await request.json();
  } catch {
    body = null;
  }
  const problem = bodyProblem(body);
  if (problem) return Response.json({ error: problem }, { status: 400 });

  const nowMs = Date.now();
  const bot = botOf(request);
  if (body.action === "read") auth.store.markNotificationsRead(user.id, body.ids, RETENTION_DAYS, nowMs);
  if (body.action === "read_all") {
    const { items } = await collectItems({ store: auth.store, user, bot, nowMs });
    auth.store.markNotificationsRead(user.id, items.map((i) => i.id), RETENTION_DAYS, nowMs);
  }
  if (body.action === "setting") auth.store.setNotificationSetting(user.id, body.kind, body.enabled);
  // Kinds and counts only: never an item's text.
  authLog("INFO", "notifications state", { component: "notifications", username: user.username, action: body.action, kind: body.kind ?? null });
  return Response.json(await notificationsView({ store: auth.store, user, bot, nowMs }), { headers: NO_STORE });
}
