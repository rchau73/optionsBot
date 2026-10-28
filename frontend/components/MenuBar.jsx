"use client";

import clsx from "clsx";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import AskButton from "./AskButton";
import UserMenu from "./UserMenu";
import { fetchMonitorConfig } from "@/lib/api";
import { botParam, menuItems, showMenu } from "@/lib/menu";

/**
 * The menu bar on every signed-in page (#385): Overview · Income · Worst case
 * · Positions · How it works · Activity · Account · Details (pro), then Ask
 * (the assistant panel, #408) and the signed-in person's menu ("bob ▾": Users and Audit for admins, Change
 * password, Sign out; #394). The bot is the page's ?bot=, else the first
 * configured bot, and every link keeps it. Not shown on sign-in or the
 * reset-link page. person: { username, role } from the layout (the access
 * token), null when it cannot be read.
 */
export default function MenuBar({ person = null }) {
  const pathname = usePathname() ?? "/";
  const params = useSearchParams();
  const shown = showMenu(pathname);
  const bot = botParam(params?.get("bot"));
  const first = useFirstBot(shown && !bot);
  if (!shown) return null;
  return <MenuNav pathname={pathname} bot={bot ?? first} person={person} />;
}

/** The first configured bot, fetched once when wanted (no ?bot= on the page). */
function useFirstBot(wanted) {
  const [first, setFirst] = useState(null);
  useEffect(() => {
    if (!wanted) return undefined;
    const controller = new AbortController();
    fetchMonitorConfig(controller.signal)
      .then((c) => setFirst(botParam(c.names[0])))
      .catch(() => {}); // the links then go without ?bot=; each page offers its bot picker
    return () => controller.abort();
  }, [wanted]);
  return first;
}

/** Pure render of the menu, for tests. */
export function MenuNav({ pathname, bot = null, person = null }) {
  return (
    <nav aria-label="Main" className="border-b border-line bg-panel px-2 py-1 sm:px-4">
      <ul className="flex flex-wrap items-center gap-1">
        {menuItems(pathname, { bot }).map((item) => (
          <li key={item.key}>
            <Link
              href={item.href}
              aria-current={item.current ? "page" : undefined}
              className={clsx(
                "inline-flex min-h-11 items-center rounded-full px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
                item.current ? "bg-slate-700 font-semibold text-white" : "text-muted hover:text-white",
              )}
            >
              {item.label}
            </Link>
          </li>
        ))}
        <li className="ml-auto">
          <AskButton bot={bot} />
        </li>
        <li>
          <UserMenu role={person?.role ?? null} username={person?.username ?? null} />
        </li>
      </ul>
    </nav>
  );
}
