// The only module that talks to the backend. Everything goes through the
// Next.js proxy at /api/bots/... (read-only, except closePositions and
// rebalanceBot) or /api/auth/... (signOut, changePassword), behind the
// signed-in session's cookies.

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
  const [status, positions, orders, account, summary] = await Promise.all([
    getJSON(`${base}/status`, signal),
    getJSON(`${base}/positions`, signal),
    getJSON(`${base}/orders`, signal),
    // Older bots have no /account: treat as "no account data", not an outage.
    getJSON(`${base}/account`, signal).catch(() => null),
    getJSON(`${base}/summary`, signal).then(
      (data) => ({ data, error: null }),
      (e) => ({ data: null, error: e.message }),
    ),
  ]);
  return { status, positions, orders, account, summary: summary.data, summaryError: summary.error };
}

/** Journal events after `since` (sequence number), oldest first. */
export async function fetchBotEvents(name, since, signal) {
  const { events } = await getJSON(`/api/bots/${encodeURIComponent(name)}/events?since=${since}&limit=200`, signal);
  return events;
}

/** Opens and closes after journal sequence `after`, oldest first: { trades, history_since }. */
export async function fetchBotTrades(name, after, signal) {
  return getJSON(`/api/bots/${encodeURIComponent(name)}/trades?after=${after}&limit=1000`, signal);
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
