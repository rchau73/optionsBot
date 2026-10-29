// Server-side only: the monitor's users and sessions in one SQLite file
// (data/monitor/auth.db, one per environment). sql.js runs SQLite in
// WebAssembly — no native build in the image — on an in-memory copy that is
// written back to the file (temp file + rename) after every change.
import fs from "node:fs";
import path from "node:path";
import initSqlJs from "sql.js";

export const ROLES = ["admin", "manager", "user"];
const USERNAME = /^[a-z0-9._-]{3,64}$/;

const SCHEMA = `
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role          TEXT NOT NULL CHECK (role IN ('admin', 'manager', 'user')),
  email         TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS refresh_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS refresh_tokens_user ON refresh_tokens(user_id);
-- Sign-in lockout (lib/auth/limits.js), kept here so a restart does not clear
-- it. Times are in ms.
CREATE TABLE IF NOT EXISTS login_failures (
  username TEXT NOT NULL,
  at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS login_failures_username ON login_failures(username);
CREATE TABLE IF NOT EXISTS login_locks (
  username TEXT PRIMARY KEY,
  until    INTEGER NOT NULL
);
-- Single-use password-reset links an admin issues (#307): only the token's
-- SHA-256 is stored. Times are in ms; used_at NULL = not used yet; created_by
-- is the issuing admin's username.
CREATE TABLE IF NOT EXISTS reset_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  expires_at INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  used_at    INTEGER
);
CREATE INDEX IF NOT EXISTS reset_tokens_user ON reset_tokens(user_id);
-- In-product survey answers (#363, docs/product/analytics/surveys.md §5): the
-- codes (a JSON object of the survey's allow-listed fields) and the optional
-- free text — personal data, kept here beside the user, deleted with them and
-- never sent in an event or a log line. created_at in Unix seconds.
CREATE TABLE IF NOT EXISTS survey_answers (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id),
  survey_id  TEXT NOT NULL,
  codes      TEXT NOT NULL,
  free_text  TEXT,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS survey_answers_user ON survey_answers(user_id);
-- The notification bell (#447): which items a person has read (per person
-- on the server, so a second device agrees; read_at in Unix seconds, pruned
-- after 90 days like the items) and the kinds they turned off (enabled 0).
CREATE TABLE IF NOT EXISTS notification_reads (
  user_id INTEGER NOT NULL REFERENCES users(id),
  item_id TEXT NOT NULL,
  read_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, item_id)
);
CREATE TABLE IF NOT EXISTS notification_settings (
  user_id INTEGER NOT NULL REFERENCES users(id),
  kind    TEXT NOT NULL,
  enabled INTEGER NOT NULL,
  PRIMARY KEY (user_id, kind)
);
`;

/** Lower-cases and trims a username; null when it is not 3–64 of a-z 0-9 . _ - */
export function normalizeUsername(raw) {
  if (typeof raw !== "string") return null;
  const name = raw.trim().toLowerCase();
  return USERNAME.test(name) ? name : null;
}

let sqlModule = null;

function loadSql() {
  // The .wasm is read from node_modules (next.config.mjs traces it into the
  // standalone image), so no bundler has to handle it.
  sqlModule ??= initSqlJs({
    wasmBinary: fs.readFileSync(path.join(process.cwd(), "node_modules/sql.js/dist/sql-wasm.wasm")),
  });
  return sqlModule;
}

export class AuthStore {
  /** Opens (or creates) the database file; ":memory:" keeps it in memory only (tests). */
  static async open(file) {
    const SQL = await loadSql();
    const exists = file !== ":memory:" && fs.existsSync(file);
    const db = exists ? new SQL.Database(fs.readFileSync(file)) : new SQL.Database();
    const store = new AuthStore(db, file);
    db.exec(SCHEMA);
    const migrated = store.migrate();
    if (!exists || migrated) store.save();
    return store;
  }

