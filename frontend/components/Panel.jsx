"use client";

import { useCallback, useId, useState, useSyncExternalStore } from "react";

// The remembered open/closed choice lives in localStorage; reading it through
// useSyncExternalStore keeps the server render (no storage) and the browser in
// step, and follows changes made in other tabs.
const listeners = new Set();
const subscribe = (fn) => {
  listeners.add(fn);
  window.addEventListener("storage", fn);
  return () => {
    listeners.delete(fn);
    window.removeEventListener("storage", fn);
  };
};

const readOpen = (key) => {
  try {
    return window.localStorage.getItem(key); // "1", "0" or null
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
  listeners.forEach((fn) => fn());
};

/**
 * A titled card. With `collapsible`, the header toggles the body; `summary`
 * is shown in the header while collapsed, and the choice is remembered per
 * browser under `storageKey`.
 */
export default function Panel({ title, right, children, className = "", collapsible = false, storageKey, summary, defaultOpen = true }) {
  const bodyId = useId();
  const [local, setLocal] = useState(null); // this session's choice when storage is unavailable
  const getSnapshot = useCallback(() => (collapsible && storageKey ? readOpen(storageKey) : null), [collapsible, storageKey]);
  const saved = useSyncExternalStore(subscribe, getSnapshot, () => null);
  const open = local ?? (saved == null ? defaultOpen : saved === "1");

  const toggle = () => {
    setLocal(!open);
    if (storageKey) writeOpen(storageKey, !open);
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
