import { Suspense } from "react";
import MenuBar from "@/components/MenuBar";
import "./globals.css";

export const metadata = {
  title: "optionsBot monitor",
  description: "Live, read-only monitor of the optionsBot strategies.",
};

export default function RootLayout({ children }) {
  return (
    <html lang="en" className="dark">
      <body className="antialiased">
        {/* The menu reads ?bot= (useSearchParams): Suspense keeps the pages statically renderable. */}
        <Suspense fallback={null}>
          <MenuBar />
        </Suspense>
        {children}
      </body>
    </html>
  );
}
