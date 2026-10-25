import ResetPasswordForm from "@/components/ResetPasswordForm";

// no-referrer: the token in this page's URL never leaves in a Referer header.
export const metadata = { title: "Set a new password · optionsBot monitor", referrer: "no-referrer" };

/** /reset?token=… (#307): public like /login (proxy.js); the token is checked only when the form is sent. */
export default async function ResetPage({ searchParams }) {
  const { token } = await searchParams;
  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <ResetPasswordForm token={typeof token === "string" ? token : ""} />
    </main>
  );
}
