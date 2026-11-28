// Pure logic of the Users page (#306): form checks before sending and the
// words of each confirmation. The server checks everything again
// (lib/auth/service.js); these only spare a round trip and say what follows.

import { DISPLAY_TZ_LABEL, localClock } from "./time";

export const ROLES = ["admin", "manager", "user"];
/** The sign-in rule (lib/auth/password.js MIN_PASSWORD_LENGTH; that file is server-only). */
export const MIN_PASSWORD = 12;
const USERNAME = /^[a-z0-9._-]{3,64}$/;

/** What is wrong with a password and its confirmation, or null. */
export function passwordFormProblem({ password, confirm }) {
  if ((password ?? "").length < MIN_PASSWORD) return `Too short: at least ${MIN_PASSWORD} characters`;
  if (password !== confirm) return "The two passwords do not match";
  return null;
}

/** What is wrong with the "Add user" form, or null. */
export function newUserProblem(form) {
  if (!USERNAME.test((form.username ?? "").trim().toLowerCase())) return "Username: 3 to 64 of a-z 0-9 . _ -";
  if (!ROLES.includes(form.role)) return "Pick a role";
  return passwordFormProblem(form);
}

/** An account in words, from the registry list: "Main (acct-a)", or its id alone when unknown or unlabelled. */
export function accountWords(id, accounts = []) {
  const a = accounts.find((x) => x.account_id === id);
  return a && a.label && a.label !== id ? `${a.label} (${id})` : id;
}

/**
 * The Users page's Accounts cell (#760): which accounts a person may act on.
 * Admins act on every account, users on none; a manager without a link on
 * every account of this deployment, a linked one on the listed accounts only.
 */
export function accountsSummary(user, accounts = []) {
  if (user.role === "admin") return "All accounts";
  if (user.role === "user") return "None: sees only";
  const links = user.accounts ?? [];
  if (links.length === 0) return "All accounts (no links)";
  return links.map((id) => accountWords(id, accounts)).join(", ");
}

/**
 * The account-links dialog's consequence for manager `user` when the links
 * become `picked` (account ids): what they may act on afterwards, in plain words.
 */
export function linksConsequence(user, picked, accounts = []) {
  if (picked.length === 0) return `${user.username} may close legs and rebalance on every account of this deployment, as without links.`;
  const names = picked.map((id) => accountWords(id, accounts)).join(", ");
  return `${user.username} may close legs and rebalance only on ${names}; the monitor refuses them on any other account.`;
}

/**
 * The Users page's lock badge (#846): "Locked until 14:32 BRT" from a row's
 * `locked_until` (ISO, display zone), or null when the row is not locked.
 */
export function lockedLabel(user) {
  if (!user?.locked_until) return null;
  const clock = localClock(user.locked_until);
  return clock ? `Locked until ${clock.slice(0, 5)} ${DISPLAY_TZ_LABEL}` : "Locked";
}

/** An account without a sign-in for this many days is flagged unused (#910); fixed, not a setting. */
export const UNUSED_DAYS = 90;
const DAY_MS = 24 * 60 * 60 * 1000;

/** Whole days from ISO time `iso` to `now` (ms), or null when `iso` is missing or unreadable. */
function daysSince(iso, now) {
  const t = Date.parse(iso ?? "");
  return Number.isNaN(t) ? null : Math.floor((now - t) / DAY_MS);
}

/**
 * The Users page's unused tag (#910) for a row at `now` (ms), or null:
 * "Unused for 90 days" when the last sign-in is UNUSED_DAYS or more ago,
 * "Never signed in, created N days ago" when it never signed in and was
 * created that long ago. A tag only: it blocks nothing and changes nothing.
 */
export function unusedLabel(user, now) {
  if (user?.last_sign_in_at) {
    const days = daysSince(user.last_sign_in_at, now);
    return days !== null && days >= UNUSED_DAYS ? `Unused for ${UNUSED_DAYS} days` : null;
  }
  const age = daysSince(user?.created_at, now);
  return age !== null && age >= UNUSED_DAYS ? `Never signed in, created ${age} days ago` : null;
}

/**
 * The confirmation of an action on `user` by the admin `me` (a username):
 * { title, body, button } in plain words, saying what follows.
 * action: "role" (with `role`), "password", "link" (a reset link), "signout", "unlock" or "delete".
 */
export function confirmText(action, user, { role = null, me = null } = {}) {
  const self = user.username === me;
  const sessions = self ? "All your sessions end" : `All of ${user.username}'s sessions end`;
  const again = self ? "you sign in again" : "they sign in again";
  switch (action) {
    case "role":
      return {
        title: `Change ${user.username}'s role`,
        body: `${user.username}: ${user.role} → ${role}. ${sessions}: ${again} with the new role.`,
        button: "Change role",
      };
    case "password":
      return {
        title: `Reset ${user.username}'s password`,
        body: `Sets a new password for ${user.username} and signs them out on every browser. Give them the new password yourself, by a safe channel.`,
        button: "Reset password",
      };
    case "link":
      return {
        title: `Reset link for ${user.username}`,
        body: `Creates a link with which ${user.username} sets a new password. It works once, for 30 minutes, and voids any earlier link. You see it only now: send it by a safe channel. ${sessions} when it is used.`,
        button: "Create link",
      };
    case "signout":
      return {
        title: `Sign ${user.username} out everywhere`,
        body: `${sessions}; ${again} with the same password.`,
        button: "Sign out everywhere",
      };
    case "unlock":
      return {
        title: `Unlock ${user.username}`,
        body: "They can sign in again now. Their failed attempts are forgotten. If someone else is guessing this password, reset it instead.",
        button: "Unlock",
      };
    case "delete":
      return {
        title: `Delete ${user.username}`,
        body: `${user.username} can no longer sign in and their analytics events are deleted. This cannot be undone.`,
        button: "Delete user",
      };
    default:
      throw new Error(`unknown action ${action}`);
  }
}
