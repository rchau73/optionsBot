"use client";

import { useEffect, useId, useRef } from "react";
import PayoffCard from "./PayoffCard";
import { findStrangle } from "@/lib/payoff";

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * The payoff chart of one strangle in a dialog (#391), opened from a leg row,
 * a held leg or an overview card. Esc or × closes it; focus stays inside
 * while it is open and returns to `opener` (the element clicked) on close.
 * Full screen on a phone.
 *
 * report: the bot's /api/strangles/payoff answer; strangleId: the strangle;
 * error: why the answer is missing.
 */
export default function PayoffModal({ report, strangleId, error, onClose, opener }) {
  const boxRef = useRef(null);
  const closeRef = useRef(null);
  const titleId = useId();
  const sp = findStrangle(report, strangleId);

  useEffect(() => {
    const back = opener ?? (typeof document !== "undefined" ? document.activeElement : null);
    closeRef.current?.focus();
    return () => back?.focus?.();
  }, [opener]);

  const onKeyDown = (e) => {
    if (e.key === "Escape") {
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== "Tab") return;
    const items = [...(boxRef.current?.querySelectorAll(FOCUSABLE) ?? [])];
    if (!items.length) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-stretch justify-center bg-black/70 sm:items-center sm:p-4" onClick={onClose}>
      <div
        ref={boxRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onKeyDown={onKeyDown}
        onClick={(e) => e.stopPropagation()}
        className="h-full w-full space-y-3 overflow-y-auto bg-surface p-3 sm:h-auto sm:max-h-[92vh] sm:max-w-5xl sm:rounded-lg sm:border sm:border-line"
      >
        <div className="flex items-center justify-between gap-2">
          <h2 id={titleId} className="text-lg font-semibold">
            Payoff of the strangle
          </h2>
          <button
            ref={closeRef}
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="inline-flex min-h-11 min-w-11 items-center justify-center rounded text-2xl text-muted hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
          >
            ×
          </button>
        </div>
        {sp ? (
          <PayoffCard sp={sp} report={report} headingLevel="h3" />
        ) : (
          <p role="alert" className="text-base text-warn">
            Chart unknown: {error || (report ? "this strangle is no longer open" : "no answer from the bot yet")}.
          </p>
        )}
      </div>
    </div>
  );
}
