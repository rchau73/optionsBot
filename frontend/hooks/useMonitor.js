"use client";

import { useEffect, useRef, useState } from "react";
import { fetchBotEvents, fetchBotState, fetchBotTrades, fetchMonitorConfig } from "@/lib/api/bots";
import { isHedgeEvent } from "@/lib/hedgeWords";
import { mergeTrades } from "@/lib/trades";
import { STALE_AFTER_SEC, describeEvent, mergeFeed, summarise, totalsByBot } from "@/lib/monitor";

const HISTORY_POINTS = 900; // 15 minutes at 1 s
const TRADE_PAGE = 1000; // fetchBotTrades' limit
const HEDGE_EVENTS = 500; // as many as the bot keeps in memory

/**
 * Polls every configured bot (interval from the server's MONITOR_POLL_MS)
 * and keeps the latest state, a merged activity feed and a session P&L history.
 * Returns { names, bots, feed, history, trades, tradesComplete, hedgeEvents, error, pollMs }; trades is
 * every open and close per bot from the journals (fetched incrementally);
 * tradesComplete says, per bot, that the last page has been read; hedgeEvents
 * is each bot's futures hedge lines among the events it keeps (#679).
 */
export function useMonitor() {
  const [names, setNames] = useState(null);
  const [intervalMs, setIntervalMs] = useState(1000);
  const [error, setError] = useState(null);
  const [states, setStates] = useState({});
  const [feed, setFeed] = useState([]);
  const [history, setHistory] = useState([]);
  const [polledAt, setPolledAt] = useState(0); // time of the last poll, for staleness
  const lastSeq = useRef({});
  const [trades, setTrades] = useState({}); // bot → trades, oldest first
  const tradesRef = useRef({});
  const [tradesComplete, setTradesComplete] = useState({}); // bot → its journal's trades are all in (#618)
  const completeRef = useRef({});
  const [hedgeEvents, setHedgeEvents] = useState({}); // bot → its hedge journal lines, oldest first
  const hedgeRef = useRef({});

  // Discover the bots once (retry until the server answers).
  useEffect(() => {
    let timer;
    const controller = new AbortController();
    const load = async () => {
      try {
        const config = await fetchMonitorConfig(controller.signal);
        setIntervalMs(config.pollMs);
        setNames(config.names);
        setError(null);
      } catch (e) {
        if (controller.signal.aborted) return;
        setError(`monitor server: ${e.message}`);
        timer = setTimeout(load, 2000);
      }
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, []);

  // Poll every bot.
  useEffect(() => {
    if (!names?.length) return undefined;
    let timer;
    let stopped = false;
    const controller = new AbortController();

    const pollBot = async (name) => {
      try {
        const state = await fetchBotState(name, controller.signal);
        // A restarted bot starts its event sequence again from 1.
        const counted = Object.values(state.status.event_counts ?? {}).reduce((a, b) => a + b, 0);
        if ((lastSeq.current[name] ?? 0) > counted) {
          lastSeq.current[name] = 0;
          hedgeRef.current[name] = [];
        }
        const events = await fetchBotEvents(name, lastSeq.current[name] ?? 0, controller.signal);
        if (events.length) lastSeq.current[name] = events[events.length - 1].seq;
        hedgeRef.current[name] = mergeTrades(hedgeRef.current[name] ?? [], events.filter(isHedgeEvent)).slice(-HEDGE_EVENTS);
        // Trades: only those after the last one held; a reset journal starts over.
        const unit = state.status.underlying ?? name.toUpperCase();
        let held = tradesRef.current[name] ?? [];
        if (held.length && held[held.length - 1].seq > counted) held = [];
        const page = await fetchBotTrades(name, held.length ? held[held.length - 1].seq : 0, controller.signal).catch(() => null);
        if (page) {
          tradesRef.current[name] = mergeTrades(held, (page.trades ?? []).map((t) => ({ ...t, bot: name, unit })));
          // A page shorter than the limit is the journal's last one.
          completeRef.current[name] = (page.trades ?? []).length < TRADE_PAGE;
        }
        return { name, ...state, updatedAt: Date.now(), error: null, lines: events.map((e) => describeEvent(name, e, unit)) };
      } catch (e) {
        return { name, error: e.message, lines: [] };
      }
    };

    const tick = async () => {
      const results = await Promise.all(names.map(pollBot));
      if (stopped) return;
      setStates((prev) => {
        const next = { ...prev };
        for (const r of results) {
          next[r.name] = r.error ? { ...prev[r.name], name: r.name, error: r.error } : r;
        }
        const now = Date.now();
        const current = Object.values(next).map((b) => withStale(b, now));
        const point = summarise(current);
        setHistory((h) =>
          [...h, { t: now, totalUsd: point.totalUsd, realisedUsd: point.realisedUsd, bots: totalsByBot(current) }].slice(-HISTORY_POINTS),
        );
        return next;
      });
      setFeed((f) => mergeFeed(f, results.flatMap((r) => r.lines)));
      setTrades((prev) => {
        const changed = names.some((n) => (prev[n]?.length ?? 0) !== (tradesRef.current[n]?.length ?? 0));
        return changed ? { ...tradesRef.current } : prev;
      });
      setHedgeEvents((prev) => (names.some((n) => prev[n]?.at(-1)?.seq !== hedgeRef.current[n]?.at(-1)?.seq) ? { ...hedgeRef.current } : prev));
      setTradesComplete((prev) => (names.some((n) => prev[n] !== completeRef.current[n]) ? { ...completeRef.current } : prev));
      setPolledAt(Date.now());
      timer = setTimeout(tick, intervalMs);
    };
    tick();
    return () => {
      stopped = true;
      controller.abort();
      clearTimeout(timer);
    };
  }, [names, intervalMs]);

  const bots = (names ?? []).map((n) => withStale(states[n] ?? { name: n }, polledAt));
  return { names, bots, feed, history, trades, tradesComplete, hedgeEvents, error, pollMs: intervalMs, polledAt };
}

/** A bot is stale when its last good update is older than STALE_AFTER_SEC at `now`. */
export function withStale(bot, now) {
  const stale = !bot.updatedAt || now - bot.updatedAt > STALE_AFTER_SEC * 1000 || Boolean(bot.error);
  return { ...bot, stale };
}
