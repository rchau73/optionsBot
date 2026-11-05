import { requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { compareDir } from "@/lib/compare";
import { checkRequest, createRequest, MAX_OPEN_PER_PERSON, openCount, queueDir, readRequests } from "@/lib/labrequests";

export const dynamic = "force-dynamic";

// Five keys, values and an underlying: far under this.
const MAX_BODY = 4096;

/**
 * POST /api/lab/requests {"settings": [{"key", "value"}], "underlying"?}
 * (#641, epic E9): ask for a comparison run of the shipped config against
 * these values. Each value is checked like POST /api/lab/validate; a refused
 * one returns the reason and queues nothing. A valid request is written to
 * the private queue (lib/labrequests.js) with status queued, for the runner
 * (scripts/lab_runner.sh); GET /api/lab/runs lists it with its status.
 * Managers and admins, same-origin JSON, at most MAX_OPEN_PER_PERSON waiting
 * per person. It writes no config and applies nothing; the audit line
 * (lab_request) names the person, the request and the keys, never the values.
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
  const noStore = { "Cache-Control": "no-store" };
  const checked = checkRequest(body);
  if (checked.status) {
    authLog("WARN", "lab request refused", { component: "lab", username: user.username, status: checked.status, key: checked.body.key });
    return Response.json({ ...checked.body, queued: false }, { status: checked.status, headers: noStore });
  }
  const dir = queueDir(compareDir());
  if (openCount(readRequests(dir), user.username) >= MAX_OPEN_PER_PERSON) {
    authLog("WARN", "lab request refused", { component: "lab", username: user.username, status: 429 });
    return Response.json(
      { error: `you already have ${MAX_OPEN_PER_PERSON} requests waiting or running: wait for one to finish`, queued: false },
      { status: 429, headers: noStore },
    );
  }
  let queued;
  try {
    queued = createRequest(dir, user.username, checked);
  } catch {
    authLog("ERROR", "lab request not written", { component: "lab", username: user.username });
    return Response.json({ error: "the request could not be queued: see the monitor's logs", queued: false }, { status: 500, headers: noStore });
  }
  const keys = queued.settings.map((s) => s.key);
  authLog("INFO", "lab run requested", { component: "lab", username: user.username, id: queued.id, keys });
  auth.recordAction(user, "lab_request", { id: queued.id, underlying: queued.underlying, keys });
  return Response.json({ ...queued, queued: true }, { status: 201, headers: noStore });
}
