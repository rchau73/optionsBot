import { configuredBots } from "@/lib/bots";
import { sessionUser } from "@/lib/auth/server";
import { sessionRequired } from "@/lib/auth/tokens";

export const dynamic = "force-dynamic";

// A pass runs a few margin simulations per strangle (one a second at Deribit);
// the bot answers within its own 120 s limit.
const TIMEOUT_MS = 125000;

/**
 * Manual rebalance: POST /api/bots/<bot>/rebalance {"dry_run": true|false} →
 * <bot url>/api/rebalance?dry_run=… with BOT_ADMIN_TOKEN. The token stays on
 * the server; the bot runs its own rebalance with its own gates and journals
 * a real pass. Same-origin JSON requests only, so another site open in the
 * browser cannot trigger it, and only with a signed-in session (checked here
 * as well as in proxy.js).
 */
export async function POST(request, { params }) {
  if (!(await sessionUser(request))) return sessionRequired();
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
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    const headers = { "Content-Type": "application/json", "Cache-Control": "no-store" };
    const retryAfter = res.headers.get("retry-after");
    if (retryAfter) headers["Retry-After"] = retryAfter;
    return new Response(await res.text(), { status: res.status, headers });
  } catch {
    return Response.json({ error: `bot ${bot} did not answer: check the activity feed before retrying` }, { status: 502 });
  }
}
