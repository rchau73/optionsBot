"use client";

import clsx from "clsx";
import { useAssistant } from "./AssistantProvider";
import { OFF_TEXT } from "@/lib/assistantPanel";
import { botParam } from "@/lib/menu";
import { NPS_ASK_HINT, NPS_ASK_LABEL } from "@/lib/analytics/nps";

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
  // The monthly NPS (#446): a static "1" and a one-line hint while it is due — no motion.
  const nps = assistant.npsDue === true && !assistant.open;
  const pageBot = () => botParam(new URLSearchParams(window.location.search).get("bot"));
  return (
    <button
      type="button"
      aria-label={nps ? NPS_ASK_LABEL : "Ask"}
      title={off ? OFF_TEXT : nps ? NPS_ASK_HINT : "Ask the assistant about this page, the strategy or a trade"}
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
      {nps ? (
        <>
          <span data-testid="nps-badge" aria-hidden="true" className="absolute -right-1 -top-1 inline-flex h-5 min-w-5 items-center justify-center rounded-full border border-warn bg-panel px-1 text-xs font-bold text-warn">
            1
          </span>
          <span aria-hidden="true" className="absolute bottom-full right-0 mb-2 whitespace-nowrap rounded border border-line bg-panel px-2 py-1 text-xs font-normal text-muted">
            {NPS_ASK_HINT}
          </span>
        </>
      ) : null}
    </button>
  );
}
