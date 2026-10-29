"use client";

import clsx from "clsx";
import { useEffect, useState } from "react";
import { fetchNpsState, sendNpsAction } from "@/lib/api";

/**
 * Account › Privacy (#446): the person's own switch for the monthly NPS
 * question in the assistant. Off after "Don't ask me again"; turning it on
 * only makes it possible again — the other rules (consent, 30 days since it
 * was shown, …) still decide when it is due.
 */
export default function PrivacySettings() {
  const [optedOut, setOptedOut] = useState(null); // null = not known
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    fetchNpsState(controller.signal).then((s) => {
      if (!controller.signal.aborted && typeof s?.opted_out === "boolean") setOptedOut(s.opted_out);
    });
    return () => controller.abort();
  }, []);

  const toggle = async () => {
    setSaving(true);
    setError(null);
    try {
      const s = await sendNpsAction(optedOut ? "opt_in" : "opt_out");
      setOptedOut(s.opted_out === true);
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };

  const on = optedOut === false;
  return (
    <div className="space-y-1 text-sm">
      <div className="flex items-center gap-3">
        <button
          type="button"
          role="switch"
          aria-checked={on}
          aria-labelledby="privacy-nps-label"
          disabled={optedOut === null || saving}
          onClick={toggle}
          className={clsx(
            "relative inline-flex h-7 w-12 shrink-0 items-center rounded-full border border-line focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-50",
            on ? "bg-sky-800" : "bg-slate-800",
          )}
        >
          <span aria-hidden="true" className={clsx("inline-block h-5 w-5 rounded-full bg-white", on ? "translate-x-6" : "translate-x-1")} />
        </button>
        <span id="privacy-nps-label">Monthly question in the assistant (how likely you are to recommend the monitor)</span>
        <span className="text-xs text-muted">{optedOut === null ? "unknown" : on ? "on" : "off"}</span>
      </div>
      <p className="text-xs text-muted">Optional, at most once a month, and only if you said yes to usage analytics. Your answers never include amounts.</p>
      {error ? (
        <p role="alert" className="text-xs text-warn">
          Not changed: {error}
        </p>
      ) : null}
    </div>
  );
}
