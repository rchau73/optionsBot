"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import ChangePasswordModal from "./ChangePassword";
import SignOut from "./SignOut";

const ITEM = "block w-full px-3 py-1.5 text-left hover:bg-line/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/**
 * The header's user menu: Users (admins only, #306 — a link only, the routes
 * enforce the role), change your password, sign out of this browser.
 */
export default function UserMenu({ role = null }) {
  const [open, setOpen] = useState(false);
  const [changing, setChanging] = useState(false);
  const ref = useRef(null);

  // A click outside or Escape closes the menu.
  useEffect(() => {
    if (!open) return undefined;
    const onClick = (e) => ref.current && !ref.current.contains(e.target) && setOpen(false);
    const onKey = (e) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onClick);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onClick);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative ml-auto text-sm">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="rounded border border-line px-2 py-1 text-muted hover:text-inherit focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
      >
        Account ▾
      </button>
      {open ? (
        <div role="menu" className="absolute right-0 z-40 mt-1 w-44 rounded border border-line bg-panel py-1 shadow-xl">
          {role === "admin" ? (
            <Link role="menuitem" href="/admin/users" className={ITEM}>
              Users
            </Link>
          ) : null}
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
