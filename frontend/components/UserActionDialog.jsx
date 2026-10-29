"use client";

import clsx from "clsx";
import { useEffect, useState } from "react";
import { changeUserRole, deleteUser, resetUserPassword, signOutUser } from "@/lib/api/users";
import { confirmText, MIN_PASSWORD, passwordFormProblem } from "@/lib/users";

const INPUT =
  "mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/** Sends one confirmed action → the sentence shown on the page afterwards. */
async function run(action, user, { role, password }) {
  switch (action) {
    case "role":
      await changeUserRole(user.id, role);
      return { tone: "good", text: `${user.username} is now ${role}; their sessions ended.` };
    case "password":
      await resetUserPassword(user.id, password);
      return { tone: "good", text: `${user.username}'s password is reset; they are signed out on every browser.` };
    case "signout":
      await signOutUser(user.id);
      return { tone: "good", text: `${user.username} is signed out everywhere.` };
    case "delete": {
      const r = await deleteUser(user.id);
      const errors = [r.events_error, r.survey_answers_error].filter(Boolean);
      if (errors.length) return { tone: "warn", text: `${user.username} is deleted, but: ${errors.join("; ")}` };
      return { tone: "good", text: `${user.username} is deleted (${r.events_removed ?? 0} analytics events removed).` };
    }
    default:
      throw new Error(`unknown action ${action}`);
  }
}

/**
 * The confirmation before a role change, a password reset, a sign-out
 * everywhere or a deletion: says what follows, then sends it. A reset asks
 * for the new password (twice) in the same dialog.
 */
export default function UserActionDialog({ action, user, role = null, me, onDone, onCancel }) {
  const [form, setForm] = useState({ password: "", confirm: "" });
  const [state, setState] = useState({ phase: "confirm" }); // confirm | sending | error
  const busy = state.phase === "sending";
  const text = confirmText(action, user, { role, me });

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !busy && onCancel();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel, busy]);

  async function submit(e) {
    e.preventDefault();
    if (action === "password") {
      const problem = passwordFormProblem(form);
      if (problem) {
        setState({ phase: "error", error: problem });
        return;
      }
    }
    setState({ phase: "sending" });
    try {
      onDone(await run(action, user, { role, password: form.password }));
    } catch (err) {
      setState({ phase: "error", error: err.message });
    }
  }

  const set = (field) => (e) => setForm({ ...form, [field]: e.target.value });
  const danger = action === "delete";
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="user-action-title">
      <form onSubmit={submit} className="w-full max-w-sm space-y-3 rounded-lg border border-line bg-panel p-4 text-sm shadow-xl" aria-label={text.title}>
        <h2 id="user-action-title" className="text-base font-bold">
          {text.title}
        </h2>
        <p>{text.body}</p>
        {action === "password" ? (
          <>
            <label className="block">
              <span className="text-muted">New password</span>
              <input type="password" autoComplete="new-password" value={form.password} onChange={set("password")} className={INPUT} />
            </label>
            <label className="block">
              <span className="text-muted">Confirm new password</span>
              <input type="password" autoComplete="new-password" value={form.confirm} onChange={set("confirm")} className={INPUT} />
            </label>
            <p className="text-xs text-muted">At least {MIN_PASSWORD} characters.</p>
          </>
        ) : null}
        {state.phase === "error" ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            {state.error}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onCancel} disabled={busy} className="rounded bg-slate-700 px-3 py-1">
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy}
            className={clsx("rounded px-3 py-1 font-semibold text-white disabled:opacity-60", danger ? "bg-loss" : "bg-action")}
          >
            {busy ? "Sending…" : text.button}
          </button>
        </div>
      </form>
    </div>
  );
}
