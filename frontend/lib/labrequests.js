// The lab's run requests (#641, epic E9): a manager asks for a comparison of
// the shipped config against proposed values. Each value is checked like
// POST /api/lab/validate (lib/labvalidate.js); a valid request becomes one
// small file in a private queue (<COMPARE_DIR>/requests/<id>.json, the shape
// of tests/testdata/lab_request.json), which the runner (cmd/labrun,
// scripts/lab_runner.sh) takes one at a time. Nothing here writes a config
// or applies a setting.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { catalogueKey, validateProposal } from "@/lib/labvalidate";

/** The statuses a request goes through (internal/compare/requests.go). */
export const STATUS = { queued: "queued", running: "running", done: "done", failed: "failed", expired: "expired" };

/** At most this many values in one request. */
export const MAX_SETTINGS = 5;

/** At most this many requests of one person waiting or running. */
export const MAX_OPEN_PER_PERSON = 3;

/** A request still queued after this long is expired (compare.RequestMaxAge). */
export const MAX_AGE_MS = 24 * 3600 * 1000;

/** At most this many requests listed beside the runs. */
export const REQUEST_LIST_LIMIT = 50;

// A YAML key as in the comparison's Differences: dte_delta_matrix[0].dte, or [] for every element.
const KEY = /^[a-z0-9_]+(\[\d*\])*(\.[a-z0-9_]+(\[\d*\])*)*$/;
// A request id: its UTC time and a random suffix.
const ID = /^\d{8}T\d{6}Z-[0-9a-f]{8}$/;

/** The queue folder inside the comparison reports' folder. */
export const queueDir = (compareDir) => path.join(compareDir, "requests");

/** 2026-11-04T23:00:00.000Z → 20261104T230000Z. */
const compactTime = (date) => date.toISOString().replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z");

/**
 * Checks {underlying?, settings: [{key, value}, …]}: { status, body } when it
 * is refused (the reason in `error`, and `reasons` for a value outside its
 * rules), else { underlying, settings }. Every value goes through
 * validateProposal, a list index ("[0]") read as the catalogue's "[]".
 */
export function checkRequest(body) {
  const settings = body?.settings;
  if (!body || typeof body !== "object" || !Array.isArray(settings) || settings.length === 0) {
    return { status: 400, body: { error: 'send {"settings": [{"key": "<setting>", "value": <proposed value>}], "underlying": "BTC"}' } };
  }
  if (settings.length > MAX_SETTINGS) return { status: 400, body: { error: `at most ${MAX_SETTINGS} values in one request` } };
  const seen = new Set();
  let underlying;
  for (const s of settings) {
    if (!s || typeof s.key !== "string" || !KEY.test(s.key) || !("value" in s)) {
      return { status: 400, body: { error: 'each setting is {"key": "<setting>", "value": <proposed value>}' } };
    }
    if (seen.has(s.key)) return { status: 400, body: { error: `${s.key} is listed twice`, key: s.key } };
    seen.add(s.key);
    const { status, body: answer } = validateProposal({ key: catalogueKey(s.key), value: s.value, underlying: body.underlying });
    if (status !== 200) return { status, body: { error: answer.error, key: s.key } };
    if (!answer.valid) {
      return { status: 400, body: { error: `${s.key}: ${answer.reasons.join("; ")}`, key: s.key, reasons: answer.reasons } };
    }
    underlying = answer.underlying;
  }
  return { underlying, settings: settings.map((s) => ({ key: s.key, value: s.value })) };
}

/** A request's status as the list shows it: queued past the age limit is expired, whatever the runner has written yet. */
export function effectiveStatus(r, now = Date.now()) {
  return r.status === STATUS.queued && now - Date.parse(r.requested_at) > MAX_AGE_MS ? STATUS.expired : r.status;
}

/** Every request of `dir`, newest first; a file that does not parse is skipped. A missing dir is an empty queue. */
export function readRequests(dir) {
  let names;
  try {
    names = fs.readdirSync(dir);
  } catch {
    return [];
  }
  const out = [];
  for (const name of names.sort().reverse()) {
    const id = name.replace(/\.json$/, "");
    if (!name.endsWith(".json") || !ID.test(id)) continue;
    try {
      const r = JSON.parse(fs.readFileSync(path.join(dir, name), "utf8"));
      if (r.id === id) out.push(r);
    } catch {
      // half-written by another process or not a request: left out
    }
  }
  return out;
}

/** How many of `username`'s requests are waiting or running. */
export function openCount(requests, username, now = Date.now()) {
  return requests.filter((r) => r.requester === username && [STATUS.queued, STATUS.running].includes(effectiveStatus(r, now))).length;
}

/**
 * Writes a new queued request (through a temporary file and a rename, so the
 * runner never reads half of it) and returns it.
 */
export function createRequest(dir, username, { underlying, settings }, now = new Date()) {
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  const time = now.toISOString();
  const request = {
    id: `${compactTime(now)}-${crypto.randomBytes(4).toString("hex")}`,
    requester: username,
    requested_at: time,
    underlying,
    settings,
    status: STATUS.queued,
    updated_at: time,
  };
  const file = path.join(dir, `${request.id}.json`);
  fs.writeFileSync(`${file}.tmp`, `${JSON.stringify(request, null, 2)}\n`, { mode: 0o600, flag: "wx" });
  fs.renameSync(`${file}.tmp`, file);
  return request;
}

/**
 * The requests that have no report listed yet (`runIds`: the ids of the runs
 * list), newest first, with the status the list shows: queued, running,
 * failed (with its reason), expired, or done while its report is not
 * readable yet. Values and usernames only, never an amount.
 */
export function pendingRequests(requests, runIds, now = Date.now()) {
  const listed = new Set(runIds);
  return requests
    .filter((r) => !(r.status === STATUS.done && listed.has(r.run_id)))
    .slice(0, REQUEST_LIST_LIMIT)
    .map((r) => ({
      id: r.id,
      requested_at: r.requested_at,
      requester: r.requester,
      underlying: r.underlying,
      settings: r.settings,
      status: effectiveStatus(r, now),
      updated_at: r.updated_at,
      run_id: r.run_id ?? null,
      reason: r.reason ?? null,
    }));
}
