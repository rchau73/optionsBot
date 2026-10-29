import clsx from "clsx";
import { ENV_LABELS, TRAIN_DAYS, releaseTrain, reportNow, trainLine, utcText } from "@/lib/analytics/deliveryView";

/**
 * The release train (#473): a stable lane (a mark per release) and an mvp
 * lane (a tick per deploy) over the last TRAIN_DAYS days, the open PR's dot
 * on each — filled where it went live, hollow at the right edge while it
 * waits — and one line saying where it went live. Scrolls sideways inside
 * itself on a phone. Each lane is an image whose name says what it shows.
 */
export default function ReleaseTrain({ report, env, pr }) {
  const train = releaseTrain(report, pr);
  const open = train.open;
  const dotText = (d) => (d.filled ? `#${open.number} live ${utcText(d.at)}` : `#${open.number} waiting`);
  const stableName = [
    `${ENV_LABELS.stable}: ${train.releases.length} ${train.releases.length === 1 ? "release" : "releases"} in the last ${TRAIN_DAYS} days`,
    ...train.releases.map((r) => `${r.tag} ${utcText(r.at)}${r.failed ? " (failed)" : ""}`),
    ...(open ? [dotText(open.stable)] : []),
  ].join("; ");
  const mvpName = [
    `${ENV_LABELS.mvp}: ${train.deploys.length} ${train.deploys.length === 1 ? "deploy" : "deploys"} in the last ${TRAIN_DAYS} days`,
    ...(open ? [dotText(open.mvp)] : []),
  ].join("; ");

  return (
    <section aria-labelledby="dora-train" className="rounded border border-line bg-panel p-3">
      <h2 id="dora-train" className="mb-2 font-semibold">
        Release train
      </h2>
      <div className="overflow-x-auto" tabIndex={0} aria-label="Release train, scrolls sideways">
        <div className="min-w-[36rem] space-y-2">
          <Lane title={ENV_LABELS.stable} name={stableName} current={env === "stable"}>
            {train.releases.map((r) => (
              <span
                key={r.tag}
                data-mark="release"
                className="absolute top-0 flex -translate-x-1/2 flex-col items-center"
                style={{ left: `${r.x}%` }}
              >
                <span className={clsx("h-4 w-1 rounded", r.failed ? "bg-loss" : "bg-profit")} />
                <span className="text-[11px]">{r.tag}</span>
              </span>
            ))}
            {open ? <Dot dot={open.stable} lane="stable" /> : null}
          </Lane>
          <Lane title={ENV_LABELS.mvp} name={mvpName} current={env === "mvp"}>
            {train.deploys.map((d) => (
              <span key={d.at} data-mark="deploy" className="absolute top-1 h-3 w-px bg-testnet" style={{ left: `${d.x}%` }} />
            ))}
            {open ? <Dot dot={open.mvp} lane="mvp" /> : null}
          </Lane>
          <div className="relative ml-36 h-4 text-[11px] text-muted" aria-hidden="true">
            {train.days.map((d) => (
              <span key={d.text} className="absolute -translate-x-1/2" style={{ left: `${d.x}%` }}>
                {d.text}
              </span>
            ))}
          </div>
        </div>
      </div>
      <p className="mt-2 text-sm">{trainLine(pr, reportNow(report))}</p>
    </section>
  );
}

function Lane({ title, name, current, children }) {
  return (
    <div className="flex items-center gap-2">
      <span className={clsx("w-34 shrink-0 text-xs", current ? "font-semibold" : "text-muted")}>{title}</span>
      <div role="img" aria-label={name} className="relative h-9 flex-1 border-b border-line">
        {children}
      </div>
    </div>
  );
}

/** The open PR's dot: filled where it went live, hollow while it waits. */
function Dot({ dot, lane }) {
  return (
    <span
      data-dot={lane}
      data-at={dot.at ?? ""}
      data-filled={String(dot.filled)}
      className={clsx(
        "absolute bottom-0 h-3 w-3 -translate-x-1/2 translate-y-1/2 rounded-full border-2 border-warn",
        dot.filled ? "bg-warn" : "bg-panel",
      )}
      style={{ left: `${dot.x}%` }}
    />
  );
}
