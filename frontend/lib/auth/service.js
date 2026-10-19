// Server-side only: sign-in, refresh, logout and the first admin — the logic
// behind the /api/auth routes, kept free of Next.js so tests call it directly.
import crypto from "node:crypto";
import { hashPassword, MAX_PASSWORD_LENGTH, passwordProblem, verifyDummy, verifyPassword } from "./password";
import { LoginLimiter } from "./limits";
import { normalizeUsername } from "./store";
import { REFRESH_TTL_SEC, signAccess, verifyAccess } from "./tokens";
import { authLog } from "./log";

/** The one answer for a wrong user, a wrong password and a locked username. */
export const INVALID_LOGIN = "invalid username or password";

/** SHA-256 (hex) of a refresh token: the only form stored. */
export function hashToken(token) {
  return crypto.createHash("sha256").update(token).digest("hex");
}

export class AuthService {
  /** store: AuthStore · key: authKey() · now: clock in ms. */
  constructor({ store, key, now = () => Date.now(), limiter = new LoginLimiter() }) {
    this.store = store;
    this.key = key;
    this.now = now;
    this.limiter = limiter;
  }

  /**
   * Checks a username and password. Every refusal of a well-formed request
   * costs one argon2 verification, so unknown users, wrong passwords and
   * locked usernames take the same time and get the same answer.
   * → { ok: true, user, access, refresh } | { ok: false, status, error, retryAfter? }
   */
  async login({ username, password, ip = "unknown" }) {
    const now = this.now();
    const wait = this.limiter.takeIp(ip, now);
    if (wait > 0) {
      authLog("WARN", "sign-in rate limited", { ip, retry_after_sec: wait });
      return { ok: false, status: 429, error: "too many sign-in attempts: try again later", retryAfter: wait };
    }
    if (typeof username !== "string" || typeof password !== "string" || !username || !password || password.length > MAX_PASSWORD_LENGTH) {
      return { ok: false, status: 400, error: "username and password required" };
    }

    // A malformed username can't exist, but is still throttled under its own name.
    const valid = normalizeUsername(username);
    const name = valid ?? username.trim().toLowerCase().slice(0, 64);
    const user = valid ? this.store.userByName(valid) : null;
    const locked = this.limiter.lockedUntil(name, now);
    if (locked > 0) {
      await verifyDummy(password);
      authLog("WARN", "sign-in refused: username locked", { username: name, ip, locked_until: new Date(locked).toISOString() });
      return { ok: false, status: 401, error: INVALID_LOGIN };
    }

    const ok = user ? await verifyPassword(password, this.store.passwordHash(user.id)) : await verifyDummy(password);
    if (!ok) {
      const nowLocked = this.limiter.recordFailure(name, this.now());
      authLog("WARN", "sign-in failed", { username: name, ip, reason: user ? "password" : "unknown_user", locked: nowLocked });
      return { ok: false, status: 401, error: INVALID_LOGIN };
    }

    this.limiter.clear(name);
    const refresh = crypto.randomBytes(32).toString("base64url");
    this.store.addSession(hashToken(refresh), user.id, now + REFRESH_TTL_SEC * 1000, now);
    authLog("INFO", "signed in", { username: user.username, role: user.role, ip });
    return { ok: true, user, access: await signAccess(user, this.key, now), refresh };
  }

  /** A new access token for a live refresh token whose user still exists → { ok, access, user }. */
  async refresh(refreshToken) {
    if (!refreshToken) return { ok: false };
    const now = this.now();
    const userId = this.store.sessionUser(hashToken(refreshToken), now);
    const user = userId === null ? null : this.store.userById(userId);
    if (!user) return { ok: false };
    return { ok: true, user, access: await signAccess(user, this.key, now) };
  }

  /** Ends the session of a refresh token (logout); the token stops working at once. */
  logout(refreshToken, username = null) {
    if (refreshToken) this.store.revokeSession(hashToken(refreshToken));
    authLog("INFO", "signed out", { username });
  }

  /** The user of an access token, or null. */
  user(accessToken) {
    return verifyAccess(accessToken, this.key, this.now());
  }

  /**
   * Creates the first admin from ADMIN_USERNAME / ADMIN_PASSWORD / ADMIN_EMAIL
   * when there are no users; never touches an existing user.
   */
  async bootstrapAdmin(env = process.env) {
    if (this.store.countUsers() > 0) return null;
    const name = normalizeUsername(env.ADMIN_USERNAME);
    const problem = name ? passwordProblem(env.ADMIN_PASSWORD) : "ADMIN_USERNAME: 3 to 64 of a-z 0-9 . _ -";
    if (problem) {
      authLog("WARN", "no users and no valid first admin: nobody can sign in", { problem: problem.replace(/^password/, "ADMIN_PASSWORD") });
      return null;
    }
    const user = this.store.createUser(
      { username: name, passwordHash: await hashPassword(env.ADMIN_PASSWORD), role: "admin", email: env.ADMIN_EMAIL || null },
      this.now(),
    );
    authLog("INFO", "created the first admin", { username: user.username });
    return user;
  }
}
