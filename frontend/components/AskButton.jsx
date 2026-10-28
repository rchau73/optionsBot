"use client";

import clsx from "clsx";
import { useAssistant } from "./AssistantProvider";
import { OFF_TEXT } from "@/lib/assistantPanel";
import { botParam } from "@/lib/menu";

/**
 * The assistant's floating button (#408, #419): a chat bubble fixed at the
 * bottom-right of every signed-in page ("Ask" beside it from 640 px), opening
 * the panel about the page's bot (?bot=). Dimmed, with the reason as its
 * tooltip, while the assistant is off in this environment (the panel still
 * opens and says so). Nothing outside the assistant's provider.
 */
export default function AskButton() {
  const assistant = useAssistant();
  if (!assistant) return null;
  const off = assistant.enabled === false;
  const pageBot = () => botParam(new URLSearchParams(window.location.search).get("bot"));
  return (
    <button
      type="button"
      aria-label="Ask"
      title={off ? OFF_TEXT : "Ask the assistant about this page, the strategy or a trade"}
      aria-expanded={assistant.open}
      onClick={(e) => (assistant.open ? assistant.close() : assistant.openPanel({ entry: "menu", from: e.currentTarget, bot: pageBot() }))}
      className={clsx(
        "fixed bottom-4 right-4 z-30 inline-flex min-h-12 min-w-12 items-center justify-center gap-2 rounded-full border px-4 text-sm font-semibold shadow-lg shadow-black/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-testnet",
        off ? "border-line bg-panel text-muted opacity-50" : "border-testnet bg-sky-900 text-sky-50 hover:bg-sky-800",
      )}
    >
      <svg aria-hidden="true" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z" />
      </svg>
      <span aria-hidden="true" className="hidden sm:inline">
        Ask
      </span>
    </button>
  );
}
