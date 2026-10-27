import AccountPage from "@/components/AccountPage";

export const metadata = { title: "Account · optionsBot monitor" };

/** /account?bot=<name> (#394): the shared account, margin policy and Rebalance, moved off the overview. */
export default function Page() {
  return <AccountPage />;
}
