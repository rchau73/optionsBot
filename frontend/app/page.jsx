import Monitor from "@/components/Monitor";
import { viewerRole } from "@/lib/auth/viewer";
import { DETAILS_VIEW } from "@/lib/menu";

export default async function Page({ searchParams }) {
  const role = await viewerRole();
  const { bot, view } = (await searchParams) ?? {};
  return <Monitor role={role} bot={typeof bot === "string" ? bot : null} details={view === DETAILS_VIEW} />;
}
