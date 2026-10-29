import NotificationsPage from "@/components/NotificationsPage";
import { botParam } from "@/lib/menu";

export const metadata = { title: "Notifications · optionsBot monitor" };

/** /notifications?bot=<name>: the bell's list as a full page (a phone opens it), with the settings per kind (#447). */
export default async function Page({ searchParams }) {
  const { bot } = (await searchParams) ?? {};
  return <NotificationsPage bot={botParam(bot)} />;
}
