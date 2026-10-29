"use client";

import clsx from "clsx";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import NotificationBell from "./NotificationBell";
import UserMenu from "./UserMenu";
import { useDismiss } from "./useDismiss";
import { fetchMonitorConfig } from "@/lib/api/bots";
import { botParam, menuGroups, showMenu } from "@/lib/menu";

/**
 * The menu bar on every signed-in page (#385, #450): Overview · Details (pro)
 * · Operational ▾ (Positions, Income, Worst case, Activity, Account) · Help ▾
 * (How it works), then the notification bell (#447) and the signed-in person's menu ("bob ▾": Users and Audit for admins, Change
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

const PILL = "inline-flex min-h-11 items-center rounded-full px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const pill = (current) => clsx(PILL, current ? "bg-slate-700 font-semibold text-white" : "text-muted hover:text-white");

/** Pure render of the menu, for tests. */
export function MenuNav({ pathname, bot = null, person = null }) {
  return (
    <nav aria-label="Main" className="border-b border-line bg-panel px-2 py-1 sm:px-4">
      <ul className="flex flex-wrap items-center gap-1">
        {menuGroups(pathname, { bot }).map((entry) => (
          <li key={entry.key}>
            {entry.items ? (
              <MenuGroup group={entry} />
            ) : (
              <Link href={entry.href} aria-current={entry.current ? "page" : undefined} className={pill(entry.current)}>
                {entry.label}
              </Link>
            )}
          </li>
        ))}
        <li className="ml-auto">
          <NotificationBell bot={bot} />
        </li>
        <li>
          <UserMenu role={person?.role ?? null} username={person?.username ?? null} />
        </li>
      </ul>
    </nav>
  );
}

/** A dropdown of the bar ("Operational ▾"), filled when one of its pages is the current one (#450). */
function MenuGroup({ group }) {
  const [open, setOpen] = useState(false);
  const ref = useRef(null);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(ref, open, close);
  return (
    <div ref={ref} className="relative">
      <button type="button" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)} className={pill(group.current)}>
        {group.label} ▾
      </button>
      {open ? (
        <div role="menu" aria-label={group.label} className="absolute left-0 z-40 mt-1 w-44 rounded border border-line bg-panel py-1 text-sm shadow-xl">
          {group.items.map((item) => (
            <Link
              key={item.key}
              role="menuitem"
              href={item.href}
              aria-current={item.current ? "page" : undefined}
              onClick={close}
              className={clsx(
                "flex min-h-11 w-full items-center px-3 hover:bg-line/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
                item.current ? "font-semibold text-white" : "text-muted",
              )}
            >
              {item.label}
            </Link>
          ))}
        </div>
      ) : null}
    </div>
  );
}
