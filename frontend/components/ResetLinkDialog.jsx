"use client";

import { useEffect, useState } from "react";
import { issueResetLink } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { confirmText } from "@/lib/users";

const BUTTON = "rounded px-3 py-1 font-semibold disabled:opacity-60";

/**
 * A reset link for `user` (#307): says what follows, creates the link, then
 * shows it once with a copy button and its expiry. Closing the dialog forgets
 * it — the monitor stores only its hash. onDone(notice) after a link was made.
 */
export default function ResetLinkDialog({ user, me, onDone, onCancel }) {
  const [state, setState] = useState({ phase: "confirm" }); // confirm | sending | shown | error
  const [copied, setCopied] = useState(false);
  const busy = state.phase === "sending";
  const text = confirmText("link", user, { me });

  const close = () => {
    if (state.phase !== "shown") return onCancel();
    return onDone({ tone: "good", text: `A reset link for ${user.username} was created; it works once, until ${formatDateTime(state.link.expires_at)}.` });
  };

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !busy && close();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  async function create(e) {
    e.preventDefault();
    setState({ phase: "sending" });
    try {
      setState({ phase: "shown", link: await issueResetLink(user.id) });
    } catch (err) {
      setState({ phase: "error", error: err.message });
    }
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(state.link.url);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="reset-link-title">
      <form onSubmit={create} className="w-full max-w-md space-y-3 rounded-lg border border-line bg-panel p-4 text-sm shadow-xl" aria-label={text.title}>
        <h2 id="reset-link-title" className="text-base font-bold">
          {text.title}
        </h2>
        {state.phase === "shown" ? (
          <>
            <p>Send this link to {user.username}. It is shown only now; closing this window forgets it.</p>
            <label className="block">
              <span className="text-muted">Reset link</span>
              <input
                readOnly
                value={state.link.url}
                onFocus={(e) => e.target.select()}
                className="mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 font-mono text-xs"
              />
            </label>
            <p className="text-xs text-muted">
              Works once, until <span className="tabular-nums">{formatDateTime(state.link.expires_at)}</span>.
            </p>
            {copied ? (
              <p role="status" className="text-xs text-profit">
                Copied.
              </p>
            ) : null}
          </>
        ) : (
          <p>{text.body}</p>
        )}
        {state.phase === "error" ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            {state.error}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          {state.phase === "shown" ? (
            <>
              <button type="button" onClick={copy} className={`${BUTTON} bg-action text-white`}>
                Copy link
              </button>
              <button type="button" onClick={close} className={`${BUTTON} bg-slate-700`}>
                Done
              </button>
            </>
          ) : (
            <>
              <button type="button" onClick={close} disabled={busy} className="rounded bg-slate-700 px-3 py-1">
                Cancel
              </button>
              <button type="submit" disabled={busy} className={`${BUTTON} bg-action text-white`}>
                {busy ? "Sending…" : text.button}
              </button>
            </>
          )}
        </div>
      </form>
    </div>
  );
}
