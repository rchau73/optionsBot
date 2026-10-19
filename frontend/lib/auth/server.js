// Server-side only: the monitor's one AuthService (database opened once,
// first admin created on first use) and the helpers the routes share.
import path from "node:path";
import { AuthService } from "./service";
import { AuthStore } from "./store";
import { auditPath, fileAudit } from "./audit";
import { ACCESS_COOKIE, authKey, readCookie, roleRequired, sessionRequired } from "./tokens";

/** AUTH_DB_PATH, default data/monitor/auth.db (a volume of its own in compose). */
export function authDbPath(raw = process.env.AUTH_DB_PATH) {
  return raw || path.join(process.cwd(), "data", "monitor", "auth.db");
}

// Kept on globalThis so dev-mode reloads reuse one database handle.
const KEY = Symbol.for("optionsbot.monitor.auth");

/** The shared AuthService; rejects when AUTH_SECRET is missing or short. */
export function getAuth() {
  globalThis[KEY] ??= (async () => {
    const db = authDbPath();
    const service = new AuthService({ store: await AuthStore.open(db), key: authKey(), audit: fileAudit(auditPath(db)) });
    await service.bootstrapAdmin();
    return service;
  })().catch((err) => {
    globalThis[KEY] = undefined;
    throw err;
  });
  return globalThis[KEY];
}

/** Drops the shared instance (tests). */
export function resetAuth() {
  globalThis[KEY] = undefined;
}

/** The client's IP as Next's server forwards it (first X-Forwarded-For entry). */
export function clientIp(request) {
  const fwd = request.headers.get("x-forwarded-for");
  return (fwd?.split(",")[0] || request.headers.get("x-real-ip") || "unknown").trim();
}

/** True for a same-origin request (Origin header matches Host). */
export function sameOrigin(request) {
  const origin = request.headers.get("origin");
  const host = request.headers.get("host");
  if (!origin || !host) return false;
  try {
    return new URL(origin).host === host;
  } catch {
    return false;
  }
}

/**
 * For a state-changing route: the person behind the request, re-read from the
 * database (live session, existing user, current role ≥ need — never the
 * token's word alone, #171). → { auth, user } or { response } (401 / 403 / 503)
 * to return as is.
 */
export async function requireRole(request, need) {
  let auth;
  try {
    auth = await getAuth();
  } catch {
    return { response: Response.json({ error: "sign-in is unavailable: see the monitor's logs" }, { status: 503 }) };
  }
  const r = await auth.actingUser(readCookie(request, ACCESS_COOKIE), need);
  if (r.ok) return { auth, user: r.user };
  return { response: r.status === 403 ? roleRequired(need) : sessionRequired() };
}

/** The signed-in user of a request, or null (state-changing routes check it again behind proxy.js). */
export async function sessionUser(request) {
  try {
    return await (await getAuth()).user(readCookie(request, ACCESS_COOKIE));
  } catch {
    return null;
  }
}
