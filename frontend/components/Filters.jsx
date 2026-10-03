"use client";

const GROUPS = [
  ["strategy", "Strategy"],
  ["slot", "Slot (DTE · Δ)"],
  ["expiry", "Expiry"],
  ["type", "Call / put"],
  ["none", "No grouping"],
];

function Select({ label, value, options, onChange }) {
  return (
    <label className="flex items-center gap-1 text-xs text-muted">
      {label}
      <select
        className="rounded border border-line bg-surface px-2 py-1 text-sm text-slate-100 focus-visible:outline focus-visible:outline-testnet"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {options.map(([v, text]) => (
          <option key={v} value={v}>
            {text}
          </option>
        ))}
      </select>
    </label>
  );
}

/** Filter and group controls for the positions table. */
export default function Filters({ filters, options, onChange }) {
  const set = (key) => (value) => onChange({ ...filters, [key]: value });
  const all = (values) => [["", "All"], ...values.map((v) => [v, v])];
  return (
    <div className="flex flex-wrap items-center gap-3">
      <Select label="Bot" value={filters.bot} options={all(options.bots)} onChange={set("bot")} />
      <Select label="Strategy" value={filters.strategy} options={all(options.strategies)} onChange={set("strategy")} />
      <Select label="Type" value={filters.type} options={all(["call", "put"])} onChange={set("type")} />
      <Select label="Moneyness" value={filters.moneyness} options={all(["OTM", "ATM", "ITM"])} onChange={set("moneyness")} />
      <Select label="Group by" value={filters.groupBy} options={GROUPS} onChange={set("groupBy")} />
    </div>
  );
}
