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

export async function fetchBotNames(signal) {
  const { bots } = await getJSON("/api/bots", signal);
  return bots.map((b) => b.name);
}

/** Status, positions and pending orders of one bot, fetched together. */
export async function fetchBotState(name, signal) {
  const base = `/api/bots/${encodeURIComponent(name)}`;
  const [status, positions, orders] = await Promise.all([
    getJSON(`${base}/status`, signal),
    getJSON(`${base}/positions`, signal),
    getJSON(`${base}/orders`, signal),
  ]);
  return { status, positions, orders };
}

/** Journal events after `since` (sequence number), oldest first. */
export async function fetchBotEvents(name, since, signal) {
  const { events } = await getJSON(`/api/bots/${encodeURIComponent(name)}/events?since=${since}&limit=200`, signal);
  return events;
}
