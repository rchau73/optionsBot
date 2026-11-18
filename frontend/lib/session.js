// Browser side of the sign-in page (the monitor's other calls go through
// lib/api/http.js apiFetch, which refreshes the session by itself).
import { refreshOutcome } from "./api/http";
import { idleSignOutMessage } from "./auth/idle";

/** Signs in; resolves to { username, role }, throws with the server's message. */
export async function signIn(username, password) {
  const res = await fetch("/api/auth/login", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    /* body was not JSON */
  }
  if (!res.ok) throw new Error(body?.error ?? `sign-in failed (${res.status})`);
  return body.user;
}

/**
 * → { ok: true } when a live refresh cookie gave a new access token (no password
 * needed), else { ok: false, idle? } (idle: the limit in hours of a session this
 * refresh ended for inactivity, #821).
 */
export async function tryRefresh() {
  try {
    return await refreshOutcome(await fetch("/api/auth/refresh", { method: "POST", cache: "no-store" }));
  } catch {
    return { ok: false };
  }
}

/** The login page's inactivity sentence from its query string (`idle`, set by apiFetch), or null. */
export function signedOutMessage(search) {
  return idleSignOutMessage(new URLSearchParams(search).get("idle"));
}

/** The local page to open after signing in, from the login page's ?next= (never another site). */
export function loginNext(search) {
  const raw = new URLSearchParams(search).get("next");
  if (typeof raw !== "string" || !raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "/";
  if (raw.startsWith("/login") || raw.startsWith("/api/")) return "/";
  return raw;
}
