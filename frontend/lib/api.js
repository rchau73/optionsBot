// The only module that talks to the backend. Everything goes through the
// Next.js proxy at /api/bots/... (read-only, except closePositions and
// rebalanceBot), /api/auth/... (signOut, changePassword, resetWithLink — the
// one call without a session) or /api/admin/... (the Users and Audit pages,
// admins only), behind the signed-in session's cookies.

let refreshing = null;

/** One refresh for every caller that got a 401 at the same time → true when the session lives on. */
function refreshSession() {
  refreshing ??= fetch("/api/auth/refresh", { method: "POST", cache: "no-store" })
    .then((res) => res.ok)
    .catch(() => false)
    .finally(() => {
      refreshing = null;
    });
  return refreshing;
}

/** Page navigation (replaced in tests: jsdom does not navigate). */
export const navigation = { assign: (url) => window.location.assign(url) };

/** Sends the browser to the sign-in page, back to this page afterwards. */
function toLogin() {
  const here = window.location.pathname + window.location.search;
  navigation.assign(`/login?next=${encodeURIComponent(here)}`);
}

/**
 * fetch for the monitor's API: a 401 (access token expired) triggers one
 * shared silent refresh and one retry; when the session is over, the page
 * goes to /login.
 */
export async function apiFetch(url, init = {}) {
  const res = await fetch(url, init);
  if (!sessionExpired(res)) return res;
  if (await refreshSession()) {
    const retry = await fetch(url, init);
    if (!sessionExpired(retry)) return retry;
  }
  toLogin();
  return res;
}

// Only the monitor's own 401 means "no session"; a bot's 401 is passed on as an error.
function sessionExpired(res) {
  return res.status === 401 && res.headers?.get?.("x-monitor-session") === "required";
}

/** Ends the session (its refresh token stops working) and goes to the sign-in page. */
export async function signOut() {
  try {
    await apiFetch("/api/auth/logout", { method: "POST", cache: "no-store" });
  } finally {
    navigation.assign("/login");
  }
}

/**
 * Changes the signed-in person's password; every other session of theirs ends,
 * this browser stays signed in. Throws with the server's reason.
 */
export async function changePassword(current, next) {
  const res = await apiFetch("/api/auth/password", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ current, next }),
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    /* body was not JSON */
  }
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
}

/** A same-origin JSON request to the monitor's admin routes; throws with the server's reason. */
async function adminSend(url, method, body = {}) {
  const res = await apiFetch(url, {
    method,
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  let answer = null;
  try {
    answer = await res.json();
  } catch {
    /* body was not JSON */
  }
  if (!res.ok) throw new Error(answer?.error ?? `${res.status}`);
  return answer ?? {};
}

const userUrl = (id, action = "") => `/api/admin/users/${encodeURIComponent(id)}${action}`;

/** Every monitor user (admins only): [{ id, username, role, email, created_at, last_sign_in_at }]. */
export async function fetchUsers(signal) {
  return (await getJSON("/api/admin/users", signal)).users;
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

/**
 * Sets a new password with a reset link (no session needed); every session of
 * that person ends. Throws with the server's reason.
 */
export async function resetWithLink(token, next) {
  const res = await fetch("/api/auth/reset", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token, next }),
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    /* body was not JSON */
  }
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
}

/** One page of the audit trail (admins only) → { off, entries, skipped, next_before }; `query` from lib/audit.js auditQueryString. */
export async function fetchAudit(query, signal) {
  return getJSON(`/api/admin/audit${query ? `?${query}` : ""}`, signal);
}

async function getJSON(url, signal) {
  const res = await apiFetch(url, { cache: "no-store", signal });
  if (!res.ok) {
    let message = `${res.status}`;
    try {
      message = (await res.json()).error ?? message;
    } catch {
      /* body was not JSON */
    }
    throw new Error(message);
  }
  return res.json();
}

/** Bot names and the configured poll interval (ms). */
export async function fetchMonitorConfig(signal) {
  const { bots, pollMs } = await getJSON("/api/bots", signal);
  return { names: bots.map((b) => b.name), pollMs: pollMs ?? 1000 };
}

/**
 * Status, positions, pending orders, account and investor summary of one bot,
 * fetched together. A failed summary is kept apart (summaryError) so the rest
 * of the bot still shows; the investor view then says it is unavailable.
 */
export async function fetchBotState(name, signal) {
  const base = `/api/bots/${encodeURIComponent(name)}`;
  const [status, positions, orders, account, summary, payoff] = await Promise.all([
    getJSON(`${base}/status`, signal),
    getJSON(`${base}/positions`, signal),
    getJSON(`${base}/orders`, signal),
    // Older bots have no /account: treat as "no account data", not an outage.
    getJSON(`${base}/account`, signal).catch(() => null),
    getJSON(`${base}/summary`, signal).then(
      (data) => ({ data, error: null }),
      (e) => ({ data: null, error: e.message }),
    ),
    // The payoff charts (#391); a failure leaves the rest of the bot shown.
    getJSON(`${base}/strangles/payoff`, signal).then(
      (data) => ({ data, error: null }),
      (e) => ({ data: null, error: e.message }),
    ),
  ]);
  return {
    status,
    positions,
    orders,
    account,
    summary: summary.data,
    summaryError: summary.error,
    payoff: payoff.data,
    payoffError: payoff.error,
  };
}

/** The payoff chart of each open strangle of one bot (/api/strangles/payoff, #391). */
export async function fetchBotPayoff(name, signal) {
  return getJSON(`/api/bots/${encodeURIComponent(name)}/strangles/payoff`, signal);
}

/** The investor summary of one bot (/api/summary): holding, income, worst case, last action. */
export async function fetchBotSummary(name, signal) {
  return getJSON(`/api/bots/${encodeURIComponent(name)}/summary`, signal);
}

/** Journal events after `since` (sequence number), oldest first. */
export async function fetchBotEvents(name, since, signal, limit = 200) {
  const { events } = await getJSON(`/api/bots/${encodeURIComponent(name)}/events?since=${since}&limit=${limit}`, signal);
  return events;
}

/** Opens and closes after journal sequence `after`, oldest first: { trades, history_since }. */
export async function fetchBotTrades(name, after, signal) {
  return getJSON(`/api/bots/${encodeURIComponent(name)}/trades?after=${after}&limit=1000`, signal);
}

/** One bot's income month by month with its summary (/api/income, #319). */
export async function fetchBotIncome(name, signal) {
  return getJSON(`/api/bots/${encodeURIComponent(name)}/income`, signal);
}

/** Bucketed P&L history of one bot for a range (15m, 1h, 6h, 1d, 1w, 1m, all). */
export async function fetchPnlHistory(name, range, signal) {
  return getJSON(`/api/bots/${encodeURIComponent(name)}/pnl/history?range=${encodeURIComponent(range)}`, signal);
}

/**
 * Asks a bot to buy back positions at market (manual close; the bot allows it
 * only under the regime-side block). Resolves to its per-position results.
 */
export async function closePositions(name, positionIds) {
  const res = await apiFetch(`/api/bots/${encodeURIComponent(name)}/close`, {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ position_ids: positionIds }),
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    /* body was not JSON */
  }
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
  return body.results ?? [];
}

