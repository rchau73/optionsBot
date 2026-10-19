// Test helpers for the sign-in code: an in-memory user database and a valid
// session cookie, without touching data/monitor/auth.db.
import { resetAuth } from "@/lib/auth/server";
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

/** A Cookie header carrying a valid 15-minute access token. */
export async function sessionCookie(user = { id: 1, username: "admin", role: "admin" }) {
  return `${ACCESS_COOKIE}=${await signAccess(user, authKey(TEST_SECRET))}`;
}