  /**
   * Reads an existing file into a copy that is never written back (no schema
   * change, no save): the admin CLI reads users while the monitor, which holds
   * its own copy and writes it after every change, keeps running.
   */
  static async openReadOnly(file) {
    const SQL = await loadSql();
    return new AuthStore(new SQL.Database(fs.readFileSync(file)), ":memory:");
  }

  constructor(db, file) {
    this.db = db;
    this.file = file;
  }

  /** Adds the columns a file older than this code lacks; true when one was added. */
  migrate() {
    const cols = this.all("PRAGMA table_info(users)").map((c) => c.name);
    const added = [
      // NULL = not asked yet, 0 = no, 1 = yes (event plan §5: off until yes).
      ["analytics_consent", "INTEGER"],
      // Unix seconds of the last successful sign-in, NULL = never (#306, the Users page).
      ["last_sign_in_at", "INTEGER"],
      // The monthly NPS card (#446, surveys.md §1): sign-ins counted (the
      // sessions of the ≥ 3 rule), the day it was last shown (Unix seconds,
      // NULL = never) and the person's "Don't ask me again" (1).
      ["sign_in_count", "INTEGER NOT NULL DEFAULT 0"],
      ["nps_last_shown_at", "INTEGER"],
      ["nps_opted_out", "INTEGER NOT NULL DEFAULT 0"],
    ].filter(([name]) => !cols.includes(name));
    for (const [name, type] of added) this.db.exec(`ALTER TABLE users ADD COLUMN ${name} ${type}`);
    if (added.some(([name]) => name === "sign_in_count")) {
      // Sign-ins before the count existed: each live session was one, and a recorded sign-in at least one.
      this.db.run(`UPDATE users SET sign_in_count = MAX(
        (SELECT COUNT(*) FROM refresh_tokens r WHERE r.user_id = users.id),
        CASE WHEN last_sign_in_at IS NULL THEN 0 ELSE 1 END)`);
    }
    // Answers whose user is gone: a deletion whose survey step failed (AuthService.deleteUser) finishes here.
    this.db.run("DELETE FROM survey_answers WHERE user_id NOT IN (SELECT id FROM users)");
    const orphans = this.db.getRowsModified();
    return added.length > 0 || orphans > 0;
  }

  save() {
    if (this.file === ":memory:") return;
    fs.mkdirSync(path.dirname(this.file), { recursive: true, mode: 0o700 });
    const tmp = `${this.file}.tmp`;
    fs.writeFileSync(tmp, Buffer.from(this.db.export()), { mode: 0o600 });
    fs.renameSync(tmp, this.file);
  }

  all(sql, params = []) {
    const stmt = this.db.prepare(sql);
    try {
      stmt.bind(params);
      const rows = [];
      while (stmt.step()) rows.push(stmt.getAsObject());
      return rows;
    } finally {
      stmt.free();
    }
  }

  one(sql, params = []) {
    return this.all(sql, params)[0] ?? null;
  }

  write(sql, params = []) {
    this.db.run(sql, params);
    this.save();
  }

  countUsers() {
    return this.one("SELECT COUNT(*) AS n FROM users").n;
  }

  countAdmins() {
    return this.one("SELECT COUNT(*) AS n FROM users WHERE role = 'admin'").n;
  }

  /** Adds a user; passwordHash is already argon2id. Returns the user (no hash). */
  createUser({ username, passwordHash, role, email = null }, nowMs = Date.now()) {
    const name = normalizeUsername(username);
    if (!name) throw new Error("username: 3 to 64 of a-z 0-9 . _ -");
    if (!ROLES.includes(role)) throw new Error(`role: one of ${ROLES.join(", ")}`);
    const t = Math.floor(nowMs / 1000);
    this.write("INSERT INTO users (username, password_hash, role, email, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", [
      name,
      passwordHash,
      role,
      email,
      t,
      t,
    ]);
    return this.userByName(name);
  }

  /** The user without the hash, or null. */
  userByName(username) {
    const name = normalizeUsername(username);
    return name ? this.one("SELECT id, username, role, email FROM users WHERE username = ?", [name]) : null;
  }

