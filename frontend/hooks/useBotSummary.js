"use client";

import { useEffect, useState } from "react";
import { fetchBotSummary, fetchMonitorConfig } from "@/lib/api/bots";

/** The worst-case page polls no faster than this: the model moves with prices, not every second. */
export const SUMMARY_MIN_POLL_MS = 5000;

/**
 * Polls one bot's /api/summary (#318). Returns { names, summary, error }:
 * names = the configured bots (for the bot picker), summary = the latest
 * answer (kept while a later poll fails, error says why).
 */
export function useBotSummary(bot) {
  const [names, setNames] = useState(null);
  const [pollMs, setPollMs] = useState(SUMMARY_MIN_POLL_MS);
  const [summary, setSummary] = useState(null);
  const [error, setError] = useState(null);

  useEffect(() => {
    const controller = new AbortController();
    fetchMonitorConfig(controller.signal)
      .then((c) => {
        setNames(c.names);
        setPollMs(Math.max(c.pollMs, SUMMARY_MIN_POLL_MS));
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(`monitor server: ${e.message}`);
      });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (!bot) return undefined;
    let timer;
    let stopped = false;
    const controller = new AbortController();
    const tick = async () => {
      try {
        const s = await fetchBotSummary(bot, controller.signal);
        if (stopped) return;
        setSummary(s);
        setError(null);
      } catch (e) {
        if (stopped) return;
        setError(e.message);
      }
      timer = setTimeout(tick, pollMs);
    };
    tick();
    return () => {
      stopped = true;
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot, pollMs]);

  return { names, summary, error };
}
