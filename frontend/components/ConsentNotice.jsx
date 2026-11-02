"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import { COPY, noticeDue, saveConsent } from "@/lib/analytics/consent";
import { useConsent } from "@/hooks/useConsent";
import { sendConsent } from "@/lib/api/survey";
import { setAnalyticsConsent, track } from "@/lib/track";

// Yes and No share every class: equal size and weight, neither pre-selected.
const CHOICE = "min-h-11 flex-1 rounded-md border border-line bg-slate-800 px-4 text-sm font-semibold hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-60 sm:flex-none sm:min-w-24";

const DEPS = { send: sendConsent, setConsent: setAnalyticsConsent, track };

/**
 * The one-time analytics notice (#577): shown to a signed-in person whose
 * stored answer is null (never asked), above the page and never over it. It
 * has no close button — an answer closes it — and Yes and No look the same.
 * Until a yes is stored nothing is sent (lib/track.js). A failed save keeps
 * the notice with "Not saved" and a retry. Not on the sign-in or reset pages.
 * The words are in lib/analytics/consent.js.
 */
export default function ConsentNotice() {
  const pathname = usePathname();
  const consent = useConsent(); // shared with Account › Privacy: an answer there closes the notice
  const [saving, setSaving] = useState(null); // the answer being saved
  const [failed, setFailed] = useState(null); // the answer that was not saved
  const card = useRef(null);
  const shown = noticeDue(consent, pathname);

  useEffect(() => {
    if (shown) card.current?.focus();
  }, [shown]);

  if (!shown) return null;

  const answer = async (yes) => {
    setSaving(yes);
    setFailed(null);
    try {
      await saveConsent(yes, { fromNotice: true }, DEPS); // useConsent hears the stored answer
    } catch {
      setFailed(yes);
    } finally {
      setSaving(null);
    }
  };

  return (
    <section
      ref={card}
      tabIndex={-1}
      aria-labelledby="consent-notice-title"
      className="m-3 w-auto rounded-lg border border-line bg-panel p-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
    >
      <h2 id="consent-notice-title" className="mb-1 font-semibold">
        {COPY.title}
      </h2>
      <p>{COPY.draft.join(" ")}</p>
      <p className="mt-1 text-muted">{COPY.kept}</p>
      <Link href="/account" className="inline-flex min-h-11 items-center text-testnet underline-offset-2 hover:underline focus-visible:outline focus-visible:outline-2">
        {COPY.accountLink}
      </Link>
      <div className="mt-3 flex gap-3">
        <button type="button" className={CHOICE} disabled={saving !== null} onClick={() => answer(true)}>
          {saving === true ? COPY.saving : COPY.yes}
        </button>
        <button type="button" className={CHOICE} disabled={saving !== null} onClick={() => answer(false)}>
          {saving === false ? COPY.saving : COPY.no}
        </button>
      </div>
      {failed !== null ? (
        <p role="alert" className="mt-2 flex flex-wrap items-center gap-2 text-warn">
          {COPY.notSaved}
          <button type="button" className="min-h-11 rounded-md px-3 underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet" onClick={() => answer(failed)}>
            {COPY.retry}
          </button>
        </p>
      ) : null}
    </section>
  );
}
