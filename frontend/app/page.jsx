import Monitor from "@/components/Monitor";
import { viewerRole } from "@/lib/auth/viewer";

export default async function Page({ searchParams }) {
  const role = await viewerRole();
  const { bot } = (await searchParams) ?? {};
  return <Monitor role={role} bot={typeof bot === "string" ? bot : null} />;
}
