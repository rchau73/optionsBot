// Server-side only: sign-in, refresh, logout and the first admin — the logic
// behind the /api/auth routes, kept free of Next.js so tests call it directly.
import crypto from "node:crypto";
import { hashPassword, MAX_PASSWORD_LENGTH, MIN_PASSWORD_LENGTH, passwordProblem, verifyDummy, verifyPassword } from "./password";
import { LOCK_FAILURES, LoginLimiter } from "./limits";
import { normalizeUsername, ROLES } from "./store";
import { REFRESH_TTL_SEC, signAccess, verifyAccess } from "./tokens";
import { authLog } from "./log";
import { roleAtLeast } from "./roles";

/** The one answer for a wrong user, a wrong password and a locked username. */
export const INVALID_LOGIN = "invalid username or password";

/** SHA-256 (hex) of a refresh token: the only form stored. */
export function hashToken(token) {
  return crypto.createHash("sha256").update(token).digest("hex");
}

/** Why `next` may not replace `current`, in plain words, or null. */
export function newPasswordProblem(current, next) {
  if (typeof next !== "string" || next.length < MIN_PASSWORD_LENGTH) return `Too short: at least ${MIN_PASSWORD_LENGTH} characters`;
  if (next.length > MAX_PASSWORD_LENGTH) return `Too long: at most ${MAX_PASSWORD_LENGTH} characters`;
  if (next === current) return "The new password must differ from the current one";
  return null;
}

export class AuthService {
  /**
   * store: AuthStore · key: authKey() · now: clock in ms · audit: sink of audit entries (lib/auth/audit.js) ·
   * deleteEvents: removes a user row id's analytics events → { removed }, throws when it cannot
   * (lib/analytics/retention.js userEventsEraser; none by default).
   */
  constructor({ store, key, now = () => Date.now(), limiter = new LoginLimiter(store), audit = () => {}, deleteEvents = () => ({ removed: 0 }) }) {
    this.store = store;
    this.key = key;
    this.now = now;
    this.limiter = limiter;
    this.auditSink = audit;
    this.deleteEvents = deleteEvents;
  }

  /** One audit entry: what happened, by whom (username, role), when; never a secret. */
  audit(action, fields) {
    this.auditSink({ time: new Date(this.now()).toISOString(), action, ...fields });
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
      this.audit("sign_in_failed", { username: name, ip, reason: "locked" });
      return { ok: false, status: 401, error: INVALID_LOGIN };
    }

    const ok = user ? await verifyPassword(password, this.store.passwordHash(user.id)) : await verifyDummy(password);
    if (!ok) {
      const nowLocked = this.limiter.recordFailure(name, this.now());
      const reason = user ? "password" : "unknown_user";
      authLog("WARN", "sign-in failed", { username: name, ip, reason, locked: nowLocked });
      this.audit("sign_in_failed", { username: name, ip, reason });
      if (nowLocked) this.audit("locked", { username: name, ip, failures: LOCK_FAILURES });
      return { ok: false, status: 401, error: INVALID_LOGIN };
    }

