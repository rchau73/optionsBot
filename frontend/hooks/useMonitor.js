"use client";

import { useEffect, useRef, useState } from "react";
import { fetchBotEvents, fetchBotState, fetchMonitorConfig } from "@/lib/api";
import { STALE_AFTER_SEC, describeEvent, mergeFeed, summarise } from "@/lib/monitor";

const HISTORY_POINTS = 900; // 15 minutes at 1 s

/**
 * Polls every configured bot (interval from the server's MONITOR_POLL_MS)
 * and keeps the latest state, a merged activity feed and a session P&L history.
 * Returns { names, bots, feed, history, error, pollMs }.
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
        if ((lastSeq.current[name] ?? 0) > counted) lastSeq.current[name] = 0;
        const events = await fetchBotEvents(name, lastSeq.current[name] ?? 0, controller.signal);
        if (events.length) lastSeq.current[name] = events[events.length - 1].seq;
        const unit = state.status.underlying ?? name.toUpperCase();
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
        const point = summarise(Object.values(next).map((b) => withStale(b, now)));
        setHistory((h) => [...h, { t: now, totalUsd: point.totalUsd, realisedUsd: point.realisedUsd }].slice(-HISTORY_POINTS));
        return next;
      });
      setFeed((f) => mergeFeed(f, results.flatMap((r) => r.lines)));
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
  return { names, bots, feed, history, error, pollMs: intervalMs, polledAt };
}

/** A bot is stale when its last good update is older than STALE_AFTER_SEC at `now`. */
export function withStale(bot, now) {
  const stale = !bot.updatedAt || now - bot.updatedAt > STALE_AFTER_SEC * 1000 || Boolean(bot.error);
  return { ...bot, stale };
}
