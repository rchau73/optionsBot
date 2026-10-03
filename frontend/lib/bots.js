// Server-side only: where each bot's read-only API lives.
// BOT_APIS="btc=http://bot-btc:8081,eth=http://bot-eth:8082" (comma-separated name=url).

const DEFAULT_BOTS = "btc=http://127.0.0.1:8081,eth=http://127.0.0.1:8082";

/** Parses BOT_APIS into [{ name, url }], ignoring malformed entries. */
export function parseBots(raw = DEFAULT_BOTS) {
  return raw
    .split(",")
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((entry) => {
      const i = entry.indexOf("=");
      if (i <= 0) return null;
      const name = entry.slice(0, i).trim().toLowerCase();
      const url = entry.slice(i + 1).trim().replace(/\/+$/, "");
      if (!/^[a-z0-9_-]+$/.test(name) || !/^https?:\/\//.test(url)) return null;
      return { name, url };
    })
    .filter(Boolean);
}

export function configuredBots() {
  return parseBots(process.env.BOT_APIS || DEFAULT_BOTS);
}

/** The API endpoints the proxy may forward to — read-only, nothing else. */
export const ALLOWED_ENDPOINTS = ["health", "status", "positions", "orders", "pnl", "events"];

/** Poll interval for the browser (MONITOR_POLL_MS, 500–60000 ms, default 1000). */
export function pollIntervalMs(raw = process.env.MONITOR_POLL_MS) {
  const n = Number.parseInt(raw ?? "", 10);
  if (!Number.isFinite(n)) return 1000;
  return Math.min(Math.max(n, 500), 60000);
}
