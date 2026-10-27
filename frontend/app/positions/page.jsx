import PositionsPage from "@/components/PositionsPage";
import { botParam } from "@/lib/menu";

export const metadata = { title: "Positions · optionsBot monitor" };

/** One payoff chart per strangle (#391): /positions?bot=<name>. */
export default async function Page({ searchParams }) {
  const { bot } = (await searchParams) ?? {};
  return <PositionsPage bot={botParam(bot)} />;
}
