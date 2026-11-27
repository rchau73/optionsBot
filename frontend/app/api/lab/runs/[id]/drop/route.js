import { readAccounts } from "@/lib/auth/accountLinks";
import { requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { compareDir, validId } from "@/lib/compare";
import { readRun } from "@/lib/lab";
import {
  buildDropRecord,
  checkDrop,
  decisionRefusal,
  DROP_AUDIT_ACTION,
  droppedAnswer,
  droppedDir,
  readRecord,
  recordsDir,
  removeRecord,
  writeRecord,
} from "@/lib/labapply";
import { queueDir, readRequests } from "@/lib/labrequests";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };

// No body is needed: anything sent is ignored, and far under this.
const MAX_BODY = 1024;

/**
 * POST /api/lab/runs/<id>/drop (#890, epic E9): a manager decides to leave the
 * setting as it is, and the server records that decision, so the lab's usage
 * report counts it beside the confirmed changes. Managers and admins,
 * same-origin JSON, the apply route's role and account-link check
 * (decisionRefusal). Only a done run with a proposal, not already dropped
 * and not confirmed (lib/labapply.js checkDrop). On success it writes
 * COMPARE_DIR/dropped/<id>.json once (the person as an 8-character hash, the
 * account, the run and the time; no setting value, no amount) and one audit
 * line (parameter_change_dropped), and answers 201 `applied: false, dropped:
 * true`. It writes no config, restarts no bot and places no order; a refusal
 * writes nothing, and an audit line that cannot be written takes the record
 * back (500).
 */
export async function POST(request, { params }) {
  const { auth, user, response } = await requireRole(request, "manager");
  if (response) return response;
  if (!sameOrigin(request)) return Response.json({ error: "cross-origin request refused" }, { status: 403 });
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) {
    return Response.json({ error: "JSON body required" }, { status: 415 });
  }
  if ((await request.text()).length > MAX_BODY) return Response.json({ error: "request too large" }, { status: 413 });
  const { id } = await params;
  if (!validId(id)) return Response.json({ error: "no such run" }, { status: 404 });
  const dir = compareDir();
  const r = readRun(dir, id);
  if (r.missing) return Response.json({ error: "no such run" }, { status: 404 });
  if (r.error) {
    // The reason is the id only, never the file's content.
    authLog("ERROR", "lab run unreadable", { component: "lab", username: user.username, id });
    return Response.json({ error: "the run cannot be read: see the monitor's logs" }, { status: 500 });
  }
  const problem = decisionRefusal(auth, user, r.run, readAccounts);
  if (problem) return Response.json({ error: problem }, { status: 403 });

  // From here to writeRecord nothing awaits: the monitor is one Node process, so an apply of the same run
  // cannot run between this check of applied/ and the write to dropped/ (the apply route keeps the same rule).
  const now = Date.now();
  const dropped = droppedDir(dir);
  const checked = checkDrop({ run: r.run, now, requests: readRequests(queueDir(dir)), recorded: readRecord(recordsDir(dir), id), dropped: readRecord(dropped, id) });
  if (checked.status) {
    authLog("WARN", "lab drop refused", { component: "lab", username: user.username, id, status: checked.status, reason: checked.body.error });
    return Response.json(checked.body, { status: checked.status, headers: NO_STORE });
  }

  const { record, line } = buildDropRecord(checked.preview, user, now);
  try {
    writeRecord(dropped, record);
  } catch (err) {
    if (err?.code === "EEXIST") {
      authLog("WARN", "lab drop refused", { component: "lab", username: user.username, id, status: 409, reason: "already dropped" });
      return Response.json({ error: "this run's proposal was already dropped: a drop is recorded once", applied: false, dropped: false }, { status: 409, headers: NO_STORE });
    }
    authLog("ERROR", "lab drop not recorded", { component: "lab", username: user.username, id });
    return Response.json({ error: "the drop could not be recorded: see the monitor's logs", applied: false, dropped: false }, { status: 500, headers: NO_STORE });
  }
  try {
    auth.recordAction(user, DROP_AUDIT_ACTION, {
      run_id: id,
      underlying: record.account.underlying,
      config: record.account.config,
      applied: false,
      line,
    });
  } catch {
    // A drop never stands without its audit line: take it back, so it can be retried.
    removeRecord(dropped, id);
    authLog("ERROR", "lab drop not recorded: audit line failed", { component: "lab", username: user.username, id });
    return Response.json({ error: "the drop could not be recorded: see the monitor's logs", applied: false, dropped: false }, { status: 500, headers: NO_STORE });
  }
  authLog("INFO", "lab proposal dropped", { component: "lab", username: user.username, id });
  return Response.json(droppedAnswer(record, line), { status: 201, headers: NO_STORE });
}
