// The audit trail's hash chain (#619), pure. Every line written by
// lib/auth/audit.js carries two fields after the entry's own:
//   prev       SHA-256 (hex) of the previous line's exact bytes, "" for the first line of the trail;
//   line_hash  SHA-256 of the line itself up to and without `,"line_hash":…`.
// prev links each line to the one before (a removed, inserted or reordered line
// breaks the next link); line_hash names an edited line itself (and the last
// line, which no later prev covers). Both are hashes of audit lines, never of a
// password or a token. A chain shows tampering; it cannot stop someone who can
// rewrite the whole volume from rewriting the chain (docs/operations.md).
import crypto from "node:crypto";

/** The fields the chain adds to a line; the Audit page leaves them out of "detail". */
export const CHAIN_FIELDS = ["prev", "line_hash"];

const SEAL = /,"line_hash":"([0-9a-f]{64})"\}$/;

/** SHA-256 of a string (as UTF-8) or of bytes, hex. */
export function sha256(data) {
  return crypto.createHash("sha256").update(data).digest("hex");
}

/** The audit line of `entry` (no newline) linked to the previous line's hash `prev` ("" for the first). */
export function sealLine(entry, prev) {
  const { prev: _prev, line_hash: _hash, ...fields } = entry;
  const body = JSON.stringify({ ...fields, prev });
  return `${body.slice(0, -1)},"line_hash":"${sha256(body)}"}`;
}

/** True when a line's line_hash matches the rest of the line. */
export function sealIntact(text) {
  const m = SEAL.exec(text);
  return m !== null && sha256(`${text.slice(0, m.index)}}`) === m[1];
}

function parseObject(text) {
  try {
    const v = JSON.parse(text);
    return v && typeof v === "object" && !Array.isArray(v) ? v : null;
  } catch {
    return null;
  }
}

/**
 * Checks a trail given as its lines oldest first (an iterable or async
 * iterable of { file, line (1-based in its file), bytes, archive }), one line
 * at a time. Lines before the first one with a `prev` field are legacy
 * (written before #619): counted, not checked. From there every line must be
 * JSON with `prev` = the hash of the line before and an intact `line_hash`;
 * blank lines are ignored. Stops at the first break.
 * → { ok, checked, legacy, legacy_before (time of the first chained line when
 *     legacy lines exist, else null), first_break: null | { file, archive, line, time, reason } }
 * reason: "link" (the line before was removed, reordered or edited), "edited"
 * (this line was changed), "damaged" (not a JSON object), "unchained" (no prev
 * after the chain started).
 */
export async function verifyChain(lines) {
  let prevBytes = null;
  let started = false;
  let checked = 0;
  let legacy = 0;
  let legacyBefore = null;
  for await (const { file, line, bytes, archive = false } of lines) {
    const text = bytes.toString("utf8");
    if (!text.trim()) continue;
    const entry = parseObject(text);
    const chained = entry !== null && "prev" in entry;
    if (!started && !chained) {
      legacy++;
      prevBytes = bytes;
      continue;
    }
    const time = typeof entry?.time === "string" ? entry.time : null;
    if (!started) {
      started = true;
      if (legacy > 0) legacyBefore = time;
    }
    const expected = prevBytes === null ? "" : sha256(prevBytes);
    let reason = null;
    if (entry === null) reason = "damaged";
    else if (!chained) reason = "unchained";
    else if (entry.prev !== expected) reason = "link";
    else if (!sealIntact(text)) reason = "edited";
    if (reason) {
      return { ok: false, checked, legacy, legacy_before: legacyBefore, first_break: { file, archive, line, time, reason } };
    }
    checked++;
    prevBytes = bytes;
  }
  return { ok: true, checked, legacy, legacy_before: legacyBefore, first_break: null };
}
