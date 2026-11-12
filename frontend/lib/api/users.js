// The Users page (/api/admin/users/..., admins only).

import { getJSON, sendJSON } from "./http.js";

/** A request to the admin routes → its body ({} when it has none); throws with the server's reason. */
async function adminSend(url, method, body = {}) {
  return (await sendJSON(url, method, body)) ?? {};
}

const userUrl = (id, action = "") => `/api/admin/users/${encodeURIComponent(id)}${action}`;

/**
 * Every monitor user and the registry's accounts (admins only): { users: [{ id,
 * username, role, email, created_at, last_sign_in_at, accounts }], accounts:
 * [{ account_id, label, environment }] }.
 */
export async function fetchUsers(signal) {
  const body = await getJSON("/api/admin/users", signal);
  return { users: body.users, accounts: body.accounts ?? [] };
}

/** Sets the accounts a manager may act on (#760; [] = every account) → { accounts, added, removed }. */
export async function setAccountLinks(id, accounts) {
  return adminSend(userUrl(id, "/accounts"), "PUT", { accounts });
}

/** Adds a person ({ username, password, role, email? }) → the new user. */
export async function createUser(fields) {
  return (await adminSend("/api/admin/users", "POST", fields)).user;
}

/** Changes a user's role; every session of theirs ends. */
export async function changeUserRole(id, role) {
  return (await adminSend(userUrl(id), "PATCH", { role })).user;
}

/** Sets a user's password; every session of theirs ends. */
export async function resetUserPassword(id, password) {
  await adminSend(userUrl(id, "/password"), "POST", { password });
}

/** Ends every session of a user. */
export async function signOutUser(id) {
  await adminSend(userUrl(id, "/signout"), "POST");
}

/** Deletes a user → { events_removed | events_error, survey_answers_removed | survey_answers_error }. */
export async function deleteUser(id) {
  return adminSend(userUrl(id), "DELETE");
}

/** Creates a single-use reset link for a user (30 min; voids their older ones) → { url, expires_at }, shown once. */
export async function issueResetLink(id) {
  return adminSend(userUrl(id, "/reset-link"), "POST");
}
