"use client";

import { useEffect, useState } from "react";
import { fetchBotIncome } from "@/lib/api/bots";

const REFRESH_MS = 60_000; // months move slowly; the history gains a line a minute

/** One bot's /api/income, refreshed every minute: { income, error, loading }. */
export function useIncome(bot) {
  const [state, setState] = useState({ bot: null, income: null, error: null });

  useEffect(() => {
    if (!bot) return undefined;
    let timer;
    const controller = new AbortController();
    const load = async () => {
      try {
        const income = await fetchBotIncome(bot, controller.signal);
        if (!controller.signal.aborted) setState({ bot, income, error: null });
      } catch (e) {
        if (!controller.signal.aborted) setState((s) => ({ bot, income: s.bot === bot ? s.income : null, error: e.message }));
      }
      if (!controller.signal.aborted) timer = setTimeout(load, REFRESH_MS);
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot]);

  if (state.bot !== bot) return { income: null, error: null, loading: Boolean(bot) };
  return { income: state.income, error: state.error, loading: false };
}
