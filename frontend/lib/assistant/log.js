// Server-side only: the assistant's review log (#191, L11/L12: every AI
// output logged and reviewable), data/monitor/assistant.jsonl. Append-only:
// one "answer" line per question (question, sources given, citations, the
// model's text, what was shown and why, user, flag false) and one "flag" line
// each time someone marks an answer wrong — the answer line is never
// rewritten, so the review keeps what was shown. readAnswers folds the flags in.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { authLog } from "@/lib/auth/log";

/** ASSISTANT_LOG_PATH, default assistant.jsonl beside the user database; null when the database is in memory. */
export function assistantLogPath(dbPath, raw = process.env.ASSISTANT_LOG_PATH) {
  if (raw) return raw;
  return dbPath === ":memory:" ? null : path.join(path.dirname(dbPath), "assistant.jsonl");
}

export function newAnswerId() {
  return crypto.randomBytes(12).toString("hex");
}

/** Appends one JSON line; false (and an ERROR log) when the write fails. */
export function appendLine(file, entry) {
  try {
    fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
    fs.appendFileSync(file, `${JSON.stringify(entry)}\n`, { mode: 0o600 });
    return true;
  } catch (err) {
    authLog("ERROR", "assistant log write failed", { component: "assistant", file, error: err.message });
    return false;
  }
}

/** Every answer line by id, with flag true once a flag line names it (unreadable lines skipped). */
export function readAnswers(file) {
  const answers = new Map();
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch {
    return answers;
  }
  for (const line of text.split("\n")) {
    if (!line.trim()) continue;
    let e;
    try {
      e = JSON.parse(line);
    } catch {
      continue;
    }
    if (e.kind === "answer") answers.set(e.id, e);
    else if (e.kind === "flag" && answers.has(e.id)) answers.get(e.id).flag = true;
  }
  return answers;
}
