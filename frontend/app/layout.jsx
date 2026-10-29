import { Suspense } from "react";
import AssistantProvider from "@/components/AssistantProvider";
import MenuBar from "@/components/MenuBar";
import ScreenTracker from "@/components/ScreenTracker";
import { viewer } from "@/lib/auth/viewer";
import "./globals.css";

export const metadata = {
  title: "optionsBot monitor",
  description: "Live, read-only monitor of the optionsBot strategies.",
};

/** The assistant's provider for a signed-in person; the signed-out pages render as they are. */
function AssistantRoot({ signedIn, children }) {
  return signedIn ? <AssistantProvider>{children}</AssistantProvider> : children;
}

export default async function RootLayout({ children }) {
  // The menu bar ends with the signed-in person's menu (#394): name and role
  // from the access token, display only (the routes re-check the role).
  const me = await viewer();
  const person = me ? { username: me.username ?? null, role: me.role ?? null } : null;
  return (
    <html lang="en" className="dark">
      <body className="antialiased">
        {/* The assistant panel (#408) lives above the pages, so it stays open across them; signed in only. */}
        <AssistantRoot signedIn={Boolean(me)}>
          {/* The menu reads ?bot= (useSearchParams), which needs a Suspense boundary. */}
          <Suspense fallback={null}>
            <MenuBar person={person} />
          </Suspense>
          {/* Page views for the usage report (#424), after the person's yes only; signed in only. It reads ?topic= (#473). */}
          {me ? (
            <Suspense fallback={null}>
              <ScreenTracker />
            </Suspense>
          ) : null}
          {children}
        </AssistantRoot>
      </body>
    </html>
  );
}
