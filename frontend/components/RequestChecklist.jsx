import clsx from "clsx";

// The request check's four items under a report (#409): ticked when the
// report carries it, crossed when it is missing. A sign beside each, so
// colour is never the only signal.
export default function RequestChecklist({ checklist }) {
  if (!checklist?.length) return null;
  return (
    <ul aria-label="Request check" className="space-y-1">
      {checklist.map((item) => (
        <li key={item.key} className={clsx("flex gap-2 text-xs", item.ok ? "text-profit" : "text-warn")}>
          <span aria-hidden="true">{item.ok ? "✓" : "✗"}</span>
          <span>
            <span className="sr-only">{item.ok ? "Present: " : "Missing: "}</span>
            {item.label}
          </span>
        </li>
      ))}
    </ul>
  );
}
