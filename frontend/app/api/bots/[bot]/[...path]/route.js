import { ALLOWED_ENDPOINTS, configuredBots } from "@/lib/bots";

export const dynamic = "force-dynamic";

const TIMEOUT_MS = 2000;

/**
 * Read-only proxy: GET /api/bots/<bot>/<endpoint> → <bot url>/api/<endpoint>.
 * Only allow-listed GET endpoints are forwarded, so the browser never reaches
 * a bot directly and nothing here can change trading state.
 */
export async function GET(request, { params }) {
  const { bot, path } = await params;
  const target = configuredBots().find((b) => b.name === bot);
  const endpoint = path?.join("/");
  if (!target || !ALLOWED_ENDPOINTS.includes(endpoint)) {
    return Response.json({ error: "unknown bot or endpoint" }, { status: 404 });
  }

  const search = new URL(request.url).search;
  try {
    const res = await fetch(`${target.url}/api/${endpoint}${search}`, {
      cache: "no-store",
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    return new Response(await res.text(), {
      status: res.status,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
    });
  } catch {
    return Response.json({ error: `bot ${bot} is unreachable` }, { status: 502 });
  }
}
