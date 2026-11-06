// Server-side only: argon2id password hashes (hash-wasm: WebAssembly, so the
// Alpine image needs no compiler and no native build).
import crypto from "node:crypto";
import { argon2id, argon2Verify } from "hash-wasm";
import { COMMON_PASSWORDS } from "./commonPasswords";

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

// The look-alikes people type for a letter. 1 stands for i or l, so it gets its own step.
const LOOK_ALIKES = { "@": "a", 4: "a", 3: "e", "!": "i", 0: "o", $: "s", 5: "s", 7: "t" };

const unmask = (text, one) => text.replace(/[@4!30$57]/g, (c) => LOOK_ALIKES[c]).replaceAll("1", one);

/**
 * True when the password is on the bundled list (#680), ignoring case, look-alike
 * characters (p@ssw0rd) and digits or symbols around it (password2026!), which
 * is how a short common word reaches 12 characters.
 */
function passwordIsCommon(password) {
  const lower = password.toLowerCase();
  const noTail = lower.replace(/[^a-z]+$/, "");
  const noEnds = noTail.replace(/^[^a-z]+/, "");
  return [lower, noTail, noEnds].some(
    (form) => COMMON_PASSWORDS.has(form) || COMMON_PASSWORDS.has(unmask(form, "i")) || COMMON_PASSWORDS.has(unmask(form, "l")),
  );
}

/**
 * Why a new password is guessable, or null (#680): the one rule beyond length,
 * for every way a person gets a new password. Takes the username the password
 * is for; the reason goes to the person as is, and the password to no log.
 */
export function guessablePasswordProblem(password, username) {
  if (passwordIsCommon(password)) return "too common";
  if (password.toLowerCase().includes(username.toLowerCase())) return "contains your username";
  return null;
}
