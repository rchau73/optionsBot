"use client";

import { useEffect, useState } from "react";
import { fetchBotIncome } from "@/lib/api/bots";

const REFRESH_MS = 60_000; // months move slowly; the history gains a line a minute

/** One bot's /api/income (from: the % view's start, #629), refreshed every minute: { income, error, loading }. */
export function useIncome(bot, from = null) {
  const [state, setState] = useState({ key: null, income: null, error: null });
  const key = bot ? `${bot}|${from ?? ""}` : null;

  useEffect(() => {
    if (!bot) return undefined;
    let timer;
    const controller = new AbortController();
    const load = async () => {
      try {
        const income = await fetchBotIncome(bot, controller.signal, from);
        if (!controller.signal.aborted) setState({ key, income, error: null });
      } catch (e) {
        if (!controller.signal.aborted) setState((s) => ({ key, income: s.key === key ? s.income : null, error: e.message }));
      }
      if (!controller.signal.aborted) timer = setTimeout(load, REFRESH_MS);
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot, from, key]);

  if (state.key !== key) return { income: null, error: null, loading: Boolean(bot) };
  return { income: state.income, error: state.error, loading: false };
}
