// The only module that talks to the backend. Everything goes through the
// Next.js read-only proxy at /api/bots/...

async function getJSON(url, signal) {
  const res = await fetch(url, { cache: "no-store", signal });
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

/** Status, positions, pending orders and account summary of one bot, fetched together. */
export async function fetchBotState(name, signal) {
  const base = `/api/bots/${encodeURIComponent(name)}`;
  const [status, positions, orders, account] = await Promise.all([
    getJSON(`${base}/status`, signal),
    getJSON(`${base}/positions`, signal),
    getJSON(`${base}/orders`, signal),
    // Older bots have no /account: treat as "no account data", not an outage.
    getJSON(`${base}/account`, signal).catch(() => null),
  ]);
  return { status, positions, orders, account };
}

/** Journal events after `since` (sequence number), oldest first. */
export async function fetchBotEvents(name, since, signal) {
  const { events } = await getJSON(`/api/bots/${encodeURIComponent(name)}/events?since=${since}&limit=200`, signal);
  return events;
}

/** Bucketed P&L history of one bot for a range (15m, 1h, 6h, 1d, 1w, 1m, all). */
export async function fetchPnlHistory(name, range, signal) {
  return getJSON(`/api/bots/${encodeURIComponent(name)}/pnl/history?range=${encodeURIComponent(range)}`, signal);
}
