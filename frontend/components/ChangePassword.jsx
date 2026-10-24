"use client";

import { useEffect, useState } from "react";
import { changePassword } from "@/lib/api";

/** The sign-in rule (lib/auth/password.js MIN_PASSWORD_LENGTH; that file is server-only). */
export const MIN_LENGTH = 12;

/** What is wrong with the form before it is sent, in plain words, or null. */
export function formProblem({ current, next, confirm }) {
  if (!current) return "Enter your current password";
  if (next.length < MIN_LENGTH) return `Too short: at least ${MIN_LENGTH} characters`;
  if (next === current) return "The new password must differ from the current one";
  if (next !== confirm) return "The two new passwords do not match";
  return null;
}

const INPUT =
  "mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/**
 * Change your own password: current, new and confirm. On success this browser
 * stays signed in and every other session of yours is signed out.
 */
export default function ChangePasswordModal({ onClose }) {
  const [form, setForm] = useState({ current: "", next: "", confirm: "" });
  const [state, setState] = useState({ phase: "form" }); // form | sending | done | error
  const busy = state.phase === "sending";

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !busy && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, busy]);

  const set = (field) => (e) => setForm({ ...form, [field]: e.target.value });

  async function submit(e) {
    e.preventDefault();
    const problem = formProblem(form);
    if (problem) {
      setState({ phase: "error", error: problem });
      return;
    }
    setState({ phase: "sending" });
    try {
      await changePassword(form.current, form.next);
      setState({ phase: "done" });
    } catch (err) {
      setForm({ ...form, current: "" });
      setState({ phase: "error", error: err.message });
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="password-title">
      <form onSubmit={submit} className="w-full max-w-sm space-y-3 rounded-lg border border-line bg-panel p-4 text-sm shadow-xl" aria-label="Change password">
        <h2 id="password-title" className="text-base font-bold">
          Change password
        </h2>
        {state.phase === "done" ? (
          <p className="rounded bg-profit/10 px-2 py-1 text-profit" role="status">
            Password changed. You stay signed in here; every other browser of yours is signed out.
          </p>
        ) : (
          <>
            <label className="block">
              <span className="text-muted">Current password</span>
              <input type="password" autoComplete="current-password" value={form.current} onChange={set("current")} className={INPUT} />
            </label>
            <label className="block">
              <span className="text-muted">New password</span>
              <input type="password" autoComplete="new-password" value={form.next} onChange={set("next")} className={INPUT} aria-describedby="password-rule" />
            </label>
            <label className="block">
              <span className="text-muted">Confirm new password</span>
              <input type="password" autoComplete="new-password" value={form.confirm} onChange={set("confirm")} className={INPUT} />
            </label>
            <p id="password-rule" className="text-xs text-muted">
              At least {MIN_LENGTH} characters, different from the current one. Every other browser of yours is signed out.
            </p>
            {state.phase === "error" ? (
              <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
                {state.error}
              </p>
            ) : null}
          </>
        )}
        <div className="flex justify-end gap-2">
          {state.phase === "done" ? (
            <button type="button" onClick={onClose} className="rounded bg-slate-700 px-3 py-1 font-semibold">
              Close window
            </button>
          ) : (
            <>
              <button type="button" onClick={onClose} disabled={busy} className="rounded bg-slate-700 px-3 py-1">
                Cancel
              </button>
              <button type="submit" disabled={busy} className="rounded bg-action px-3 py-1 font-semibold text-white disabled:opacity-60">
                {busy ? "Saving…" : "Change password"}
              </button>
            </>
          )}
        </div>
      </form>
    </div>
  );
}
