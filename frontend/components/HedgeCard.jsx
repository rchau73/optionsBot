import Badge from "./Badge";
import { HedgeFigures, HedgeWords } from "./HedgeRows";
import TermTip from "./TermTip";
import { HEDGE_BADGE, hedgeState } from "@/lib/futuresHedge";

/** The perpetual's card on the Positions page, beside the strangle cards (#666). */
export default function HedgeCard({ h, coin }) {
  const unit = h.underlying || coin;
  const state = hedgeState(h);
  const badge = HEDGE_BADGE[state];
  const title = `${unit} · Futures hedge · ${h.instrument}`;
  return (
    <section aria-label={title} className="w-full space-y-2 rounded-lg border border-line bg-panel p-3" data-testid="hedge-card">
      <header className="flex flex-wrap items-center gap-2">
        <h2 className="text-base font-semibold">{title}</h2>
        <TermTip term="perpetual" />
        <Badge tone={badge.tone}>{badge.text}</Badge>
      </header>
      <HedgeWords h={h} unit={unit} state={state} />
      <HedgeFigures h={h} unit={unit} />
    </section>
  );
}