  userById(id) {
    return this.one("SELECT id, username, role, email FROM users WHERE id = ?", [id]);
  }

  /** Every user for the Users page, oldest first: no hash, times in Unix seconds (last sign-in null = never). */
  listUsers() {
    return this.all("SELECT id, username, role, email, created_at, last_sign_in_at FROM users ORDER BY id");
  }

  /** Records a successful sign-in (its time, and one more in the count). */
  recordSignIn(id, nowMs = Date.now()) {
    this.write("UPDATE users SET last_sign_in_at = ?, sign_in_count = sign_in_count + 1 WHERE id = ?", [Math.floor(nowMs / 1000), id]);
  }

  /**
   * What the NPS rule reads (#446, lib/analytics/nps.js): { consent,
   * createdAtSec, sessions, lastShownSec (null = never), optedOut }, or null
   * for an unknown user.
   */
  npsState(id) {
    const r = this.one("SELECT analytics_consent, created_at, sign_in_count, nps_last_shown_at, nps_opted_out FROM users WHERE id = ?", [id]);
    if (!r) return null;
    return {
      consent: r.analytics_consent === 1 ? true : r.analytics_consent === 0 ? false : null,
      createdAtSec: r.created_at,
      sessions: r.sign_in_count,
      lastShownSec: r.nps_last_shown_at ?? null,
      optedOut: r.nps_opted_out === 1,
    };
  }

  /** Records the day the NPS card was shown (the 30-day cap counts from it). */
  markNpsShown(id, nowMs = Date.now()) {
    this.write("UPDATE users SET nps_last_shown_at = ? WHERE id = ?", [Math.floor(nowMs / 1000), id]);
  }

  /** "Don't ask me again" (true) or turned back on in Account › Privacy (false). */
  setNpsOptOut(id, optedOut, nowMs = Date.now()) {
    this.write("UPDATE users SET nps_opted_out = ?, updated_at = ? WHERE id = ?", [optedOut ? 1 : 0, Math.floor(nowMs / 1000), id]);
  }

  /** The stored hash of a user, for the sign-in check only. */
  passwordHash(id) {
    return this.one("SELECT password_hash FROM users WHERE id = ?", [id])?.password_hash ?? null;
  }

  /** Changes a password and signs the user out everywhere. */
  setPassword(id, passwordHash, nowMs = Date.now()) {
    this.db.run("UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", [passwordHash, Math.floor(nowMs / 1000), id]);
    this.revokeSessions(id);
  }

  /** Changes a role and signs the user out everywhere. */
  setRole(id, role, nowMs = Date.now()) {
    if (!ROLES.includes(role)) throw new Error(`role: one of ${ROLES.join(", ")}`);
    this.db.run("UPDATE users SET role = ?, updated_at = ? WHERE id = ?", [role, Math.floor(nowMs / 1000), id]);
    this.revokeSessions(id);
  }

  /** The user's analytics answer: true (yes), false (no) or null (not asked). */
  analyticsConsent(id) {
    const v = this.one("SELECT analytics_consent FROM users WHERE id = ?", [id])?.analytics_consent;
    return v === 1 ? true : v === 0 ? false : null;
  }

  /** How many users answered yes, no or were never asked (the Analytics page, #445): counts only. */
  consentCounts() {
    const row = this.one(
      "SELECT SUM(analytics_consent = 1) AS yes, SUM(analytics_consent = 0) AS no, SUM(analytics_consent IS NULL) AS unasked FROM users",
    );
    return { yes: row?.yes ?? 0, no: row?.no ?? 0, unasked: row?.unasked ?? 0 };
  }

  /** Records the user's analytics answer (true / false) or clears it (null). */
  setAnalyticsConsent(id, consent, nowMs = Date.now()) {
    const v = consent === true ? 1 : consent === false ? 0 : null;
    this.write("UPDATE users SET analytics_consent = ?, updated_at = ? WHERE id = ?", [v, Math.floor(nowMs / 1000), id]);
  }

