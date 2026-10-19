"use client";

import { signOut } from "@/lib/api";

/** Ends this browser's session. */
export default function SignOut() {
  return (
    <button
      type="button"
      onClick={() => signOut()}
      className="ml-auto rounded border border-line px-2 py-1 text-sm text-muted hover:text-inherit focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
    >
      Sign out
    </button>
  );
}