    this.limiter.clear(name);
    const refresh = crypto.randomBytes(32).toString("base64url");
    const sid = hashToken(refresh);
    this.store.addSession(sid, user.id, now + REFRESH_TTL_SEC * 1000, now);
    authLog("INFO", "signed in", { username: user.username, role: user.role, ip });
    this.audit("signed_in", { username: user.username, role: user.role, ip });
    return { ok: true, user, access: await signAccess(user, this.key, now, sid), refresh };
  }

  /**
   * A signed-in person changes their own password (#212). The session must be
   * live (re-read like a state-changing route). `next` follows the sign-in
   * rule (≥ 12 characters) and must differ from `current`; a wrong `current`
   * counts as a failed sign-in of that username, so 5 lock it exactly like
   * the sign-in form. Success ends every session of the user and opens a new
   * one for this browser only.
   * → { ok: true, user, access, refresh } | { ok: false, status, error, retryAfter? }
   */
  async changePassword({ accessToken, current, next, ip = "unknown" }) {
    const acting = await this.actingUser(accessToken, "user");
    if (!acting.ok) return { ok: false, status: 401, session: true, error: "sign in required" };
    const user = acting.user;
    if (typeof current !== "string" || typeof next !== "string" || !current || current.length > MAX_PASSWORD_LENGTH) {
      return { ok: false, status: 400, error: "current and new password required" };
    }
    const weak = newPasswordProblem(current, next);
    if (weak) return { ok: false, status: 400, error: weak };

    const locked = this.limiter.lockedUntil(user.username, this.now());
    if (locked > 0) {
      await verifyDummy(current);
      const retryAfter = Math.max(1, Math.ceil((locked - this.now()) / 1000));
      authLog("WARN", "password change refused: username locked", { username: user.username, ip, locked_until: new Date(locked).toISOString() });
      this.audit("password_change_failed", { username: user.username, role: user.role, ip, reason: "locked" });
      return { ok: false, status: 429, error: "too many wrong passwords: try again later", retryAfter };
    }
    if (!(await verifyPassword(current, this.store.passwordHash(user.id)))) {
      const nowLocked = this.limiter.recordFailure(user.username, this.now());
      authLog("WARN", "password change failed", { username: user.username, ip, reason: "password", locked: nowLocked });
      this.audit("password_change_failed", { username: user.username, role: user.role, ip, reason: "password" });
      if (nowLocked) this.audit("locked", { username: user.username, ip, failures: LOCK_FAILURES });
      return { ok: false, status: 401, error: "Current password is wrong" };
    }

    this.limiter.clear(user.username);
    const now = this.now();
    // setPassword ends every session (this browser's too), then this browser gets a new one.
    this.store.setPassword(user.id, await hashPassword(next), now);
    const refresh = crypto.randomBytes(32).toString("base64url");
    const sid = hashToken(refresh);
    this.store.addSession(sid, user.id, now + REFRESH_TTL_SEC * 1000, now);
    authLog("INFO", "password changed", { event: "password_changed", username: user.username, ip });
    this.audit("password_changed", { username: user.username, role: user.role, ip });
    return { ok: true, user, access: await signAccess(user, this.key, now, sid), refresh };
  }

  /** A new access token for a live refresh token whose user still exists → { ok, access, user }. */
  async refresh(refreshToken) {
    if (!refreshToken) return { ok: false };
    const now = this.now();
    const sid = hashToken(refreshToken);
    const userId = this.store.sessionUser(sid, now);
    const user = userId === null ? null : this.store.userById(userId);
    if (!user) return { ok: false };
    return { ok: true, user, access: await signAccess(user, this.key, now, sid) };
  }

  /**
   * The person behind a state-changing request, re-read from the database
   * rather than trusted from the 15-minute token (#171): the token's session
   * must still be live and theirs, the user must still exist, and their
   * current role must be at least `need`. The role is checked first, so a
   * demoted user hears 403 (not allowed), not 401 (sign in again).
   * → { ok: true, user } | { ok: false, status: 401 (no user or no live session) | 403 (role) }
   */
  async actingUser(accessToken, need) {
    const now = this.now();
    const claimed = await verifyAccess(accessToken, this.key, now);
    const user = claimed ? this.store.userById(claimed.id) : null;
    if (!user) return { ok: false, status: 401 };
    if (!roleAtLeast(user.role, need)) {
      authLog("WARN", "action refused: role", { username: user.username, role: user.role, need });
      return { ok: false, status: 403, user };
    }
    // Tokens signed before #171 carry no session: 401, so the browser refreshes into one that does.
    if (!claimed.sid || this.store.sessionUser(claimed.sid, now) !== user.id) {
      authLog("WARN", "action refused: session ended", { username: user.username });
      return { ok: false, status: 401 };
    }
    return { ok: true, user };
  }

  /** Why `by` may not change `target` to `role` (or delete it, role null), or null. */
  userChangeProblem(by, target, role) {
    if (!target) return { status: 404, error: "no such user" };
    if (role !== null && !ROLES.includes(role)) return { status: 400, error: `role: one of ${ROLES.join(", ")}` };
    if (role === null && by.id === target.id) return { status: 409, error: "you cannot delete yourself" };
    if (target.role === "admin" && role !== "admin" && this.store.countAdmins() <= 1) {
      return { status: 409, error: "the last admin cannot be removed or demoted: there must always be an admin" };
    }
    return null;
  }

  /** An admin changes a user's role; every session of that user ends. → { ok, user } | { ok: false, status, error } */
  changeRole(by, targetId, role) {
    const target = this.store.userById(targetId);
    const problem = this.userChangeProblem(by, target, role);
    if (problem) return { ok: false, ...problem };
    this.store.setRole(target.id, role, this.now());
    authLog("INFO", "role changed", { by: by.username, username: target.username, from: target.role, to: role });
    this.audit("role_changed", { username: by.username, role: by.role, target: target.username, from: target.role, to: role });
    return { ok: true, user: this.store.userById(target.id) };
  }

  /**
   * An admin deletes a user: every session of theirs, their consent (a column
   * of the row) and their analytics events (event plan §5, #296). Access goes
   * first; events that cannot be deleted are an ERROR log, an audit line and
   * `events_error` in the answer, with the row id the admin CLI needs to retry.
   * → { ok: true, events_removed } | { ok: true, events_error } | { ok: false, status, error }
   */
  deleteUser(by, targetId) {
    const target = this.store.userById(targetId);
    const problem = this.userChangeProblem(by, target, null);
    if (problem) return { ok: false, ...problem };
    this.store.deleteUser(target.id);
    authLog("INFO", "user deleted", { by: by.username, username: target.username });
    this.audit("user_deleted", { username: by.username, role: by.role, target: target.username });

    const who = { username: by.username, role: by.role, target: target.username, target_row_id: target.id, reason: "user_deleted" };
    try {
      const { removed } = this.deleteEvents(target.id);
      authLog("INFO", "analytics events of a deleted user deleted", { component: "analytics", by: by.username, username: target.username, removed });
      this.audit("events_deleted", { ...who, removed });
      return { ok: true, events_removed: removed };
    } catch (err) {
      authLog("ERROR", "analytics events of a deleted user NOT deleted", {
        component: "analytics",
        by: by.username,
        username: target.username,
        user_row_id: target.id,
        error: err.message,
      });
      this.audit("events_delete_failed", { ...who, error: err.message });
      return {
        ok: true,
        events_error: `the user is deleted but their analytics events are not (${err.message}): run scripts/analytics-user.mjs delete --row-id ${target.id}`,
      };
    }
  }

  /** "Sign out everywhere": ends every session of a user; their next state-changing request is refused. */
  signOutEverywhere(by, targetId) {
    const target = this.store.userById(targetId);
    if (!target) return { ok: false, status: 404, error: "no such user" };
    this.store.revokeSessions(target.id);
    authLog("INFO", "signed out everywhere", { by: by.username, username: target.username });
    this.audit("signed_out_everywhere", { username: by.username, role: by.role, target: target.username });
    return { ok: true };
  }

  /** Audits a manual action a person asked a bot for (close, rebalance) with what the bot answered. */
  recordAction(user, action, fields) {
    this.audit(action, { username: user.username, role: user.role, ...fields });
  }

  /** Ends the session of a refresh token (logout); the token stops working at once. */
  logout(refreshToken, username = null) {
    if (refreshToken) this.store.revokeSession(hashToken(refreshToken));
    authLog("INFO", "signed out", { username });
    if (username) this.audit("signed_out", { username });
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
    this.audit("user_created", { username: "bootstrap", target: user.username, to: "admin" });
    return user;
  }
}
