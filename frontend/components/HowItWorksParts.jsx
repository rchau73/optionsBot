// The pieces every picture of "How it works" shares (#322, #696).

// SVG geometry of pictures 2, 3 and 4.
export const W = 360;
export const PAD_X = 12;

export function Picture({ n, title, children }) {
  return (
    <section aria-label={title} className="space-y-3 rounded-lg border border-line bg-panel p-4">
      <h2 className="text-lg font-semibold">
        <span className="text-muted">{n} · </span>
        {title}
      </h2>
      {children}
    </section>
  );
}

export function Card({ title, children, loss = false }) {
  return (
    <div className="space-y-1 rounded border border-line p-3">
      <h3 className="text-base font-semibold">
        {title}
        {loss ? (
          <span className="ml-2 text-sm text-loss" aria-label="loss">
            ▼
          </span>
        ) : null}
      </h3>
      <p className="text-base">{children}</p>
    </div>
  );
}
