"use client";

import { useEffect, useState } from "react";
import { onConsentSaved } from "@/lib/analytics/consent";
import { fetchConsent } from "@/lib/api/survey";

/**
 * The person's analytics answer (#577) — true, false,
 * null (never asked) or undefined (not read yet, or unreadable). Read once from
 * GET /api/consent, then kept in step with every answer saveConsent stores in
 * this tab, so the notice and Account › Privacy never disagree. A read that
 * arrives after an answer was stored does not overwrite it.
 */
export function useConsent() {
  const [consent, setConsent] = useState(undefined);

  useEffect(() => {
    const controller = new AbortController();
    const off = onConsentSaved((c) => setConsent(c));
    fetchConsent(controller.signal).then((c) => {
      if (!controller.signal.aborted) setConsent((prev) => (prev === undefined ? c : prev));
    });
    return () => {
      off();
      controller.abort();
    };
  }, []);

  return consent;
}
