import "./globals.css";

export const metadata = {
  title: "optionsBot monitor",
  description: "Live, read-only monitor of the optionsBot strategies.",
};

export default function RootLayout({ children }) {
  return (
    <html lang="en" className="dark">
      <body className="antialiased">{children}</body>
    </html>
  );
}
