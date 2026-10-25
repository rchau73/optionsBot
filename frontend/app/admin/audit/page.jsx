import Link from "next/link";
import AuditTrail from "@/components/AuditTrail";
import UserMenu from "@/components/UserMenu";
import { viewer } from "@/lib/auth/viewer";

export const metadata = { title: "Audit · optionsBot monitor" };

/** /admin/audit (#308, #249): admins read the audit trail. GET /api/admin/audit enforces the role; this only picks the view. */
export default async function AuditPage() {
  const me = await viewer();
  const admin = me?.role === "admin";
  return (
    <>
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
        <h1 className="mr-2 text-lg font-bold">optionsBot · Audit</h1>
        <Link href="/" className="text-sm text-muted underline hover:text-inherit">
          ← Monitor
        </Link>
        <UserMenu role={me?.role ?? null} />
      </header>
      <main className="mx-auto max-w-6xl p-4">
        {admin ? (
          <AuditTrail />
        ) : (
          <p role="alert" className="rounded border border-line bg-panel px-3 py-2">
            Only an admin reads the audit trail.
          </p>
        )}
      </main>
    </>
  );
}
