import ActivityPage from "@/components/ActivityPage";
import { botParam } from "@/lib/menu";

export const metadata = { title: "What the bot did, and why · optionsBot monitor" };

/** /activity?bot=<name>: one bot's actions day by day, in plain words (#385). */
export default async function Page({ searchParams }) {
  const { bot } = (await searchParams) ?? {};
  return <ActivityPage bot={botParam(bot)} />;
}
