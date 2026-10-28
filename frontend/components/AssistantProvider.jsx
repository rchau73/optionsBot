"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import AssistantPanel from "./AssistantPanel";
import { askAssistant, fetchAssistantState, flagAssistantAnswer } from "@/lib/api";
import { limitText, whyQuestion } from "@/lib/assistantPanel";
import { track } from "@/lib/track";

// The assistant panel's state for the whole monitor (#408): one provider in
// the root layout, so the panel stays open across page changes and the
// conversation lasts as long as this browser tab (never stored here; the
// questions and answers are in the monitor's own review log).

const AssistantContext = createContext(null);

/** The panel's controls ({ open, askWhy, enabled, … }), or null outside the provider (the Ask and Why? buttons then hide). */
export function useAssistant() {
  return useContext(AssistantContext);
}

let nextId = 1;
const newId = () => `m${nextId++}`;

/** Wraps the signed-in pages; renders the panel when open. */
export default function AssistantProvider({ children }) {
  const [open, setOpen] = useState(false);
  const [bot, setBot] = useState(null);
  const [messages, setMessages] = useState([]);
  const [enabled, setEnabled] = useState(null); // null = not known yet
  const [remaining, setRemaining] = useState(null);
  const opener = useRef(null);

  useEffect(() => {
    const controller = new AbortController();
    fetchAssistantState(controller.signal).then((s) => {
      if (controller.signal.aborted) return;
      setEnabled(s.enabled);
      if (s.remaining != null) setRemaining(s.remaining);
    });
    return () => controller.abort();
  }, []);

  const openPanel = useCallback(({ entry = "menu", from = null, bot: chosen = null } = {}) => {
    opener.current = from ?? (typeof document !== "undefined" ? document.activeElement : null);
    if (chosen) setBot(chosen);
    setOpen(true);
    track("assistant_open", { entry });
  }, []);

  const close = useCallback(() => {
    setOpen(false);
    const el = opener.current;
    opener.current = null;
    // After the panel unmounts, so focus lands on the opener, not on the body.
    setTimeout(() => el?.focus?.(), 0);
  }, []);

  const replace = (id, message) => setMessages((list) => list.map((m) => (m.id === id ? { ...message, id } : m)));

  const ask = useCallback(
    async (question, { bot: about = null, seq = null, entry = "menu" } = {}) => {
      const text = String(question ?? "").trim();
      if (!text) return;
      const pending = newId();
      const request = { question: text, bot: about, seq, entry };
      setMessages((list) => [...list, { id: newId(), role: "user", text }, { id: pending, role: "thinking" }]);
      track("assistant_ask", { entry });
      const { status, body, retryAfter } = await askAssistant(request);
      if (body?.remaining != null) setRemaining(body.remaining);
      if (status === 200) {
        replace(pending, {
          role: "assistant",
          answerId: body.id,
          text: body.answer,
          citations: body.citations ?? [],
          outcome: body.outcome,
          bot: about,
        });
      } else if (status === 429) {
        setRemaining(0);
        replace(pending, { role: "notice", kind: "limit", text: limitText(retryAfter) });
      } else if (status === 503) {
        setEnabled(false);
        replace(pending, { role: "notice", kind: "off" });
      } else {
        const why = body?.error ?? (status ? `status ${status}` : "the monitor did not answer");
        replace(pending, { role: "error", text: why, request });
      }
    },
    [],
  );

  const retry = useCallback(
    (message) => {
      setMessages((list) => list.filter((m) => m.id !== message.id));
      const { question, ...rest } = message.request;
      ask(question, rest);
    },
    [ask],
  );

  const askWhy = useCallback(
    (trade, from = null) => {
      openPanel({ entry: "why", from, bot: trade.bot });
      ask(whyQuestion(trade), { bot: trade.bot, seq: trade.seq ?? null, entry: "why" });
    },
    [openPanel, ask],
  );

  const flag = useCallback(async (message) => {
    await flagAssistantAnswer(message.answerId);
    setMessages((list) => list.map((m) => (m.id === message.id ? { ...m, flagged: true } : m)));
    track("assistant_flag", {});
  }, []);

  const value = useMemo(
    () => ({ open, bot, messages, enabled, remaining, openPanel, close, ask, askWhy, retry, flag }),
    [open, bot, messages, enabled, remaining, openPanel, close, ask, askWhy, retry, flag],
  );

  return (
    <AssistantContext.Provider value={value}>
      {children}
      {open ? <AssistantPanel assistant={value} /> : null}
    </AssistantContext.Provider>
  );
}
