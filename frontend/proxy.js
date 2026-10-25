import crypto from "node:crypto";
import { NextResponse } from "next/server";
import { ACCESS_COOKIE, authKey, sessionRequired, verifyAccess } from "@/lib/auth/tokens";

/** Routes reachable without a session. */
const PUBLIC_API = new Set(["/api/auth/login", "/api/auth/refresh", "/api/auth/reset"]);
/** Pages reachable with or without a session: setting a new password with a reset link (#307). */
const PUBLIC_PAGES = new Set(["/reset"]);
const LOGIN_PAGE = "/login";
const MIN_SERVICE_TOKEN = 32;

/**
 * The read-only service token (MONITOR_API_TOKEN) the deploy health check and
 * the agents send as "Authorization: Bearer …": GET of bot data only, never
 * a page, a close or a rebalance.
 */
export function serviceTokenOk(request, token = process.env.MONITOR_API_TOKEN) {
  if (!token || token.length < MIN_SERVICE_TOKEN) return false;
  if (request.method !== "GET" && request.method !== "HEAD") return false;
  const { pathname } = request.nextUrl;
  if (pathname !== "/api/bots" && !pathname.startsWith("/api/bots/")) return false;
  const got = Buffer.from(request.headers.get("authorization") ?? "");
  const want = Buffer.from(`Bearer ${token}`);
  return got.length === want.length && crypto.timingSafeEqual(got, want);
}

/**
 * Every page needs a session (else → /login?next=…) and every /api route
 * answers 401 without one, except sign-in, refresh and the reset link's page
 * and route. Route handlers that change trading state check the session again.
 */
export async function proxy(request) {
  const { pathname, search } = request.nextUrl;
  if (PUBLIC_API.has(pathname) || PUBLIC_PAGES.has(pathname)) return NextResponse.next();

  let key;
  try {
    key = authKey();
  } catch {
    return Response.json({ error: "monitor misconfigured: AUTH_SECRET is not set" }, { status: 500 });
  }
  const user = await verifyAccess(request.cookies.get(ACCESS_COOKIE)?.value, key);

  if (pathname === LOGIN_PAGE) {
    return user ? NextResponse.redirect(new URL("/", request.url)) : NextResponse.next();
  }
  if (user) return NextResponse.next();
  if (pathname.startsWith("/api/")) {
    if (serviceTokenOk(request)) return NextResponse.next();
    return sessionRequired();
  }
  const login = new URL(LOGIN_PAGE, request.url);
  login.searchParams.set("next", pathname + search);
  return NextResponse.redirect(login);
}

export const config = {
  // Everything but Next's static assets.
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
