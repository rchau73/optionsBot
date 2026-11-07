"use client";

import { useEffect, useState } from "react";
import { fetchHedgeState } from "@/lib/api/bots";
import { SUMMARY_MIN_POLL_MS } from "./useBotSummary";

/**
 * Polls one bot's futures hedge for "How it works" (#696). Returns
 * { hedge, error, at }: hedge = the latest reading, dropped when a poll fails
 * (error says why) so no old figure is shown as live; at = when it was read,
 * ms. Before the first answer all three are null.
 */
export function useBotHedge(bot) {
  const [state, setState] = useState({ hedge: null, error: null, at: null });

  useEffect(() => {
    if (!bot) return undefined;
    let timer;
    let stopped = false;
    const controller = new AbortController();
    const tick = async () => {
      const { hedge, error } = await fetchHedgeState(bot, controller.signal);
      if (stopped) return;
      setState({ hedge, error, at: Date.now() });
      timer = setTimeout(tick, SUMMARY_MIN_POLL_MS);
    };
    tick();
    return () => {
      stopped = true;
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot]);

  return state;
}
