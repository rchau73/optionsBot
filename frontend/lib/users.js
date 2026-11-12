// Pure logic of the Users page (#306): form checks before sending and the
// words of each confirmation. The server checks everything again
// (lib/auth/service.js); these only spare a round trip and say what follows.

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
 * The confirmation of an action on `user` by the admin `me` (a username):
 * { title, body, button } in plain words, saying what follows.
 * action: "role" (with `role`), "password", "link" (a reset link), "signout" or "delete".
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
