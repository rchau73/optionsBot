// Access tokens (JWT HS256, 15 min) and the session cookies. Used by proxy.js
// and the /api/auth routes; nothing here reaches the browser's JavaScript
// (both cookies are HttpOnly).
import { SignJWT, jwtVerify } from "jose";

export const ACCESS_COOKIE = "access_token";
export const REFRESH_COOKIE = "refresh_token";
export const ACCESS_TTL_SEC = 15 * 60;
export const REFRESH_TTL_SEC = 14 * 24 * 3600;
/** The refresh cookie is sent only to the auth routes (refresh, logout). */
export const REFRESH_PATH = "/api/auth";
export const MIN_SECRET_BYTES = 32;

/** The signing key from AUTH_SECRET; throws when it is missing or shorter than 32 bytes. */
export function authKey(secret = process.env.AUTH_SECRET) {
  if (!secret || Buffer.byteLength(secret, "utf8") < MIN_SECRET_BYTES) {
    throw new Error(`AUTH_SECRET must be set to at least ${MIN_SECRET_BYTES} bytes`);
  }
  return new TextEncoder().encode(secret);
}

/** Signs a 15-minute access token for a user ({ id, username, role }). */
export function signAccess(user, key, nowMs = Date.now()) {
  const iat = Math.floor(nowMs / 1000);
  return new SignJWT({ name: user.username, role: user.role })
    .setProtectedHeader({ alg: "HS256", typ: "JWT" })
    .setSubject(String(user.id))
    .setIssuedAt(iat)
    .setExpirationTime(iat + ACCESS_TTL_SEC)
    .sign(key);
}

/** The session's user ({ id, username, role }) or null for a missing, expired or forged token. */
export async function verifyAccess(token, key, nowMs = Date.now()) {
  if (!token) return null;
  try {
    const { payload } = await jwtVerify(token, key, { algorithms: ["HS256"], currentDate: new Date(nowMs) });
    return { id: Number(payload.sub), username: payload.name, role: payload.role };
  } catch {
    return null;
  }
}

const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]", "::1"]);

/**
 * Cookies are Secure except on plain http to this machine (local runs and the
 * compose port on 127.0.0.1), where a browser would drop a Secure cookie.
 */
export function cookieSecure(request) {
  const url = new URL(request.url);
  const proto = (request.headers.get("x-forwarded-proto") ?? url.protocol.replace(":", "")).split(",")[0].trim();
  const host = request.headers.get("host") ?? url.host;
  const hostname = host.replace(/:\d+$/, "").toLowerCase();
  return !(proto === "http" && LOCAL_HOSTS.has(hostname));
}

function cookie(name, value, { path, maxAge, secure }) {
  const parts = [`${name}=${value}`, `Path=${path}`, `Max-Age=${maxAge}`, "HttpOnly", "SameSite=Lax"];
  if (secure) parts.push("Secure");
  return parts.join("; ");
}

/** Set-Cookie value of the access token. */
export function accessCookie(token, secure) {
  return cookie(ACCESS_COOKIE, token, { path: "/", maxAge: ACCESS_TTL_SEC, secure });
}

/** Set-Cookie value of the refresh token (path /api/auth only). */
export function refreshCookie(token, secure) {
  return cookie(REFRESH_COOKIE, token, { path: REFRESH_PATH, maxAge: REFRESH_TTL_SEC, secure });
}

/** Set-Cookie values that delete both session cookies. */
export function clearedCookies(secure) {
  return [
    cookie(ACCESS_COOKIE, "", { path: "/", maxAge: 0, secure }),
    cookie(REFRESH_COOKIE, "", { path: REFRESH_PATH, maxAge: 0, secure }),
  ];
}

/** Reads one cookie from a request's Cookie header. */
export function readCookie(request, name) {
  const header = request.headers.get("cookie") ?? "";
  for (const part of header.split(";")) {
    const i = part.indexOf("=");
    if (i > 0 && part.slice(0, i).trim() === name) return part.slice(i + 1).trim();
  }
  return null;
}

/**
 * Marks the monitor's own "no session" 401s, so the browser refreshes on
 * those only — never on a bot's 401 passed through a proxy route.
 */
export const SESSION_HEADER = "x-monitor-session";

/** 401 for a request without a valid session. */
export function sessionRequired() {
  return Response.json({ error: "sign in required" }, { status: 401, headers: { [SESSION_HEADER]: "required" } });
}
