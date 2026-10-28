"use client";

import clsx from "clsx";
import { useAssistant } from "./AssistantProvider";
import { OFF_TEXT } from "@/lib/assistantPanel";

/**
 * The menu bar's Ask (#408): opens the assistant panel about the page's bot.
 * Dimmed, with the reason as its tooltip, while the assistant is off in this
 * environment (the panel still opens and says so). Nothing outside the
 * assistant's provider.
 */
export default function AskButton({ bot = null }) {
  const assistant = useAssistant();
  if (!assistant) return null;
  const off = assistant.enabled === false;
  return (
    <button
      type="button"
      title={off ? OFF_TEXT : "Ask the assistant about this page, the strategy or a trade"}
      aria-expanded={assistant.open}
      onClick={(e) => (assistant.open ? assistant.close() : assistant.openPanel({ entry: "menu", from: e.currentTarget, bot }))}
      className={clsx(
        "inline-flex min-h-11 items-center rounded-full border border-line px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
        off ? "text-muted opacity-50" : "text-white hover:border-muted",
      )}
    >
      Ask
    </button>
  );
}
