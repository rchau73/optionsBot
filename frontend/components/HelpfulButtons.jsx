"use client";

import clsx from "clsx";
import { useState } from "react";

// "Did this help?" under an assistant answer (#579): Helped / Didn't help,
// one choice per answer that can be changed once. "Didn't help" offers the
// flag as an optional second step and never sends one by itself. The choice
// is the person's opinion, never a measure of the answer's correctness.

const BUTTON = "min-h-11 rounded-md px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** Only under a reply of outcome "answered" that is no request draft. */
export function takesFeedback(message) {
  return message.outcome === "answered" && !message.draft;
}

export default function HelpfulButtons({ message: m, assistant }) {
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);
  const choices = m.feedbackCount ?? 0;
  const locked = choices >= 2; // chosen, then changed once

  const choose = async (helped) => {
    if (busy || locked || m.helped === helped) return;
    setError(null);
    setBusy(true);
    try {
      await assistant.feedback(m, helped);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  };

  const onReport = async () => {
    setError(null);
    try {
      await assistant.flag(m);
    } catch (e) {
      setError(e.message);
    }
  };

  const option = (helped, label) => (
    <button
      type="button"
      aria-pressed={m.helped === helped}
      disabled={busy || (locked && m.helped !== helped)}
      onClick={() => choose(helped)}
      className={clsx(BUTTON, "min-w-11 border border-line disabled:opacity-60", m.helped === helped && "bg-slate-700 font-semibold text-white")}
    >
      {m.helped === helped ? <span aria-hidden="true">✓ </span> : null}
      {label}
    </button>
  );

  return (
    <div role="group" aria-label="Did this answer help you?" className="space-y-1">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted">Did this help?</span>
        {option(true, "Helped")}
        {option(false, "Didn't help")}
      </div>
      {m.helped === false && !m.flagged ? (
        <p className="flex flex-wrap items-center gap-2 text-xs text-muted">
          Was it wrong, not only unclear?
          <button type="button" onClick={onReport} className={clsx(BUTTON, "border border-line text-white")}>
            Report it as wrong
          </button>
        </p>
      ) : null}
      {m.helped != null && !locked ? <p className="text-xs text-muted">Thanks. The choice can be changed once.</p> : null}
      {error ? <p className="text-xs text-warn">Not saved: {error}</p> : null}
    </div>
  );
}
