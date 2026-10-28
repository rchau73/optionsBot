"use client";

import clsx from "clsx";
import { useAssistant } from "./AssistantProvider";

/**
 * Why? on a trade row (#408): opens the assistant panel and asks why the bot
 * opened or closed that leg, with the bot and the trade's journal sequence
 * number (the facts endpoint, GET /api/explain). trade: { bot, seq, kind:
 * open|close, instrument, at }. Nothing outside the assistant's provider or
 * without a bot and sequence number. compact: the small size of the one-line
 * Details (pro) trade log (#405: rows ≤ 36 px).
 */
export default function WhyButton({ trade, className, compact = false }) {
  const assistant = useAssistant();
  if (!assistant || !trade?.bot || trade.seq == null) return null;
  return (
    <button
      type="button"
      aria-label={`Why? ${trade.kind === "open" ? "Opened" : "Closed"} ${trade.instrument ?? "a leg"}`}
      onClick={(e) => assistant.askWhy(trade, e.currentTarget)}
      className={clsx(
        "inline-flex items-center justify-center rounded-md border border-line text-xs text-testnet hover:border-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet",
        compact ? "h-6 px-1.5" : "min-h-11 min-w-11 px-2",
        className,
      )}
    >
      Why?
    </button>
  );
}
