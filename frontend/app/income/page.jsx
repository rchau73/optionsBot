import IncomePage from "@/components/IncomePage";

export const metadata = { title: "Income · optionsBot monitor" };

/** /income?bot=<name>: one bot's income month by month (#319). */
export default async function Page({ searchParams }) {
  const { bot } = await searchParams;
  return <IncomePage bot={typeof bot === "string" ? bot : null} />;
}
