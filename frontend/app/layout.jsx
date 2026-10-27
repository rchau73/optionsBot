import { Suspense } from "react";
import MenuBar from "@/components/MenuBar";
import { viewer } from "@/lib/auth/viewer";
import "./globals.css";

export const metadata = {
  title: "optionsBot monitor",
  description: "Live, read-only monitor of the optionsBot strategies.",
};

export default async function RootLayout({ children }) {
  // The menu bar ends with the signed-in person's menu (#394): name and role
  // from the access token, display only (the routes re-check the role).
  const me = await viewer();
  const person = me ? { username: me.username ?? null, role: me.role ?? null } : null;
  return (
    <html lang="en" className="dark">
      <body className="antialiased">
        {/* The menu reads ?bot= (useSearchParams), which needs a Suspense boundary. */}
        <Suspense fallback={null}>
          <MenuBar person={person} />
        </Suspense>
        {children}
      </body>
    </html>
  );
}
