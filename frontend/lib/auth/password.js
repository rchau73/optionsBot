// Server-side only: argon2id password hashes (hash-wasm: WebAssembly, so the
// Alpine image needs no compiler and no native build).
import crypto from "node:crypto";
import { argon2id, argon2Verify } from "hash-wasm";

/** Shortest password a user may have. */
export const MIN_PASSWORD_LENGTH = 12;
/** Longest password hashed: bounds the work one request can cost. */
export const MAX_PASSWORD_LENGTH = 1024;

// OWASP's argon2id baseline: 19 MiB, 2 passes, 1 lane.
const PARAMS = { parallelism: 1, iterations: 2, memorySize: 19456, hashLength: 32 };

/** Returns the encoded argon2id hash ($argon2id$v=19$...) of a password. */
export function hashPassword(password) {
  return argon2id({ ...PARAMS, password, salt: crypto.randomBytes(16), outputType: "encoded" });
}

/** True when the password matches the encoded hash; false for any malformed hash. */
export async function verifyPassword(password, hash) {
  try {
    return await argon2Verify({ password, hash });
  } catch {
    return false;
  }
}

let dummy = null;

/**
 * Verifies the password against a hash nobody knows, so a sign-in for an
 * unknown or locked user costs the same time as a wrong password.
 */
export async function verifyDummy(password) {
  dummy ??= hashPassword(crypto.randomBytes(32).toString("hex"));
  await verifyPassword(password, await dummy);
  return false;
}

/** Null when the password is acceptable, else why not. */
export function passwordProblem(password) {
  if (typeof password !== "string" || password.length < MIN_PASSWORD_LENGTH) {
    return `password: at least ${MIN_PASSWORD_LENGTH} characters`;
  }
  if (password.length > MAX_PASSWORD_LENGTH) return `password: at most ${MAX_PASSWORD_LENGTH} characters`;
  return null;
}
