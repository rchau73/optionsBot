import WorstCaseView from "@/components/WorstCaseView";

export const metadata = { title: "Worst case right now · optionsBot monitor" };

/** "Worst case right now" for one bot (#318): /worst-case?bot=<name>. */
export default async function Page({ searchParams }) {
  const { bot } = (await searchParams) ?? {};
  const name = typeof bot === "string" && /^[a-z0-9_-]+$/i.test(bot) ? bot.toLowerCase() : null;
  return <WorstCaseView bot={name} />;
}
