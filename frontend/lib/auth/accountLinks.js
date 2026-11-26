// Server-side only: manager–account links (#760). An admin links a manager to
// the exchange accounts they manage; a state-changing route that targets a bot
// then refuses a linked manager on a bot of any other account. The accounts
// come from the registry's key-free status file (cmd/accounts -check -json
// -out, #655): ids, labels and each account's bots, never a key.
// Off by default: a manager with no link acts on every account, as before.
import fs from "node:fs";
import path from "node:path";
import { authLog } from "./log";

/** The most accounts one manager may be linked to (a guard on the request size). */
export const MAX_LINKS = 50;
// The registry's opaque id rule (internal/accounts idPattern).
const ACCOUNT_ID = /^[a-z0-9][a-z0-9_-]{0,63}$/;

/** ACCOUNTS_STATUS_PATH, default data/accounts_status.json (accounts.DefaultStatusPath). */
export function accountsStatusPath(raw = process.env.ACCOUNTS_STATUS_PATH) {
  return raw || path.join(process.cwd(), "data", "accounts_status.json");
}

/**
 * The registry's accounts from the status file: [{ account_id, label,
 * environment, bots: [{ currency }] }]. A missing, unreadable or unknown-version
 * file, or one whose registry breaks a rule (no accounts), is [] — no account is known.
 */
export function readAccounts(file = accountsStatusPath()) {
  let doc;
  try {
    doc = JSON.parse(fs.readFileSync(file, "utf8"));
  } catch (err) {
    if (err.code !== "ENOENT") authLog("WARN", "accounts status file unreadable", { file, error: err.message });
    return [];
  }
  if (doc?.version !== 1 || !Array.isArray(doc.accounts)) {
    authLog("WARN", "accounts status file of an unknown shape", { file, version: doc?.version ?? null });
    return [];
  }
  return doc.accounts
    .filter((a) => typeof a?.account_id === "string" && ACCOUNT_ID.test(a.account_id))
    .map((a) => ({
      account_id: a.account_id,
      label: typeof a.label === "string" && a.label ? a.label : a.account_id,
      environment: typeof a.environment === "string" ? a.environment : "",
      bots: Array.isArray(a.bots) ? a.bots.filter((b) => typeof b?.currency === "string").map((b) => ({ currency: b.currency })) : [],
    }));
}

/**
 * The account a monitor bot ("btc") trades for: the one registry account with
 * a bot of that currency, or null when none has it or several do (the monitor
 * cannot tell which).
 */
export function botAccount(bot, accounts) {
  const name = String(bot).toLowerCase();
  const found = accounts.filter((a) => a.bots.some((b) => b.currency.toLowerCase() === name));
  return found.length === 1 ? found[0] : null;
}

/** "Main (acct-a)" — an account in words; its id alone when the label is the id. */
export function accountName(account) {
  return account.label === account.account_id ? account.account_id : `${account.label} (${account.account_id})`;
}

/**
 * Why `user` may not act on `bot`, in plain words, or null (#760). Only a
 * manager with at least one link is limited: admins act on every account,
 * users act on none (their role is refused first), a manager without a link
 * keeps every account of this deployment. A bot whose account cannot be told
 * is refused to a linked manager — the check fails closed.
 */
export function accountLinkProblem({ user, links, bot, accounts }) {
  if (user.role !== "manager" || links.length === 0) return null;
  const account = botAccount(bot, accounts);
  if (!account) {
    return `you manage only some accounts, and the monitor cannot tell which account bot ${bot} trades for: ask an admin to check the account registry`;
  }
  if (!links.includes(account.account_id)) {
    return `you do not manage account ${accountName(account)}, which bot ${bot} trades for: ask an admin to link it to you on the Users page`;
  }
  return null;
}

/**
 * A request's new link list checked against the registry: { ok: true, ids
 * (sorted, unique) } or { ok: false, error }. An id already linked may stay
 * (or be removed) even when the registry no longer lists it; a new one must be
 * in the registry.
 */
export function checkLinkRequest(raw, { current, accounts }) {
  if (!Array.isArray(raw) || raw.length > MAX_LINKS || !raw.every((id) => typeof id === "string" && ACCOUNT_ID.test(id))) {
    return { ok: false, error: `accounts: a list of at most ${MAX_LINKS} account ids` };
  }
  const known = new Set([...current, ...accounts.map((a) => a.account_id)]);
  const unknown = raw.filter((id) => !known.has(id));
  if (unknown.length > 0) return { ok: false, error: `account ${unknown[0]} is not in the account registry` };
  return { ok: true, ids: [...new Set(raw)].sort() };
}

/** What changes from `current` to `next`: { add, remove }, each sorted. */
export function linkChanges(current, next) {
  return { add: next.filter((id) => !current.includes(id)).sort(), remove: current.filter((id) => !next.includes(id)).sort() };
}
