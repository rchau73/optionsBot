"use client";

import { useEffect, useState } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { fetchConsent } from "@/lib/api";
import { screenViewOf } from "@/lib/analytics/screens";
import { setAnalyticsConsent, track } from "@/lib/track";

// The page views of the usage report (#424): reads the signed-in person's
// stored analytics answer once, then sends one screen_viewed per page shown
// (and per topic of the admin Analytics page, #473). Nothing is sent until
// that answer is a yes (lib/track.js); no answer, a no or a failed read sends
// nothing. Renders nothing. It reads the query string, so the layout wraps it
// in a Suspense boundary.
export default function ScreenTracker() {
  const pathname = usePathname();
  const search = useSearchParams();
  const [ready, setReady] = useState(false);
  const view = screenViewOf(pathname, search);
  const viewKey = view ? JSON.stringify(view) : null;

  useEffect(() => {
    const controller = new AbortController();
    fetchConsent(controller.signal).then((consent) => {
      if (controller.signal.aborted) return;
      setAnalyticsConsent(consent);
      setReady(true);
    });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (ready && viewKey) track("screen_viewed", JSON.parse(viewKey));
  }, [ready, viewKey]);

  return null;
}
