"use client";

import { useCallback, useState, useSyncExternalStore } from "react";

// A choice remembered per browser in localStorage (#394: the P&L charts'
// range), read through useSyncExternalStore like Panel's open/closed state, so
// the server render (no storage) and the browser agree. A stored value that
// is no longer allowed falls back to the default.

const listeners = new Set();
const subscribe = (fn) => {
  listeners.add(fn);
  window.addEventListener("storage", fn);
  return () => {
    listeners.delete(fn);
    window.removeEventListener("storage", fn);
  };
};

const read = (key) => {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null; // private mode, blocked storage
  }
};

/** [value, setValue]: the saved choice under key when it is one of allowed, else fallback. */
export function useStoredChoice(key, fallback, allowed) {
  const [local, setLocal] = useState(null); // this session's choice when storage is unavailable
  const getSnapshot = useCallback(() => read(key), [key]);
  const saved = useSyncExternalStore(subscribe, getSnapshot, () => null);
  const ok = (v) => v != null && (!allowed || allowed.includes(v));
  const value = ok(local) ? local : ok(saved) ? saved : fallback;
  const set = useCallback(
    (v) => {
      setLocal(v);
      try {
        window.localStorage.setItem(key, v);
      } catch {
        // remembering is a convenience only
      }
      listeners.forEach((fn) => fn());
    },
    [key],
  );
  return [value, set];
}
