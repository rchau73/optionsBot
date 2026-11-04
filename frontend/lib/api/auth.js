// The signed-in person's session and password (/api/auth/...).

import { apiFetch, getJSON, navigation, sendJSON } from "./http.js";

/** Ends the session (its refresh token stops working) and goes to the sign-in page. */
export async function signOut() {
  try {
    await apiFetch("/api/auth/logout", { method: "POST", cache: "no-store" });
  } finally {
    navigation.assign("/login");
  }
}

/**
 * Changes the signed-in person's password; every other session of theirs ends,
 * this browser stays signed in. Throws with the server's reason.
 */
export async function changePassword(current, next) {
  await sendJSON("/api/auth/password", "POST", { current, next });
}

/** The signed-in person's live sessions (#584): [{ id, browser, created_at, last_used_at, current }]. */
export async function listSessions(signal) {
  return (await getJSON("/api/auth/sessions", signal)).sessions;
}

/**
 * Ends one of the person's other sessions (#584); its next refresh fails.
 * Throws with the server's reason (e.g. the session in use).
 */
export async function endSession(id) {
  await sendJSON(`/api/auth/sessions/${encodeURIComponent(id)}`, "DELETE", {});
}

/**
 * Deletes the signed-in person's own account (#543): its sessions, analytics
 * events and survey answers; both cookies are cleared. Throws with the
 * server's reason (e.g. the last admin).
 */
export async function deleteAccount() {
  return sendJSON("/api/auth/account", "DELETE", {});
}

/**
 * Sets a new password with a reset link (no session needed, so plain fetch);
 * every session of that person ends. Throws with the server's reason.
 */
export async function resetWithLink(token, next) {
  const res = await fetch("/api/auth/reset", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token, next }),
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    /* body was not JSON */
  }
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
}
