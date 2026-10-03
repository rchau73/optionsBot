import { configuredBots, pollIntervalMs } from "@/lib/bots";

export const dynamic = "force-dynamic";

/** Lists the configured bots by name (URLs stay on the server) and the poll interval. */
export function GET() {
  return Response.json({ bots: configuredBots().map((b) => ({ name: b.name })), pollMs: pollIntervalMs() });
}
