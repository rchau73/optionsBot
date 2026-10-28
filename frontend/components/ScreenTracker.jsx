"use client";

import { useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import { fetchConsent } from "@/lib/api";
import { screenOf } from "@/lib/analytics/screens";
import { setAnalyticsConsent, track } from "@/lib/track";

// The page views of the usage report (#424): reads the signed-in person's
// stored analytics answer once, then sends one screen_viewed per page shown.
// Nothing is sent until that answer is a yes (lib/track.js); no answer, a no
// or a failed read sends nothing. Renders nothing.
export default function ScreenTracker() {
  const pathname = usePathname();
  const [ready, setReady] = useState(false);

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
    if (!ready) return;
    const screen = screenOf(pathname);
    if (screen) track("screen_viewed", { screen });
  }, [ready, pathname]);

  return null;
}
