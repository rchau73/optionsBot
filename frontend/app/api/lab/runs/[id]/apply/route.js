import { refuseOtherAccount, requireRole, sameOrigin } from "@/lib/auth/server";
import { authLog } from "@/lib/auth/log";
import { authKey } from "@/lib/auth/tokens";
import { compareDir, validId } from "@/lib/compare";
import { readRun } from "@/lib/lab";
import { buildRecord, checkConfirm, readRecord, RECORD_AUDIT_ACTION, recordedAnswer, recordsDir, removeRecord, writeRecord } from "@/lib/labapply";
import { queueDir, readRequests } from "@/lib/labrequests";

export const dynamic = "force-dynamic";

const NO_STORE = { "Cache-Control": "no-store" };

// A confirm token and nothing else: far under this.
const MAX_BODY = 1024;

/**
 * POST /api/lab/runs/<id>/apply {"confirm_token"} (#741, epic E9): a manager
 * confirms the change the run's preview showed. The server recomputes the
 * preview now and refuses a token that does not match it (another run,
 * person or value) or is older than 15 minutes, a value out of range, a core
 * policy (the product owner approves those), a run not done and a run already
 * recorded (lib/labapply.js). On success it writes the record
 * (COMPARE_DIR/applied/<id>.json) and one audit line
 * (parameter_change_recorded) and answers 201 `applied: false, recorded: true`:
 * it writes no config, restarts no bot and places no order. Managers and
 * admins, same-origin JSON; a manager linked to other accounts than the run's
 * bot is refused (#760); a refusal writes no record and no audit line, and
 * an audit line that cannot be written takes the record back (500).
 */
export async function POST(request, { params }) {
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
  // The run's account is its underlying's bot ("BTC" → bot btc): a manager linked to other accounts is refused (#760).
  const otherAccount = refuseOtherAccount(auth, user, String(r.run.report?.underlying ?? "").toLowerCase());
  if (otherAccount) return otherAccount;

  const now = Date.now();
  const records = recordsDir(dir);
  const checked = checkConfirm({
    run: r.run,
    person: user.username,
    token: body?.confirm_token,
    key: authKey(),
    now,
    requests: readRequests(queueDir(dir)),
    recorded: readRecord(records, id),
  });
  if (checked.status) {
    authLog("WARN", "lab confirm refused", { component: "lab", username: user.username, id, status: checked.status, reason: checked.body.error });
    return Response.json(checked.body, { status: checked.status, headers: NO_STORE });
  }

  const record = buildRecord(checked.preview, user, body.confirm_token, now);
  try {
    writeRecord(records, record);
  } catch (err) {
    if (err?.code === "EEXIST") {
      authLog("WARN", "lab confirm refused", { component: "lab", username: user.username, id, status: 409, reason: "already recorded" });
      return Response.json({ error: "this run's change was already recorded: a confirm is used once", applied: false, recorded: false }, { status: 409, headers: NO_STORE });
    }
    authLog("ERROR", "lab change not recorded", { component: "lab", username: user.username, id });
    return Response.json({ error: "the change could not be recorded: see the monitor's logs", applied: false, recorded: false }, { status: 500, headers: NO_STORE });
  }
  try {
    auth.recordAction(user, RECORD_AUDIT_ACTION, {
      run_id: id,
      underlying: record.account.underlying,
      config: record.account.config,
      changes: record.changes.map((c) => ({ key: c.key, old: c.old, new: c.new })),
      token_id: record.token_id,
      applied: false,
      line: record.line,
    });
  } catch {
    // A record never stands without its audit line: take it back, so the run is not marked recorded and the manager can confirm again.
    removeRecord(records, id);
    authLog("ERROR", "lab change not recorded: audit line failed", { component: "lab", username: user.username, id, token_id: record.token_id });
    return Response.json({ error: "the change could not be recorded: see the monitor's logs", applied: false, recorded: false }, { status: 500, headers: NO_STORE });
  }
  authLog("INFO", "lab change recorded", { component: "lab", username: user.username, id, keys: record.changes.map((c) => c.key), token_id: record.token_id });
  return Response.json(recordedAnswer(record, checked.preview), { status: 201, headers: NO_STORE });
}
