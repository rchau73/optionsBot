import clsx from "clsx";

const TONES = {
  neutral: "bg-slate-700/60 text-slate-200",
  good: "bg-profit/15 text-profit",
  bad: "bg-loss/15 text-loss",
  warn: "bg-warn/15 text-warn",
  muted: "bg-slate-800 text-muted",
  live: "bg-live text-white",
  testnet: "bg-testnet/20 text-testnet",
  call: "bg-call/15 text-call",
  put: "bg-put/15 text-put",
};

export default function Badge({ tone = "neutral", children, title }) {
  return (
    <span title={title} className={clsx("inline-block rounded px-1.5 py-0.5 text-xs font-medium", TONES[tone] ?? TONES.neutral)}>
      {children}
    </span>
  );
}
