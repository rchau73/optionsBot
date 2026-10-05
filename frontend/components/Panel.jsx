"use client";

import { useEffect, useId, useState } from "react";

const readOpen = (key) => {
  try {
    const v = window.localStorage.getItem(key);
    return v == null ? null : v === "1";
  } catch {
    return null; // private mode, blocked storage: fall back to the default
  }
};

const writeOpen = (key, open) => {
  try {
    window.localStorage.setItem(key, open ? "1" : "0");
  } catch {
    // remembering is a convenience only
  }
};

/**
 * A titled card. With `collapsible`, the header toggles the body; `summary`
 * is shown in the header while collapsed, and the choice is remembered per
 * browser under `storageKey`.
 */
export default function Panel({ title, right, children, className = "", collapsible = false, storageKey, summary, defaultOpen = true }) {
  const [open, setOpen] = useState(defaultOpen);
  const bodyId = useId();

  useEffect(() => {
    if (!collapsible || !storageKey) return;
    const saved = readOpen(storageKey);
    if (saved != null) setOpen(saved);
  }, [collapsible, storageKey]);

  const toggle = () => {
    setOpen((o) => {
      if (storageKey) writeOpen(storageKey, !o);
      return !o;
    });
  };

  const heading = <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">{title}</h2>;
  return (
    <section className={`rounded-lg border border-line bg-panel ${className}`}>
      <header className={`flex items-center justify-between gap-3 px-4 py-2 ${open ? "border-b border-line" : ""}`}>
        {collapsible ? (
          <button
            type="button"
            onClick={toggle}
            aria-expanded={open}
            aria-controls={bodyId}
            className="flex min-w-0 flex-1 items-center gap-2 rounded text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2"
          >
            <span aria-hidden="true" className={`text-muted transition-transform ${open ? "rotate-90" : ""}`}>
              ▸
            </span>
            {heading}
            {!open && summary ? <span className="ml-2 min-w-0 truncate text-xs normal-case text-muted">{summary}</span> : null}
            <span className="sr-only">{open ? "Collapse" : "Expand"}</span>
          </button>
        ) : (
          heading
        )}
        {right}
      </header>
      {open ? (
        <div id={bodyId} className="p-3">
          {children}
        </div>
      ) : null}
    </section>
  );
}
