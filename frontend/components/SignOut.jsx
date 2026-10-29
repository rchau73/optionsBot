"use client";

import { signOut } from "@/lib/api/auth";

/** The user menu's "Sign out": ends this browser's session. */
export default function SignOut({ className }) {
  return (
    <button type="button" role="menuitem" className={className} onClick={() => signOut()}>
      Sign out
    </button>
  );
}
