import { configuredBots } from "@/lib/bots";
import { requireRole } from "@/lib/auth/server";

export const dynamic = "force-dynamic";

// A pass runs a few margin simulations per strangle (one a second at Deribit);
// the bot answers within its own 120 s limit.
const TIMEOUT_MS = 125000;

/**
 * Manual rebalance: POST /api/bots/<bot>/rebalance {"dry_run": true|false} →
 * <bot url>/api/rebalance?dry_run=… with BOT_ADMIN_TOKEN. The token stays on
 * the server; the bot runs its own rebalance with its own gates and journals
 * a real pass. Same-origin JSON requests only, so another site open in the
 * browser cannot trigger it, and only by a manager or an admin, re-read from
 * the user database (#171), dry run included. The bot journals the person as
 * "actor"; the monitor audits the request in data/monitor/audit.jsonl.
 */
export async function POST(request, { params }) {
  const { auth, user, response } = await requireRole(request, "manager");
  if (response) return response;
  const { bot } = await params;
  const target = configuredBots().find((b) => b.name === bot);
  if (!target) return Response.json({ error: "unknown bot" }, { status: 404 });

  const token = process.env.BOT_ADMIN_TOKEN;
  if (!token) return Response.json({ error: "manual rebalance is off: BOT_ADMIN_TOKEN is not set on the monitor" }, { status: 503 });

  const origin = request.headers.get("origin");
  const host = request.headers.get("host");
  if (!origin || !host || new URL(origin).host !== host) {
    return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  }
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }

  let dryRun;
  try {
    dryRun = (await request.json()).dry_run;
  } catch {
    dryRun = null;
  }
  if (typeof dryRun !== "boolean") {
    return Response.json({ error: 'body must be {"dry_run": true|false}' }, { status: 400 });
  }

  try {
    const res = await fetch(`${target.url}/api/rebalance?dry_run=${dryRun}`, {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify({ actor: { user: user.username, role: user.role } }),
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    auth.recordAction(user, "manual_rebalance", { bot, dry_run: dryRun, status: res.status });
    const headers = { "Content-Type": "application/json", "Cache-Control": "no-store" };
    const retryAfter = res.headers.get("retry-after");
    if (retryAfter) headers["Retry-After"] = retryAfter;
    return new Response(await res.text(), { status: res.status, headers });
  } catch {
    auth.recordAction(user, "manual_rebalance", { bot, dry_run: dryRun, status: 502 });
    return Response.json({ error: `bot ${bot} did not answer: check the activity feed before retrying` }, { status: 502 });
  }
}