  /** Stores one survey answer (validated by lib/analytics/surveys.js) → its row id. */
  addSurveyAnswer(userId, { surveyId, codes, text = null }, nowMs = Date.now()) {
    this.db.run("INSERT INTO survey_answers (user_id, survey_id, codes, free_text, created_at) VALUES (?, ?, ?, ?, ?)", [
      userId,
      surveyId,
      JSON.stringify(codes),
      text,
      Math.floor(nowMs / 1000),
    ]);
    const id = this.one("SELECT last_insert_rowid() AS id").id;
    this.save();
    return id;
  }

  /** Removes one answer (its event could not be stored). */
  deleteSurveyAnswer(id) {
    this.write("DELETE FROM survey_answers WHERE id = ?", [id]);
  }

  /** A user's survey answers, oldest first: { survey_id, codes, text, created_at (ISO) } — for an access request's export. */
  surveyAnswers(userId) {
    return this.all("SELECT survey_id, codes, free_text, created_at FROM survey_answers WHERE user_id = ? ORDER BY id", [userId]).map((r) => ({
      survey_id: r.survey_id,
      codes: JSON.parse(r.codes),
      text: r.free_text ?? null,
      created_at: new Date(r.created_at * 1000).toISOString(),
    }));
  }

  /** Deletes every survey answer of a user → how many. */
  deleteSurveyAnswers(userId) {
    this.db.run("DELETE FROM survey_answers WHERE user_id = ?", [userId]);
    const removed = this.db.getRowsModified();
    this.save();
    return removed;
  }

  /** The ids of the notification items a person has read (#447), as a Set. */
  notificationReads(userId) {
    return new Set(this.all("SELECT item_id FROM notification_reads WHERE user_id = ?", [userId]).map((r) => r.item_id));
  }

  /**
   * Marks items read for a person (already read ones keep their date), and
   * prunes read marks older than keepDays — their items are no longer listed.
   */
  markNotificationsRead(userId, itemIds, keepDays, nowMs = Date.now()) {
    const nowSec = Math.floor(nowMs / 1000);
    for (const id of itemIds) {
      this.db.run("INSERT OR IGNORE INTO notification_reads (user_id, item_id, read_at) VALUES (?, ?, ?)", [userId, id, nowSec]);
    }
    this.write("DELETE FROM notification_reads WHERE read_at < ?", [nowSec - keepDays * 24 * 3600]);
  }

  /** A person's notification settings: kind → false for the kinds they turned off, true for those turned back on. */
  notificationSettings(userId) {
    return Object.fromEntries(this.all("SELECT kind, enabled FROM notification_settings WHERE user_id = ?", [userId]).map((r) => [r.kind, r.enabled === 1]));
  }

  setNotificationSetting(userId, kind, enabled) {
    this.write("INSERT OR REPLACE INTO notification_settings (user_id, kind, enabled) VALUES (?, ?, ?)", [userId, kind, enabled ? 1 : 0]);
  }

  /** Deletes a user, every session of theirs, their reset links and their notification state. */
  deleteUser(id) {
    this.db.run("DELETE FROM refresh_tokens WHERE user_id = ?", [id]);
    this.db.run("DELETE FROM reset_tokens WHERE user_id = ?", [id]);
    this.db.run("DELETE FROM notification_reads WHERE user_id = ?", [id]);
    this.db.run("DELETE FROM notification_settings WHERE user_id = ?", [id]);
    this.write("DELETE FROM users WHERE id = ?", [id]);
  }

  /** Stores a refresh token by its SHA-256 hash (the token itself is never stored). */
  addSession(tokenHash, userId, expiresAtMs, nowMs = Date.now()) {
    this.db.run("DELETE FROM refresh_tokens WHERE expires_at <= ?", [Math.floor(nowMs / 1000)]);
    this.write("INSERT INTO refresh_tokens (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)", [
      tokenHash,
      userId,
      Math.floor(expiresAtMs / 1000),
      Math.floor(nowMs / 1000),
    ]);
  }

