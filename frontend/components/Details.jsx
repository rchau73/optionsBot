"use client";

import { useId, useState } from "react";

/**
 * A "details" toggle and the technical text it folds (#287): the rule, its
 * numbers and the market context under a plain-words sentence. Open at first
 * when defaultOpen (managers and admins), folded otherwise.
 */
export default function Details({ defaultOpen = false, children }) {
  const [open, setOpen] = useState(defaultOpen);
  const id = useId();
  return (
    <div className="mt-0.5">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((o) => !o)}
        className="text-xs text-muted underline-offset-2 hover:text-white hover:underline focus-visible:outline focus-visible:outline-testnet"
      >
        {open ? "hide details" : "details"}
      </button>
      <div id={id} hidden={!open} className="mt-0.5 text-xs text-muted">
        {children}
      </div>
    </div>
  );
}
