import Monitor from "@/components/Monitor";
import { viewerRole } from "@/lib/auth/viewer";

export default async function Page() {
  const role = await viewerRole();
  return <Monitor role={role} />;
}
