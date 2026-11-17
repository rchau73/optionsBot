"use client";

import { useEffect, useState } from "react";
import { setAccountLinks } from "@/lib/api/users";
import { linksConsequence } from "@/lib/users";

const BUTTON = "rounded px-3 py-1 font-semibold disabled:opacity-60";

/**
 * The accounts a manager may act on (#760): one checkbox per account of the
 * registry (and per linked account the registry no longer lists), the
 * consequence of the choice in words, then Save sends the whole list. No box
 * ticked = every account of this deployment, as before links existed.
 * onDone(notice) after a save.
 */
export default function AccountLinksDialog({ user, accounts, onDone, onCancel }) {
  const current = user.accounts ?? [];
  const [picked, setPicked] = useState(current);
  const [state, setState] = useState({ phase: "edit" }); // edit | sending | error
  const busy = state.phase === "sending";
  const gone = current.filter((id) => !accounts.some((a) => a.account_id === id));
  const changed = picked.length !== current.length || picked.some((id) => !current.includes(id));

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !busy && onCancel();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel, busy]);

  const toggle = (id) => setPicked(picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id].sort());

  async function save(e) {
    e.preventDefault();
    setState({ phase: "sending" });
    try {
      const r = await setAccountLinks(user.id, picked);
      const parts = [r.added?.length ? `added ${r.added.join(", ")}` : null, r.removed?.length ? `removed ${r.removed.join(", ")}` : null].filter(Boolean);
      onDone({ tone: "good", text: `${user.username}'s accounts: ${parts.join("; ") || "no change"}.` });
    } catch (err) {
      setState({ phase: "error", error: err.message });
    }
  }

  const title = `Accounts ${user.username} manages`;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="account-links-title">
      <form onSubmit={save} className="w-full max-w-md space-y-3 rounded-lg border border-line bg-panel p-4 text-sm shadow-xl" aria-label={title}>
        <h2 id="account-links-title" className="text-base font-bold">
          {title}
        </h2>
        {accounts.length === 0 && gone.length === 0 ? (
          <p role="status" className="rounded bg-warn/15 px-2 py-1 text-warn">
            No account registry found, so no account can be linked. The monitor reads the registry&apos;s status file (data/accounts_status.json).
          </p>
        ) : (
          <fieldset className="space-y-1">
            <legend className="text-muted">Tick the accounts {user.username} may act on</legend>
            {accounts.map((a) => (
              <label key={a.account_id} className="flex min-h-11 items-center gap-2">
                <input type="checkbox" checked={picked.includes(a.account_id)} onChange={() => toggle(a.account_id)} disabled={busy} />
                <span>
                  {a.label} <span className="font-mono text-xs text-muted">{a.account_id}</span>
                  {a.environment ? <span className="text-xs text-muted"> · {a.environment}</span> : null}
                </span>
              </label>
            ))}
            {gone.map((id) => (
              <label key={id} className="flex min-h-11 items-center gap-2">
                <input type="checkbox" checked={picked.includes(id)} onChange={() => toggle(id)} disabled={busy} />
                <span>
                  <span className="font-mono text-xs">{id}</span> <span className="text-xs text-warn">no longer in the registry</span>
                </span>
              </label>
            ))}
          </fieldset>
        )}
        <p>{linksConsequence(user, picked, accounts)}</p>
        <p className="text-xs text-muted">Each account added or removed is a line of the audit trail with your username. Not for an adviser who is not the account owner until counsel has approved it.</p>
        {state.phase === "error" ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            {state.error}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onCancel} disabled={busy} className="rounded bg-slate-700 px-3 py-1">
            Cancel
          </button>
          <button type="submit" disabled={busy || !changed} className={`${BUTTON} bg-action text-white`}>
            {busy ? "Sending…" : "Save accounts"}
          </button>
        </div>
      </form>
    </div>
  );
}
