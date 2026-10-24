// Server-side only: who is looking at the page, for display defaults.
import { cookies } from "next/headers";
import { ACCESS_COOKIE, authKey, verifyAccess } from "./tokens";

/**
 * The signed-in person's role from the access token proxy.js already checked
 * (null when it cannot be read). It only picks a display default (#287:
 * details open for managers and admins), never what anyone may do — the
 * state-changing routes re-read the role from the database (requireRole).
 */
export async function viewerRole() {
  return (await viewer())?.role ?? null;
}

/**
 * The signed-in person ({ id, username, role }) from the access token, or
 * null. Display only, like viewerRole: the Users page shows its admin view to
 * admins, but its routes decide (requireRole).
 */
export async function viewer() {
  try {
    return await verifyAccess((await cookies()).get(ACCESS_COOKIE)?.value, authKey());
  } catch {
    return null;
  }
}
