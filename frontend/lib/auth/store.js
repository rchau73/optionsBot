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
    if (!exists) store.save();
    return store;
  }

  constructor(db, file) {
    this.db = db;
    this.file = file;
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

  /** Deletes a user and every session of theirs. */
  deleteUser(id) {
    this.db.run("DELETE FROM refresh_tokens WHERE user_id = ?", [id]);
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
}
