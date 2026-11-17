import { configuredBots } from "@/lib/bots";
import { refuseOtherAccount, requireRole } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

// The preview reads the exchange (a position, the account and one margin
// simulation); the switch-off closes the perpetual at market. The bot answers
// within its own 60 s limit.
const READ_TIMEOUT_MS = 25000;
const SWITCH_TIMEOUT_MS = 65000;

/**
 * The futures hedge experiment (#657), GET: /api/bots/<bot>/hedge[?preview=true]
 * → <bot url>/api/hedge — the mode, the perpetual, the benchmark and, with
 * preview, what switching off would do. Read-only; its own route (not the
 * generic proxy) because the preview reads the exchange and needs longer.
 */
export async function GET(request, { params }) {
  const { bot } = await params;
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return Response.json({ error: "unknown bot" }, { status: 404 });
  const preview = new URL(request.url).searchParams.get("preview") === "true";
  try {
    const res = await fetch(`${target.url}/api/hedge${preview ? "?preview=true" : ""}`, {
      cache: "no-store",
      signal: AbortSignal.timeout(READ_TIMEOUT_MS),
    });
    return new Response(await res.text(), {
      status: res.status,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
    });
  } catch {
    return Response.json({ error: `bot ${bot} is unreachable` }, { status: 502 });
  }
}

/**
 * POST /api/bots/<bot>/hedge {"enabled": true|false} → <bot url>/api/hedge
 * with BOT_ADMIN_TOKEN. Admins only, re-read from the user database (#171);
 * same-origin JSON requests only. The bot refuses it on a live account,
 * closes the perpetual at market before switching off (and stays on if that
 * fails) and journals the person; the monitor audits it as futures_hedge_switched.
 */
export async function POST(request, { params }) {
  const { auth, user, response } = await requireRole(request, "admin");
  if (response) return response;
  const { bot } = await params;
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return Response.json({ error: "unknown bot" }, { status: 404 });
  const otherAccount = refuseOtherAccount(auth, user, bot);
  if (otherAccount) return otherAccount;

  const token = process.env.BOT_ADMIN_TOKEN;
  if (!token) return Response.json({ error: "the futures hedge switch is off: BOT_ADMIN_TOKEN is not set on the monitor" }, { status: 503 });

  const origin = request.headers.get("origin");
  const host = request.headers.get("host");
  if (!origin || !host || new URL(origin).host !== host) {
    return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  }
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }

  let enabled;
  try {
    enabled = (await request.json()).enabled;
  } catch {
    enabled = null;
  }
  if (typeof enabled !== "boolean") {
    return Response.json({ error: 'body must be {"enabled": true|false}' }, { status: 400 });
  }

  try {
    const res = await fetch(`${target.url}/api/hedge`, {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify({ enabled, actor: { user: user.username, role: user.role } }),
      signal: AbortSignal.timeout(SWITCH_TIMEOUT_MS),
    });
    const text = await res.text();
    auth.recordAction(user, "futures_hedge_switched", { bot, enabled, status: res.status });
    return new Response(text, {
      status: res.status,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
    });
  } catch {
    auth.recordAction(user, "futures_hedge_switched", { bot, enabled, status: 502 });
    return Response.json({ error: `bot ${bot} did not answer: check the activity feed before retrying` }, { status: 502 });
  }
}
