"use client";

import clsx from "clsx";
import { useState } from "react";
import { DRAFT_HEADING, DRAFT_NOTICE, FIELDS, KIND_LABELS, MAX_FIELD, MAX_TITLE, requestFields, sentText } from "@/lib/assistantRequests";

// The draft request card (#409, E10 prototype): what the assistant drafted
// from a report with evidence, shown to the person who reported it. Nothing
// is stored or sent until they press Send for review; Edit makes every field
// editable, Discard drops the draft (nothing sent).

const BUTTON = "min-h-11 rounded-md px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const INPUT = "w-full rounded-md border border-line bg-slate-900 px-2 py-1 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

export default function RequestDraftCard({ message, assistant, onShowRequests }) {
  const { draft } = message;
  const [fields, setFields] = useState(() => requestFields(draft.kind, draft));
  const [editing, setEditing] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState(null);
  const titleOf = (citation) => message.citations?.find((c) => c.id === citation)?.title ?? citation;

  if (message.discarded) {
    return (
      <p role="status" className="text-xs text-muted">
        Draft discarded. Nothing was sent.
      </p>
    );
  }
  if (message.sentAs) {
    return (
      <div role="status" className="space-y-1 rounded-md border border-line px-3 py-2 text-sm">
        <p>{sentText(message.sentAs)}</p>
        <button type="button" onClick={onShowRequests} className={clsx(BUTTON, "border border-line")}>
          Open My requests
        </button>
      </div>
    );
  }

  const set = (key, value) => setFields((f) => ({ ...f, [key]: value }));
  const setItem = (key, i, value) => setFields((f) => ({ ...f, [key]: f[key].map((x, j) => (j === i ? value : x)) }));

  const onSend = async () => {
    setSending(true);
    setError(null);
    try {
      await assistant.sendRequest(message, draft.kind, requestFields(draft.kind, fields));
    } catch (e) {
      setError(e.message);
    } finally {
      setSending(false);
    }
  };

  return (
    <section aria-label="Draft request" className="space-y-2 rounded-md border border-line px-3 py-2" data-testid="request-draft">
      <header className="flex flex-wrap items-center gap-2">
        <span className={clsx("rounded-full px-2 py-0.5 text-xs font-semibold", draft.kind === "bug" ? "bg-loss/20 text-loss" : "bg-call/20 text-call")}>
          {KIND_LABELS[draft.kind]}
        </span>
        <h3 className="text-sm font-semibold">{DRAFT_HEADING}</h3>
      </header>

      <dl className="space-y-1 text-sm">
        {FIELDS[draft.kind].map(([key, label]) =>
          editing ? (
            <div key={key}>
              <dt>
                <label htmlFor={`draft-${message.id}-${key}`} className="text-xs text-muted">
                  {label}
                </label>
              </dt>
              <dd>
                {key === "title" ? (
                  <input id={`draft-${message.id}-${key}`} value={fields[key]} maxLength={MAX_TITLE} onChange={(e) => set(key, e.target.value)} className={INPUT} />
                ) : (
                  <textarea id={`draft-${message.id}-${key}`} value={fields[key]} maxLength={MAX_FIELD} rows={2} onChange={(e) => set(key, e.target.value)} className={INPUT} />
                )}
              </dd>
            </div>
          ) : fields[key] ? (
            <div key={key}>
              <dt className="text-xs text-muted">{label}</dt>
              <dd className="whitespace-pre-line">{fields[key]}</dd>
            </div>
          ) : null,
        )}
        {fields.evidence.length ? (
          <div>
            <dt className="text-xs text-muted">Evidence</dt>
            <dd>
              <ul className="list-disc space-y-1 pl-4">
                {fields.evidence.map((e, i) => (
                  <li key={e.citation + i}>
                    {editing ? (
                      <textarea
                        aria-label={`Evidence ${i + 1}`}
                        value={e.text}
                        maxLength={MAX_FIELD}
                        rows={2}
                        onChange={(ev) => setItem("evidence", i, { ...e, text: ev.target.value })}
                        className={INPUT}
                      />
                    ) : (
                      e.text
                    )}{" "}
                    <span className="text-xs text-muted">({titleOf(e.citation)})</span>
                  </li>
                ))}
              </ul>
            </dd>
          </div>
        ) : null}
        {fields.steps.length ? (
          <div>
            <dt className="text-xs text-muted">Steps</dt>
            <dd>
              <ol className="list-decimal space-y-1 pl-4">
                {fields.steps.map((s, i) => (
                  <li key={i}>
                    {editing ? (
                      <textarea aria-label={`Step ${i + 1}`} value={s} maxLength={MAX_FIELD} rows={1} onChange={(ev) => setItem("steps", i, ev.target.value)} className={INPUT} />
                    ) : (
                      s
                    )}
                  </li>
                ))}
              </ol>
            </dd>
          </div>
        ) : null}
      </dl>

      <p className="text-xs text-muted">{DRAFT_NOTICE}</p>
      <div className="flex flex-wrap gap-2">
        <button type="button" onClick={onSend} disabled={sending || !fields.title.trim() || !fields.what.trim()} className={clsx(BUTTON, "bg-slate-700 font-semibold text-white disabled:opacity-50")}>
          {sending ? "Sending…" : "Send for review"}
        </button>
        <button type="button" onClick={() => setEditing(!editing)} aria-pressed={editing} className={clsx(BUTTON, "border border-line")}>
          {editing ? "Done editing" : "Edit"}
        </button>
        <button type="button" onClick={() => assistant.discardDraft(message)} className={clsx(BUTTON, "border border-line")}>
          Discard
        </button>
      </div>
      {error ? (
        <p role="alert" className="text-xs text-warn">
          Not sent: {error}
        </p>
      ) : null}
    </section>
  );
}
