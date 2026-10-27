import HowItWorksView from "@/components/HowItWorksView";

export const metadata = { title: "How it works · optionsBot monitor" };

/** "How it works" for one bot (#322): /how-it-works?bot=<name>. */
export default async function Page({ searchParams }) {
  const { bot } = (await searchParams) ?? {};
  const name = typeof bot === "string" && /^[a-z0-9_-]+$/i.test(bot) ? bot.toLowerCase() : null;
  return <HowItWorksView bot={name} />;
}
