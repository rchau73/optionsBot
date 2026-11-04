"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { DISMISS_KEY, VISIT_AFTER_MS, VISIT_REFRESH_MS, dismissSignature, parseVisit, visitKey, visitRecord } from "@/lib/sincevisit";

// This browser's last visit per bot (#618), in localStorage only: read once
// when a bot first appears (the visit the card compares with, kept for the
// whole page view), written once the page has been open and visible for
// VISIT_AFTER_MS, then at most every VISIT_REFRESH_MS while it stays visible.
// No request, no analytics event: the stored time never leaves the browser.

const read = (key) => {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null; // private mode, blocked storage
  }
};
const write = (key, value) => {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // remembering the visit is a convenience only
  }
};
const visible = () => typeof document === "undefined" || document.visibilityState !== "hidden";

/**
 * bots: useMonitor's bots. Returns { previous (name → visit or null), loaded,
 * dismissed, dismiss }; loaded is false until storage has been read.
 */
export function useLastVisit(bots) {
  // The stored visits as they were when each bot first appeared (frozen for the page view).
  const [seen, setSeen] = useState({ names: "", previous: {}, dismissedSig: null });
  const [counted, setCounted] = useState(false);
  const written = useRef({}); // name → time of the last write
  const names = bots.map((b) => b.name).join(",");
  if (names && names !== seen.names) {
    // Derived state, set while rendering (no names on the server, so storage is read in the browser only).
    const fresh = names.split(",").filter((n) => !(n in seen.previous));
    setSeen({
      names,
      previous: { ...seen.previous, ...Object.fromEntries(fresh.map((n) => [n, parseVisit(read(visitKey(n)))])) },
      dismissedSig: read(DISMISS_KEY),
    });
  }
  const { previous, dismissedSig } = seen;
  const loaded = Boolean(seen.names);

  // The visit counts after VISIT_AFTER_MS of visible time (hidden time is not counted).
  useEffect(() => {
    let shownMs = 0;
    let since = null;
    let timer;
    const start = () => {
      since = Date.now();
      timer = setTimeout(() => setCounted(true), Math.max(0, VISIT_AFTER_MS - shownMs));
    };
    const stop = () => {
      if (since != null) shownMs += Date.now() - since;
      since = null;
      clearTimeout(timer);
    };
    const onChange = () => (visible() ? since == null && start() : stop());
    if (visible()) start();
    document.addEventListener("visibilitychange", onChange);
    return () => {
      stop();
      document.removeEventListener("visibilitychange", onChange);
    };
  }, []);

  // Once counted, store what the person sees now (only while the page is visible).
  useEffect(() => {
    if (!counted || !visible()) return;
    const now = Date.now();
    for (const b of bots) {
      if (written.current[b.name] && now - written.current[b.name] < VISIT_REFRESH_MS) continue;
      const record = visitRecord(b, now);
      if (!record) continue;
      write(visitKey(b.name), JSON.stringify(record));
      written.current[b.name] = now;
    }
  }, [bots, counted]);

  const signature = dismissSignature(previous);
  const dismiss = useCallback(() => {
    write(DISMISS_KEY, signature);
    setSeen((s) => ({ ...s, dismissedSig: signature }));
  }, [signature]);

  return { previous, loaded, dismissed: Boolean(signature) && dismissedSig === signature, dismiss };
}
