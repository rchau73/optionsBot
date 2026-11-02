"use client";

import clsx from "clsx";
import { useState } from "react";
import { useConsent } from "@/hooks/useConsent";
import { COPY, consentStatus, saveConsent } from "@/lib/analytics/consent";
import { sendConsent } from "@/lib/api/survey";
import { setAnalyticsConsent, track } from "@/lib/track";

const DEPS = { send: sendConsent, setConsent: setAnalyticsConsent, track };

/**
 * Account › Privacy (#577): the person's analytics answer (Yes / No / Not
 * asked yet) and a switch that stores a new one. Off stops tracking at once;
 * the server's note then says events already stored stay until the person
 * deletes their account. Beside PrivacySettings (the NPS switch).
 */
export default function AnalyticsConsentSettings() {
  const consent = useConsent(); // shared with the notice: an answer there moves the switch
  const [note, setNote] = useState(null);
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);

  const toggle = async () => {
    const next = consent !== true;
    setSaving(true);
    setError(null);
    setNote(null);
    try {
      const answer = await saveConsent(next, {}, DEPS);
      if (!next && answer?.note) setNote(answer.note);
    } catch (e) {
      setError(e.message);
    } finally {
      setSaving(false);
    }
  };

  const on = consent === true;
  return (
    <div className="space-y-1 text-sm">
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          role="switch"
          aria-checked={on}
          aria-labelledby="privacy-analytics-label"
          disabled={consent === undefined || saving}
          onClick={toggle}
          className={clsx(
            "relative inline-flex h-7 w-12 shrink-0 items-center rounded-full border border-line focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-50",
            on ? "bg-sky-800" : "bg-slate-800",
          )}
        >
          <span aria-hidden="true" className={clsx("inline-block h-5 w-5 rounded-full bg-white", on ? "translate-x-6" : "translate-x-1")} />
        </button>
        <span id="privacy-analytics-label">{COPY.rowLabel}</span>
        <span className="text-xs text-muted">{consentStatus(consent)}</span>
      </div>
      <p className="text-xs text-muted">{COPY.rowHelp}</p>
      {note ? (
        <p role="status" className="text-xs">
          {note} {COPY.deleteHint}
        </p>
      ) : null}
      {error ? (
        <p role="alert" className="text-xs text-warn">
          {COPY.notSaved}: {error}
        </p>
      ) : null}
    </div>
  );
}
