import clsx from "clsx";
import { coinOf, firstDaySteps, startedAgo } from "@/lib/overview";

// The first-day panel (#321): a brand-new bot — nothing held, no trade in its
// journal — shows what is done, what it does now and what appears next,
// instead of an empty summary. The first fill replaces it with the summary.

const ICONS = { done: "✓", now: "◷", next: "→", then: "→" };

/**
 * The panel for one bot (`isFirstDay(bot)` decides when it is shown). Its
 * headline is an h2 under the monitor's header, the page's h1 on a phone.
 */
export default function FirstDayPanel({ bot, headingLevel = 2 }) {
  const coin = coinOf(bot);
  const ago = startedAgo(bot.status?.uptime_sec);
  const steps = firstDaySteps(bot);
  const Heading = headingLevel === 1 ? "h1" : "h2";
  return (
    <section aria-label={`${coin} · first day`} className="space-y-3 rounded-lg border border-line bg-panel p-4">
      <Heading className="text-xl font-semibold">
        Your {coin} bot started {ago ? `${ago} ago` : "(start time unknown)"}
      </Heading>
      <p className="text-base">It&apos;s reading the market before its first trade. Here is what will appear on this page, and when.</p>
      <ol className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {steps.map((s) => (
          <li
            key={s.key}
            data-state={s.state}
            aria-current={s.state === "now" ? "step" : undefined}
            className={clsx(
              "space-y-1 rounded-lg border p-3",
              s.state === "done" && "border-line",
              s.state === "now" && "border-2 border-testnet",
              (s.state === "next" || s.state === "then") && "border-dashed border-line",
            )}
          >
            <p className={clsx("text-xs font-semibold uppercase tracking-wide", s.state === "done" ? "text-profit" : s.state === "now" ? "text-testnet" : "text-muted")}>
              <span aria-hidden="true">{ICONS[s.state]} </span>
              {s.when}
            </p>
            <h3 className="text-base font-semibold">{s.title}</h3>
            <p className="text-sm">{s.text}</p>
          </li>
        ))}
      </ol>
      <div className="rounded-lg border border-line p-3">
        <h3 className="text-base font-semibold">While you wait: how it earns, in 60 seconds</h3>
        <p className="text-sm">
          Income comes from being paid to take on a risk most holders avoid: big, fast moves. Months with calm markets pay; a crash month can lose. The
          first month&apos;s result says little; look at several.
        </p>
      </div>
    </section>
  );
}
