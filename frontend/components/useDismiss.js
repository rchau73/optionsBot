"use client";

import { useEffect } from "react";

/** While open, a click outside ref's element or Escape calls close (the menu bar's dropdowns). */
export function useDismiss(ref, open, close) {
  useEffect(() => {
    if (!open) return undefined;
    const onClick = (e) => ref.current && !ref.current.contains(e.target) && close();
    const onKey = (e) => e.key === "Escape" && close();
    document.addEventListener("mousedown", onClick);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onClick);
      window.removeEventListener("keydown", onKey);
    };
  }, [ref, open, close]);
}
