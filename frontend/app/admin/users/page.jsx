import Link from "next/link";
import UserMenu from "@/components/UserMenu";
import UsersAdmin from "@/components/UsersAdmin";
import { viewer } from "@/lib/auth/viewer";

export const metadata = { title: "Users · optionsBot monitor" };

/** /admin/users (#306): admins manage people. The /api/admin/users routes enforce the role; this only picks the view. */
export default async function UsersPage() {
  const me = await viewer();
  const admin = me?.role === "admin";
  return (
    <>
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-4 py-3">
        <h1 className="mr-2 text-lg font-bold">optionsBot · Users</h1>
        <Link href="/" className="text-sm text-muted underline hover:text-inherit">
          ← Monitor
        </Link>
        <UserMenu role={me?.role ?? null} />
      </header>
      <main className="mx-auto max-w-6xl p-4">
        {admin ? (
          <UsersAdmin me={me.username} />
        ) : (
          <p role="alert" className="rounded border border-line bg-panel px-3 py-2">
            Only an admin manages users. Ask an admin to add a person, change a role or reset a password.
          </p>
        )}
      </main>
    </>
  );
}
