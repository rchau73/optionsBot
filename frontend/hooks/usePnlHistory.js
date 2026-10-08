"use client";

import { useEffect, useState } from "react";
import { fetchPnlHistory } from "@/lib/api";
import { coinHistory, combineHistories } from "@/lib/monitor";

const REFRESH_MS = 10_000; // history buckets are at least seconds wide; no need to poll faster

/**
 * Loads and combines every bot's P&L history for `range`, refreshing every
 * 10 s. Returns { points, perBot, error, loading }: points in USD across bots,
 * perBot each bot's own history in its coin. range "live" disables it (the
 * live view comes from the per-second poll instead).
 */
export function usePnlHistory(names, range) {
  // The result remembers which range it belongs to; a mismatch means "loading".
  const [state, setState] = useState({ range: null, points: [], perBot: {}, error: null });

  useEffect(() => {
    if (!names?.length || !range || range === "live") return undefined;
    let timer;
    const controller = new AbortController();
    const load = async () => {
      const results = await Promise.allSettled(names.map((n) => fetchPnlHistory(n, range, controller.signal)));
      if (controller.signal.aborted) return;
      const ok = results.flatMap((r, i) => (r.status === "fulfilled" ? [{ ...r.value, bot: names[i] }] : []));
      const failed = results.length - ok.length;
      setState({
        range,
        points: combineHistories(ok),
        perBot: Object.fromEntries(ok.map((s) => [s.bot, coinHistory(s.points)])),
        error: failed ? `${failed} bot(s) without history` : null,
      });
      timer = setTimeout(load, REFRESH_MS);
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [names, range]);

  if (state.range !== range) return { points: [], perBot: {}, error: null, loading: Boolean(range) && range !== "live" };
  return { points: state.points, perBot: state.perBot, error: state.error, loading: false };
}
