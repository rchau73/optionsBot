import IncomePage from "@/components/IncomePage";

export const metadata = { title: "Income · optionsBot monitor" };

/** /income?bot=<name>[&from=<time>]: one bot's income month by month (#319); from starts the % view (#629). */
export default async function Page({ searchParams }) {
  const { bot, from } = await searchParams;
  return <IncomePage bot={typeof bot === "string" ? bot : null} from={typeof from === "string" ? from : null} />;
}
