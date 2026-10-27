"use client";

import { useEffect, useState } from "react";
import { fetchBotPayoff, fetchMonitorConfig } from "@/lib/api";

/** The Positions page polls no faster than this: the curves move with prices, not every second. */
export const PAYOFF_MIN_POLL_MS = 5000;

/**
 * Polls one bot's /api/strangles/payoff (#391). Returns { names, payoff, error }:
 * names = the configured bots (the coin switch), payoff = the latest answer
 * for this bot (kept while a later poll fails, error says why).
 */
export function usePayoff(bot) {
  const [names, setNames] = useState(null);
  const [pollMs, setPollMs] = useState(PAYOFF_MIN_POLL_MS);
  const [state, setState] = useState({ bot: null, payoff: null, error: null });

  useEffect(() => {
    const controller = new AbortController();
    fetchMonitorConfig(controller.signal)
      .then((c) => {
        setNames(c.names);
        setPollMs(Math.max(c.pollMs, PAYOFF_MIN_POLL_MS));
      })
      .catch((e) => {
        if (!controller.signal.aborted) setState((s) => ({ ...s, error: `monitor server: ${e.message}` }));
      });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (!bot) return undefined;
    let timer;
    const controller = new AbortController();
    const tick = async () => {
      try {
        const payoff = await fetchBotPayoff(bot, controller.signal);
        if (!controller.signal.aborted) setState({ bot, payoff, error: null });
      } catch (e) {
        if (!controller.signal.aborted) setState((s) => ({ bot, payoff: s.bot === bot ? s.payoff : null, error: e.message }));
      }
      if (!controller.signal.aborted) timer = setTimeout(tick, pollMs);
    };
    tick();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot, pollMs]);

  const current = state.bot === bot;
  return { names, payoff: current ? state.payoff : null, error: current || !bot ? state.error : null };
}
