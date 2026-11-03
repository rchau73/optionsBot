import { requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { validateProposal } from "@/lib/labvalidate";

export const dynamic = "force-dynamic";

// A key, a value and an underlying: far under this.
const MAX_BODY = 2048;

/**
 * POST /api/lab/validate {"key", "value", "underlying"?} (#586, epic E9): may
 * this value be tried, and who must approve it — the range, step and unit
 * from the Go catalogue, the cross rules Validate enforces against that
 * underlying's shipped config, the core-policy flag, the tip and the current
 * value (lib/labvalidate.js, content/lab-settings.json). Managers and admins,
 * same-origin JSON. It writes and applies nothing; the audit line
 * (lab_validate) names the key and the verdict, never the value.
 */
export async function POST(request) {
  const { auth, user, response } = await requireRole(request, "manager");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  const raw = await request.text();
  if (raw.length > MAX_BODY) return Response.json({ error: "request too large" }, { status: 413 });
  let body;
  try {
    body = JSON.parse(raw);
  } catch {
    body = null;
  }
  const { status, body: answer } = validateProposal(body);
  if (status !== 200) {
    authLog("WARN", "lab validate refused", { component: "lab", username: user.username, status });
    return Response.json(answer, { status, headers: { "Cache-Control": "no-store" } });
  }
  authLog("INFO", "lab value checked", { component: "lab", username: user.username, key: answer.key, valid: answer.valid });
  auth.recordAction(user, "lab_validate", { key: answer.key, underlying: answer.underlying, valid: answer.valid });
  return Response.json(answer, { headers: { "Cache-Control": "no-store" } });
}
