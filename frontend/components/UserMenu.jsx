"use client";

import Link from "next/link";
import { useCallback, useRef, useState } from "react";
import ChangePasswordModal from "./ChangePassword";
import SignOut from "./SignOut";
import { useDismiss } from "./useDismiss";
import { ADMIN_PAGES } from "@/lib/menu";

const ITEM = "block w-full px-3 py-1.5 text-left hover:bg-line/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/**
 * The signed-in person's menu, the menu bar's last item (#394), labelled with
 * their name ("bob ▾"; "Account ▾" when the name cannot be read): Users,
 * Audit and Analytics (admins only, #306, #308, #445 — links only, the routes
 * enforce the role),
 * change your password, sign out of this browser.
 */
export default function UserMenu({ role = null, username = null }) {
  const [open, setOpen] = useState(false);
  const [changing, setChanging] = useState(false);
  const ref = useRef(null);

  // A click outside or Escape closes the menu.
  useDismiss(ref, open, useCallback(() => setOpen(false), []));

  return (
    <div ref={ref} className="relative text-sm">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="inline-flex min-h-11 items-center rounded-full px-3 text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
      >
        {username || "Account"} ▾
      </button>
      {open ? (
        <div role="menu" className="absolute right-0 z-40 mt-1 w-44 rounded border border-line bg-panel py-1 shadow-xl">
          {role === "admin"
            ? ADMIN_PAGES.map((p) => (
                <Link key={p.key} role="menuitem" href={p.href} className={ITEM}>
                  {p.label}
                </Link>
              ))
            : null}
          <button
            type="button"
            role="menuitem"
            className={ITEM}
            onClick={() => {
              setOpen(false);
              setChanging(true);
            }}
          >
            Change password
          </button>
          <SignOut className={ITEM} />
        </div>
      ) : null}
      {changing ? <ChangePasswordModal onClose={() => setChanging(false)} /> : null}
    </div>
  );
}
