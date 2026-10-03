import { configuredBots } from "@/lib/bots";

export const dynamic = "force-dynamic";

/** Lists the configured bots by name (URLs stay on the server). */
export function GET() {
  return Response.json({ bots: configuredBots().map((b) => ({ name: b.name })) });
}