  /** The user id of a live session, or null when it is unknown or expired. */
  sessionUser(tokenHash, nowMs = Date.now()) {
    const row = this.one("SELECT user_id FROM refresh_tokens WHERE token_hash = ? AND expires_at > ?", [tokenHash, Math.floor(nowMs / 1000)]);
    return row?.user_id ?? null;
  }

  /** Ends one session (logout). */
  revokeSession(tokenHash) {
    this.write("DELETE FROM refresh_tokens WHERE token_hash = ?", [tokenHash]);
  }

  /** Ends every session of a user. */
  revokeSessions(userId) {
    this.write("DELETE FROM refresh_tokens WHERE user_id = ?", [userId]);
  }

  sessionCount(userId) {
    return this.one("SELECT COUNT(*) AS n FROM refresh_tokens WHERE user_id = ?", [userId]).n;
  }

  /**
   * Stores a reset link by its SHA-256 hash and voids every other link of the
   * user (used or not); links expired by nowMs (any user) are deleted too.
   */
  addResetToken(tokenHash, userId, expiresAtMs, createdBy, nowMs) {
    this.db.run("DELETE FROM reset_tokens WHERE user_id = ? OR expires_at <= ?", [userId, nowMs]);
    this.write("INSERT INTO reset_tokens (token_hash, user_id, expires_at, created_by, created_at) VALUES (?, ?, ?, ?, ?)", [
      tokenHash,
      userId,
      expiresAtMs,
      createdBy,
      nowMs,
    ]);
  }

  /**
   * Marks a live link used — unknown, used or expired at nowMs is refused —
   * in one statement, so two requests with one link cannot both win.
   * → { user_id, created_by } or null.
   */
  useResetToken(tokenHash, nowMs) {
    this.db.run("UPDATE reset_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?", [nowMs, tokenHash, nowMs]);
    if (this.db.getRowsModified() !== 1) return null;
    this.save();
    return this.one("SELECT user_id, created_by FROM reset_tokens WHERE token_hash = ?", [tokenHash]);
  }

  /** Every stored reset link of a user (tests and the admin CLI): hashes and times only. */
  resetTokens(userId) {
    return this.all("SELECT token_hash, user_id, expires_at, created_by, created_at, used_at FROM reset_tokens WHERE user_id = ? ORDER BY created_at", [userId]);
  }

  /** The recorded sign-in failure times (ms) of a username, oldest first. */
  loginFailureTimes(username) {
    return this.all("SELECT at FROM login_failures WHERE username = ? ORDER BY at", [username]).map((r) => r.at);
  }

  /** Records a failure at atMs, first deleting every failure (any username) at or before expiredMs. */
  addLoginFailure(username, atMs, expiredMs) {
    this.db.run("DELETE FROM login_failures WHERE at <= ?", [expiredMs]);
    this.write("INSERT INTO login_failures (username, at) VALUES (?, ?)", [username, atMs]);
  }

  /**
   * Locks a username until untilMs and forgets its failures; also deletes
   * locks ended by nowMs and failures (any username) at or before expiredMs.
   */
  lockLogin(username, untilMs, nowMs, expiredMs) {
    this.db.run("DELETE FROM login_failures WHERE username = ? OR at <= ?", [username, expiredMs]);
    this.db.run("DELETE FROM login_locks WHERE until <= ?", [nowMs]);
    this.write("INSERT OR REPLACE INTO login_locks (username, until) VALUES (?, ?)", [username, untilMs]);
  }

  /** The time (ms) a username's lock ends, or 0 when it has none (an ended lock still answers its time). */
  loginLockUntil(username) {
    return this.one("SELECT until FROM login_locks WHERE username = ?", [username])?.until ?? 0;
  }

  /** Forgets the failures of a username; writes the file only when it had some. */
  clearLoginFailures(username) {
    if (!this.one("SELECT 1 AS found FROM login_failures WHERE username = ? LIMIT 1", [username])) return;
    this.write("DELETE FROM login_failures WHERE username = ?", [username]);
  }
}
