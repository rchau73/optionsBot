"use client";

import Header from "./Header";
import AccountPanel from "./AccountPanel";
import OtherPositions from "./OtherPositions";
import PrivacySettings from "./PrivacySettings";
import { useMonitor } from "@/hooks/useMonitor";
import { pickAccount, riskRows, unmanagedPositions } from "@/lib/account";

/**
 * The Account page (#394, `/account?bot=<name>`): the shared Deribit account
 * that moved off the overview — balances, the margin policy per bot with
 * Rebalance and its badge, the IM / MM meters — and the positions no bot
 * manages. The account is shared by every bot, so every bot's policy row is
 * shown; ?bot= only keeps the menu's choice. Rebalance keeps its roles and
 * confirmation (the proxy and the bot decide).
 */
export default function AccountPage() {
  const { bots, error, polledAt } = useMonitor();
  return <AccountView bots={bots} error={error} polledAt={polledAt} />;
}

/** The page for one state of the data. */
export function AccountView({ bots, error = null, polledAt }) {
  const account = pickAccount(bots, polledAt);
  const risk = riskRows(bots);
  const others = unmanagedPositions(account, bots);
  return (
    <div className="min-h-screen">
      <Header bots={bots} error={error} />
      <main className="space-y-3 p-3">
        <section aria-label="Account and collateral" className="rounded-lg border border-line bg-panel p-3">
          <h1 className="mb-2 text-lg font-bold">Account &amp; collateral (shared by all bots)</h1>
          <AccountPanel account={account} risk={risk} />
        </section>
        <section aria-label="Not managed by the bot" className="rounded-lg border border-line bg-panel p-3">
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">Not managed by the bot (still uses margin)</h2>
          <OtherPositions data={others} />
        </section>
        <section aria-label="Privacy" className="rounded-lg border border-line bg-panel p-3">
          <h2 className="mb-2 text-sm font-semibold uppercase tracking-wide text-muted">Privacy</h2>
          <PrivacySettings />
        </section>
      </main>
    </div>
  );
}
