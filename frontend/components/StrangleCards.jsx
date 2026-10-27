"use client";

import PayoffCard from "./PayoffCard";
import { payoffStrangles } from "@/lib/payoff";

/**
 * The overview's strangles as payoff charts (#391): per bot, one compact card
 * per strangle ordered by DTE; a card opens the full chart in the modal
 * (onOpen(bot, strangleId, element)). The leg-by-leg table stays under
 * Details (pro).
 */
export default function StrangleCards({ bots, onOpen }) {
  if (!bots?.length) return null;
  return (
    <div className="space-y-3">
      {bots.map((bot) => (
        <BotStrangles key={bot.name} bot={bot} onOpen={onOpen} />
      ))}
    </div>
  );
}

function BotStrangles({ bot, onOpen }) {
  const report = bot.payoff;
  const list = payoffStrangles(report);
  const failure = bot.payoffError ?? (report ? null : bot.error);
  const title = `${bot.name.toUpperCase()} · strangles`;
  return (
    <section aria-label={title} className="space-y-2">
      <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">{title}</h2>
      {failure && !report ? (
        <p role="alert" className="text-base text-warn">
          Charts unknown: {failure}.
        </p>
      ) : !report ? (
        <p className="text-base text-muted">Loading the charts…</p>
      ) : !list.length ? (
        <p className="text-base text-muted">No open strangle right now.</p>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {list.map((sp) => (
            <PayoffCard key={sp.strangle_id} sp={sp} report={report} compact onOpen={(id, el) => onOpen?.(bot.name, id, el)} />
          ))}
        </div>
      )}
    </section>
  );
}
