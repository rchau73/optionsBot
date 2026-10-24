// Server-side only: what every /api/admin/users route checks first (#306).
import { requireRole, sameOrigin } from "./server";

/**
 * The admin behind a request, re-read from the database like close and
 * rebalance (a manager gets 403, an ended session 401). A state-changing
 * request (`write`) must also be same-origin JSON; its body is parsed
 * ({} when unreadable). `id` is the user id from the URL: a positive integer,
 * else 404. → { auth, user, body, id } or { response } to return as is.
 */
export async function adminRequest(request, { write = false, params = null } = {}) {
  const { auth, user, response } = await requireRole(request, "admin");
  if (response) return { response };

  let id = null;
  if (params) {
    const raw = (await params).id;
    id = /^[1-9][0-9]{0,15}$/.test(raw ?? "") ? Number(raw) : null;
    if (id === null) return { response: Response.json({ error: "no such user" }, { status: 404 }) };
  }
  if (!write) return { auth, user, id, body: null };

  if (!sameOrigin(request)) return { response: Response.json({ error: "cross-origin request refused" }, { status: 403 }) };
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return { response: Response.json({ error: "JSON body required" }, { status: 415 }) };
  }
  let body;
  try {
    body = await request.json();
  } catch {
    body = null;
  }
  return { auth, user, id, body: body && typeof body === "object" ? body : {} };
}

/** The answer of an AuthService result: 200 with `ok` fields, else its status and reason. */
export function serviceAnswer(result, okBody = {}) {
  if (!result.ok) return Response.json({ error: result.error }, { status: result.status });
  return Response.json(okBody, { headers: { "Cache-Control": "no-store" } });
}
