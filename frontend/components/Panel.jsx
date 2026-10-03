export default function Panel({ title, right, children, className = "" }) {
  return (
    <section className={`rounded-lg border border-line bg-panel ${className}`}>
      <header className="flex items-center justify-between border-b border-line px-4 py-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted">{title}</h2>
        {right}
      </header>
      <div className="p-3">{children}</div>
    </section>
  );
}
