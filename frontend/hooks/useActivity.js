"use client";

import { useEffect, useState } from "react";
import { fetchBotEvents, fetchBotTrades } from "@/lib/api";
import { mergeTrades } from "@/lib/trades";

const REFRESH_MS = 30_000; // a holder's page: the bot acts every few minutes at most
const EVENTS = 500; // the bot keeps its last 500 journal events in memory (/api/events' maximum)
const TRADE_PAGE = 1000; // /api/trades' default page
const MAX_TRADE_PAGES = 20; // 20,000 opens and closes: years of history

/**
 * One bot's journal for the Activity page (#385): its last events (/api/events)
 * and every open and close (/api/trades, fetched page by page, then only the
 * new ones), refreshed every 30 s. Returns { events, trades, error, loading };
 * a failed refresh keeps the last answer and says why.
 */
export function useActivity(bot) {
  const [state, setState] = useState({ bot: null, events: [], trades: [], error: null });

  useEffect(() => {
    if (!bot) return undefined;
    let timer;
    let trades = [];
    const controller = new AbortController();
    const load = async () => {
      try {
        const events = await fetchBotEvents(bot, 0, controller.signal, EVENTS);
        // A reset journal numbers its events from 1 again: start the trades over.
        const lastEvent = events.length ? events[events.length - 1].seq : 0;
        if (trades.length && trades[trades.length - 1].seq > lastEvent) trades = [];
        for (let i = 0; i < MAX_TRADE_PAGES; i++) {
          const after = trades.length ? trades[trades.length - 1].seq : 0;
          const page = (await fetchBotTrades(bot, after, controller.signal)).trades ?? [];
          trades = mergeTrades(trades, page);
          if (page.length < TRADE_PAGE) break;
        }
        if (!controller.signal.aborted) setState({ bot, events, trades, error: null });
      } catch (e) {
        if (!controller.signal.aborted) setState((s) => (s.bot === bot ? { ...s, error: e.message } : { bot, events: [], trades: [], error: e.message }));
      }
      if (!controller.signal.aborted) timer = setTimeout(load, REFRESH_MS);
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot]);

  if (state.bot !== bot) return { events: [], trades: [], error: null, loading: Boolean(bot) };
  return { events: state.events, trades: state.trades, error: state.error, loading: false };
}
