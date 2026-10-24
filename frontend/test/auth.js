// Test helpers for the sign-in code: an in-memory user database and a valid
// session cookie, without touching data/monitor/auth.db.
import crypto from "node:crypto";
import { getAuth, resetAuth } from "@/lib/auth/server";
import { hashToken } from "@/lib/auth/service";
import { ACCESS_COOKIE, authKey, signAccess } from "@/lib/auth/tokens";

// Low entropy on purpose: a test value, not a secret (CI runs gitleaks).
export const TEST_SECRET = "a".repeat(40);

/** Points the shared AuthService at a fresh in-memory database. */
export function useTestAuth() {
  process.env.AUTH_SECRET = TEST_SECRET;
  process.env.AUTH_DB_PATH = ":memory:";
  process.env.ADMIN_USERNAME = "admin";
  process.env.ADMIN_PASSWORD = "admin password for tests";
  resetAuth();
}

/** A Cookie header carrying a valid 15-minute access token (no session behind it: reads only). */
export async function sessionCookie(user = { id: 1, username: "admin", role: "admin" }) {
  return `${ACCESS_COOKIE}=${await signAccess(user, authKey(TEST_SECRET))}`;
}

/**
 * A person signed in on the shared test database: creates `username` with
 * `role` (unless it exists) and a live session, and returns { user, cookie,
 * sid, auth } — what a state-changing route re-reads.
 */
export async function signedIn(username = "admin", role = "admin") {
  const auth = await getAuth();
  const user = auth.store.userByName(username) ?? auth.store.createUser({ username, passwordHash: "not a real hash", role });
  const sid = hashToken(crypto.randomBytes(32).toString("base64url"));
  auth.store.addSession(sid, user.id, Date.now() + 3600 * 1000);
  const cookie = `${ACCESS_COOKIE}=${await signAccess(user, authKey(TEST_SECRET), Date.now(), sid)}`;
  return { user, cookie, sid, auth };
}
