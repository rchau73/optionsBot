// The one fetch of the monitor's API and its errors (#466). Each domain's
// calls live in their own lib/api/<domain>.js and import from here; callers
// import the domain file directly (there is no barrel file).

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

/** The response's JSON body, or null when it is not JSON. */
export async function jsonOrNull(res) {
  try {
    return await res.json();
  } catch {
    return null;
  }
}

/** A GET of JSON; throws with the server's reason (its `error`, else the status). */
export async function getJSON(url, signal) {
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

/**
 * A same-origin JSON request (POST, PATCH, DELETE) → its JSON body, or null
 * when it has none; throws with the server's reason.
 */
export async function sendJSON(url, method, body) {
  const res = await apiFetch(url, {
    method,
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const answer = await jsonOrNull(res);
  if (!res.ok) throw new Error(answer?.error ?? `${res.status}`);
  return answer;
}
