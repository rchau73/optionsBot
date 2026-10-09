// The only module that talks to the backend. Everything goes through the
// Next.js proxy at /api/bots/... (read-only, except closePositions).

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
  const res = await fetch(`/api/bots/${encodeURIComponent(name)}/close`, {
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