async function jsonOrNull(res) {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

/**
 * The assistant's state for the signed-in person (#408): { status, enabled,
 * remaining, error }. Never throws: a failure is { status: 0, enabled: null }.
 */
export async function fetchAssistantState(signal) {
  try {
    const res = await apiFetch("/api/assistant", { cache: "no-store", signal });
    const body = await jsonOrNull(res);
    return { status: res.status, enabled: res.ok ? true : res.status === 503 ? false : null, remaining: body?.remaining ?? null, error: body?.error ?? null };
  } catch {
    return { status: 0, enabled: null, remaining: null, error: null };
  }
}

/**
 * Asks the assistant (#191, #408): { question, bot?, seq? } → { status, body,
 * retryAfter } with the route's status as is (200, 429, 503, …), so the panel
 * shows each state. Never throws: a network failure is status 0.
 */
export async function askAssistant({ question, bot = null, seq = null }) {
  const payload = { question };
  if (bot) payload.bot = bot;
  if (bot && seq != null) payload.seq = seq;
  try {
    const res = await apiFetch("/api/assistant", {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const body = (await jsonOrNull(res)) ?? {};
    const retry = Number(res.headers?.get?.("retry-after") ?? body.retry_after);
    return { status: res.status, body, retryAfter: Number.isFinite(retry) && retry > 0 ? retry : null };
  } catch {
    return { status: 0, body: {}, retryAfter: null };
  }
}

/** Flags an assistant answer as wrong; throws with the server's reason. */
export async function flagAssistantAnswer(id) {
  const res = await apiFetch("/api/assistant/flag", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id }),
  });
  if (!res.ok) throw new Error((await jsonOrNull(res))?.error ?? `${res.status}`);
}

/**
 * Sends a draft request for review (#409): { answerId, kind, fields } →
 * { id, status }. Only the draft card's Send calls it; throws with the
 * server's reason (limit, already sent, …).
 */
export async function sendAssistantRequest({ answerId, kind, fields }) {
  const res = await apiFetch("/api/assistant/requests", {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ answer_id: answerId, kind, fields }),
  });
  const body = await jsonOrNull(res);
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
  return body;
}

/** The signed-in person's own requests, newest first (#409); throws with the server's reason. */
export async function fetchAssistantRequests(signal) {
  const res = await apiFetch("/api/assistant/requests", { cache: "no-store", signal });
  const body = await jsonOrNull(res);
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
  return body?.requests ?? [];
}

/**
 * Manual rebalance of one bot through the monitor's proxy. dryRun: only report
 * what the pass would do. Returns the bot's report; throws with the bot's
 * reason when it refuses (frozen, cooldown, no margin data...).
 */
export async function rebalanceBot(name, dryRun) {
  const res = await apiFetch(`/api/bots/${encodeURIComponent(name)}/rebalance`, {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dry_run: dryRun }),
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    /* body was not JSON */
  }
  if (!res.ok) throw new Error(body?.error ?? `${res.status}`);
  return body;
}
