// Server-side only: sign-in, refresh, logout, the first admin and user
// administration — the logic behind the /api/auth and /api/admin/users routes,
// kept free of Next.js so tests call it directly.
import crypto from "node:crypto";
import { hashPassword, MAX_PASSWORD_LENGTH, MIN_PASSWORD_LENGTH, passwordProblem, verifyDummy, verifyPassword } from "./password";
import { LOCK_FAILURES, LoginLimiter } from "./limits";
import { normalizeUsername, ROLES } from "./store";
import { REFRESH_TTL_SEC, signAccess, verifyAccess } from "./tokens";
import { authLog } from "./log";
import { roleAtLeast } from "./roles";

/** The one answer for a wrong user, a wrong password and a locked username. */
export const INVALID_LOGIN = "invalid username or password";
/** The one answer for an unknown, used, expired or replaced reset link (#307). */
export const RESET_LINK_INVALID = "link expired or already used";
/** How long a reset link works. */
export const RESET_LINK_TTL_MS = 30 * 60 * 1000;

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
    this.store.recordSignIn(user.id, now);
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
   * of the row), their analytics events (event plan §5, #296) and their survey
   * answers with any free text (surveys.md §5, #363). Access goes first; events
   * or answers that cannot be deleted are an ERROR log, an audit line and
   * `events_error` / `survey_answers_error` in the answer, saying how they go.
   * → { ok: true, events_removed | events_error, survey_answers_removed | survey_answers_error } | { ok: false, status, error }
   */
  deleteUser(by, targetId) {
    const target = this.store.userById(targetId);
    const problem = this.userChangeProblem(by, target, null);
    if (problem) return { ok: false, ...problem };
    this.store.deleteUser(target.id);
    authLog("INFO", "user deleted", { by: by.username, username: target.username });
    this.audit("user_deleted", { username: by.username, role: by.role, target: target.username });
    return this.eraseUserData(by, target, "user_deleted");
  }

  /**
   * A person deletes their own account (#543): the same steps as deleteUser —
   * sessions, consent, analytics events, survey answers with any free text —
   * for any role except the last admin (409 with the reason). Exchange
   * accounts and funds are not the monitor's and are untouched.
   * → { ok: true, events_removed | events_error, survey_answers_removed | survey_answers_error } | { ok: false, status, error }
   */
  deleteOwnAccount(user) {
    const me = this.store.userById(user.id);
    if (!me) return { ok: false, status: 404, error: "no such user" };
    if (me.role === "admin" && this.store.countAdmins() <= 1) {
      authLog("WARN", "account deletion refused: last admin", { username: me.username });
      return { ok: false, status: 409, error: "you are the only admin: make another person an admin on the Users page first, then delete your account" };
    }
    this.store.deleteUser(me.id);
    authLog("INFO", "account deleted by its owner", { username: me.username, role: me.role });
    this.audit("account_deleted", { username: me.username, role: me.role });
    return this.eraseUserData(me, me, "account_deleted");
  }

  /**
   * The steps after a user row is gone: their analytics events, then their
   * survey answers. A step that fails is an ERROR log, an audit line and
   * `events_error` / `survey_answers_error` in the result, saying how it goes.
   */
  eraseUserData(by, target, reason) {
    const who = { username: by.username, role: by.role, target: target.username, target_row_id: target.id, reason };
    const result = { ok: true };
    try {
      const { removed } = this.deleteEvents(target.id);
      authLog("INFO", "analytics events of a deleted user deleted", { component: "analytics", by: by.username, username: target.username, removed });
      this.audit("events_deleted", { ...who, removed });
      result.events_removed = removed;
    } catch (err) {
      authLog("ERROR", "analytics events of a deleted user NOT deleted", {
        component: "analytics",
        by: by.username,
        username: target.username,
        user_row_id: target.id,
        error: err.message,
      });
      this.audit("events_delete_failed", { ...who, error: err.message });
      result.events_error = `the user is deleted but their analytics events are not (${err.message}): run scripts/analytics-user.mjs delete --row-id ${target.id}`;
    }
    try {
      const removed = this.store.deleteSurveyAnswers(target.id);
      authLog("INFO", "survey answers of a deleted user deleted", { component: "analytics", by: by.username, username: target.username, removed });
      this.audit("survey_answers_deleted", { ...who, removed });
      result.survey_answers_removed = removed;
    } catch (err) {
      authLog("ERROR", "survey answers of a deleted user NOT deleted", {
        component: "analytics",
        by: by.username,
        username: target.username,
        user_row_id: target.id,
        error: err.message,
      });
      this.audit("survey_answers_delete_failed", { ...who, error: err.message });
      result.survey_answers_error = `the user is deleted but their survey answers are not (${err.message}): the monitor deletes them at its next start`;
    }
    return result;
  }

  /**
   * The person's answer to usage analytics (event plan §5, #363): stored on
   * their row and audited as `analytics_consent_changed` with who and the new
   * value only. No (false) stops collection at once; events already stored
   * stay until the person asks for their deletion.
   */
  setAnalyticsConsent(user, consent) {
    this.store.setAnalyticsConsent(user.id, consent, this.now());
    authLog("INFO", "analytics consent changed", { component: "analytics", username: user.username, consent });
    this.audit("analytics_consent_changed", { username: user.username, consent });
  }

  /** Every user for the Users page: id, username, role, email, created and last sign-in (ISO, null = never); never a hash. */
  listUsers() {
    const iso = (sec) => (sec ? new Date(sec * 1000).toISOString() : null);
    return this.store.listUsers().map((u) => ({
      id: u.id,
      username: u.username,
      role: u.role,
      email: u.email ?? null,
      created_at: iso(u.created_at),
      last_sign_in_at: iso(u.last_sign_in_at),
    }));
  }

  /**
   * An admin adds a person (#306): the sign-in username rule, one of the three
   * roles, the ≥ 12-character password rule, an optional email.
   * → { ok: true, user } | { ok: false, status (400 rule, 409 username taken), error }
   */
  async createUser(by, { username, password, role, email } = {}) {
    const name = normalizeUsername(username);
    if (!name) return { ok: false, status: 400, error: "username: 3 to 64 of a-z 0-9 . _ -" };
    if (!ROLES.includes(role)) return { ok: false, status: 400, error: `role: one of ${ROLES.join(", ")}` };
    const weak = passwordProblem(password);
    if (weak) return { ok: false, status: 400, error: weak };
    if (email != null && typeof email !== "string") return { ok: false, status: 400, error: "email: name@domain" };
    const mail = (email ?? "").trim();
    if (mail && (mail.length > 254 || !/^[^\s@]+@[^\s@]+$/.test(mail))) return { ok: false, status: 400, error: "email: name@domain" };
    const taken = { ok: false, status: 409, error: `username ${name} is taken` };
    if (this.store.userByName(name)) return taken;

    const hash = await hashPassword(password);
    let user;
    try {
      user = this.store.createUser({ username: name, passwordHash: hash, role, email: mail || null }, this.now());
    } catch (err) {
      // Created by another request while the password was hashed (UNIQUE username); anything else is a real failure.
      if (/UNIQUE/.test(err?.message ?? "")) return taken;
      throw err;
    }
    authLog("INFO", "user created", { by: by.username, username: user.username, role });
    this.audit("user_created", { username: by.username, role: by.role, target: user.username, to: role });
    return { ok: true, user };
  }

  /**
   * An admin sets another person's password (#306) and ends every session of
   * theirs, so each of their browsers must sign in again. Your own password
   * goes through changePassword, which asks for the current one.
   * → { ok: true } | { ok: false, status (400 rule, 404 no user, 409 yourself), error }
   */
  async resetPassword(by, targetId, password) {
    const target = this.store.userById(targetId);
    if (!target) return { ok: false, status: 404, error: "no such user" };
    if (target.id === by.id) return { ok: false, status: 409, error: "reset your own password with your name ▾ in the menu bar → Change password" };
    const weak = passwordProblem(password);
    if (weak) return { ok: false, status: 400, error: weak };
    this.store.setPassword(target.id, await hashPassword(password), this.now());
    authLog("INFO", "password reset", { event: "password_reset", by: by.username, username: target.username });
    this.audit("password_reset", { username: by.username, role: by.role, target: target.username });
    return { ok: true };
  }

  /**
   * An admin issues a single-use reset link for another person (#307): 32
   * random bytes, only their SHA-256 stored, valid RESET_LINK_TTL_MS (30 min).
   * A new link voids the person's older ones. The token is returned once, for
   * the URL the admin hands over; it is never stored, logged or audited.
   * → { ok: true, token, expires_at (ISO) } | { ok: false, status (404 no user, 409 yourself), error }
   */
  issueResetLink(by, targetId) {
    const target = this.store.userById(targetId);
    if (!target) return { ok: false, status: 404, error: "no such user" };
    if (target.id === by.id) return { ok: false, status: 409, error: "change your own password with your name ▾ in the menu bar → Change password" };
    const now = this.now();
    const token = crypto.randomBytes(32).toString("base64url");
    const expires = now + RESET_LINK_TTL_MS;
    this.store.addResetToken(hashToken(token), target.id, expires, by.username, now);
    const expiresAt = new Date(expires).toISOString();
    authLog("INFO", "reset link issued", { by: by.username, username: target.username, expires_at: expiresAt });
    this.audit("reset_link_issued", { username: by.username, role: by.role, target: target.username, expires_at: expiresAt });
    return { ok: true, token, expires_at: expiresAt };
  }

  /**
   * Someone with a reset link sets a new password (#307, public like sign-in):
   * under the per-IP sign-in limit (429), `next` follows the sign-in rule (400
   * with the reason, the link stays usable), then the link must be live —
   * unknown, used, expired or voided all get the one RESET_LINK_INVALID 400.
   * Success marks the link used and ends every session of the person; it does
   * not sign anyone in. Lockout is not touched.
   * → { ok: true } | { ok: false, status, error, retryAfter? }
   */
  async resetWithLink({ token, next, ip = "unknown" }) {
    const wait = this.limiter.takeIp(ip, this.now());
    if (wait > 0) {
      authLog("WARN", "reset by link rate limited", { ip, retry_after_sec: wait });
      return { ok: false, status: 429, error: "too many attempts: try again later", retryAfter: wait };
    }
    const weak = passwordProblem(next);
    if (weak) return { ok: false, status: 400, error: weak };
    const invalid = { ok: false, status: 400, error: RESET_LINK_INVALID };
    if (typeof token !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(token)) {
      authLog("WARN", "reset by link refused", { ip, reason: "malformed" });
      return invalid;
    }
    // The hash is computed before the link is claimed, so the claim and the check are one statement.
    const hash = await hashPassword(next);
    const now = this.now();
    const link = this.store.useResetToken(hashToken(token), now);
    const user = link ? this.store.userById(link.user_id) : null;
    if (!user) {
      authLog("WARN", "reset by link refused", { ip, reason: "unknown, used or expired" });
      return invalid;
    }
    this.store.setPassword(user.id, hash, now);
    authLog("INFO", "password reset by link", { event: "password_reset", via: "link", username: user.username, by: link.created_by, ip });
    this.audit("password_reset_by_link", { username: user.username, role: user.role, by: link.created_by, ip });
    return { ok: true };
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

  /** Audits an action a person took through a route (close, rebalance with what the bot answered; a survey answer). */
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
