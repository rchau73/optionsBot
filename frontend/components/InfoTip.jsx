"use client";

import { useId, useState } from "react";

/**
 * A tooltip that works without a mouse (#394): an "i" button (44 px) that
 * shows text on hover, on focus and on a tap, linked by aria-describedby. Used
 * for the stat tiles' plain-words sentences, so nothing is hover-only.
 */
export default function InfoTip({ label, children }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <span className="relative inline-flex">
      <button
        type="button"
        aria-label={label}
        aria-expanded={open}
        aria-describedby={open ? id : undefined}
        onClick={() => setOpen(!open)}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onBlur={() => setOpen(false)}
        onKeyDown={(e) => e.key === "Escape" && setOpen(false)}
        className="-m-3 inline-flex min-h-11 min-w-11 items-center justify-center rounded-full text-xs text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-current">
          i
        </span>
      </button>
      {open ? (
        <span id={id} role="tooltip" className="absolute right-0 top-6 z-30 w-64 rounded border border-line bg-panel px-3 py-2 text-xs normal-case tracking-normal text-slate-200 shadow-xl">
          {children}
        </span>
      ) : null}
    </span>
  );
}
