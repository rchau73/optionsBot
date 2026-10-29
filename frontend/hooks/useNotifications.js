"use client";

import { useCallback, useEffect, useState } from "react";
import { fetchNotifications, sendNotificationAction } from "@/lib/api/notifications";

const REFRESH_MS = 60_000; // nothing here is urgent: no "act now", no push

/**
 * The signed-in person's bell for the page's bot (#447), refreshed every
 * minute: { view, error, loading, markRead(ids), markAllRead(), setSetting(kind, enabled) }.
 * view: { items, needs_you, settings, errors }. Each change goes to the
 * server (read marks are per person in auth.db) and shows the view it
 * returns; a failed change throws to the caller and keeps the last view.
 */
export function useNotifications(bot) {
  const [state, setState] = useState({ view: null, error: null, loading: true });

  useEffect(() => {
    let timer;
    const controller = new AbortController();
    const load = async () => {
      try {
        const view = await fetchNotifications(bot, controller.signal);
        if (!controller.signal.aborted) setState({ view, error: null, loading: false });
      } catch (e) {
        if (!controller.signal.aborted) setState((s) => ({ view: s.view, error: e.message, loading: false }));
      }
      if (!controller.signal.aborted) timer = setTimeout(load, REFRESH_MS);
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [bot]);

  const send = useCallback(
    async (body) => {
      const view = await sendNotificationAction(bot, body);
      setState({ view, error: null, loading: false });
      return view;
    },
    [bot],
  );
  const markRead = useCallback((ids) => (ids.length ? send({ action: "read", ids }) : Promise.resolve(null)), [send]);
  const markAllRead = useCallback(() => send({ action: "read_all" }), [send]);
  const setSetting = useCallback((kind, enabled) => send({ action: "setting", kind, enabled }), [send]);

  return { ...state, markRead, markAllRead, setSetting };
}
