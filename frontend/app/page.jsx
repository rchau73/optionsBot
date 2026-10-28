import { redirect } from "next/navigation";
import Monitor from "@/components/Monitor";
import { detailsRedirect } from "@/lib/menu";

export default async function Page({ searchParams }) {
  const { bot, view } = (await searchParams) ?? {};
  // The old "Details (pro)" link (?view=details) now opens its own page (#405).
  const target = detailsRedirect({ bot, view });
  if (target) redirect(target);
  return <Monitor bot={typeof bot === "string" ? bot : null} />;
}
