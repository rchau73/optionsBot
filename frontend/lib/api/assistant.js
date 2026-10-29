// The assistant: its state, questions, flags and requests (/api/assistant...).

import { apiFetch, jsonOrNull, sendJSON } from "./http.js";

/**
 * The assistant's state for the signed-in person (#408): { status, enabled,
 * remaining, error }. Never throws: a failure is { status: 0, enabled: null }.
 */
export async function fetchAssistantState(signal) {
  try {
    const res = await apiFetch("/api/assistant", { cache: "no-store", signal });
    const body = await jsonOrNull(res);
    return { status: res.status, enabled: res.ok ? true : res.status === 503 ? false : null, remaining: body?.remaining ?? null, error: body?.error ?? null };
  } catch {
    return { status: 0, enabled: null, remaining: null, error: null };
  }
}

/**
 * Asks the assistant (#191, #408): { question, bot?, seq? } → { status, body,
 * retryAfter } with the route's status as is (200, 429, 503, …), so the panel
 * shows each state. Never throws: a network failure is status 0.
 */
export async function askAssistant({ question, bot = null, seq = null }) {
  const payload = { question };
  if (bot) payload.bot = bot;
  if (bot && seq != null) payload.seq = seq;
  try {
    const res = await apiFetch("/api/assistant", {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const body = (await jsonOrNull(res)) ?? {};
    const retry = Number(res.headers?.get?.("retry-after") ?? body.retry_after);
    return { status: res.status, body, retryAfter: Number.isFinite(retry) && retry > 0 ? retry : null };
  } catch {
    return { status: 0, body: {}, retryAfter: null };
  }
}

/** Flags an assistant answer as wrong; throws with the server's reason. */
export async function flagAssistantAnswer(id) {
  await sendJSON("/api/assistant/flag", "POST", { id });
}

/**
 * Sends a draft request for review (#409): { answerId, kind, fields } →
 * { id, status }. Only the draft card's Send calls it; throws with the
 * server's reason (limit, already sent, …).
 */
export async function sendAssistantRequest({ answerId, kind, fields }) {
  return sendJSON("/api/assistant/requests", "POST", { answer_id: answerId, kind, fields });
}

/** The signed-in person's own requests, newest first (#409); throws with the server's reason. */
export async function fetchAssistantRequests(signal) {
  const res = await apiFetch("/api/assistant/requests", { cache: "no-store", signal });
  const body = await jsonOrNull(res);
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
  return body?.requests ?? [];
}
