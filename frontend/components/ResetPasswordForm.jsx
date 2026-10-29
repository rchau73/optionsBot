"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { resetWithLink } from "@/lib/api/auth";
import { MIN_PASSWORD, passwordFormProblem } from "@/lib/users";

const INPUT =
  "mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

/**
 * The /reset page's form (#307): a new password and its confirmation, sent
 * with the link's token. The token is dropped from the address bar once the
 * page has it, so it is not left in the browser's history. Success signs the
 * person out everywhere; they sign in with the new password.
 */
export default function ResetPasswordForm({ token }) {
  const [form, setForm] = useState({ password: "", confirm: "" });
  const [state, setState] = useState({ phase: "form" }); // form | sending | done | error
  const busy = state.phase === "sending";

  useEffect(() => {
    if (window.location.search) window.history.replaceState(null, "", window.location.pathname);
  }, []);

  const set = (field) => (e) => setForm({ ...form, [field]: e.target.value });

  async function submit(e) {
    e.preventDefault();
    const problem = passwordFormProblem(form);
    if (problem) {
      setState({ phase: "error", error: problem });
      return;
    }
    setState({ phase: "sending" });
    try {
      await resetWithLink(token, form.password);
      setState({ phase: "done" });
    } catch (err) {
      setState({ phase: "error", error: err.message });
    }
  }

  return (
    <form onSubmit={submit} className="w-full max-w-sm space-y-4 rounded-lg border border-line bg-panel p-6 text-sm" aria-label="Set a new password">
      <h1 className="text-lg font-bold">optionsBot · Set a new password</h1>
      {!token ? (
        <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
          This link is incomplete. Ask an admin for a new reset link.
        </p>
      ) : state.phase === "done" ? (
        <>
          <p role="status" className="rounded bg-profit/10 px-2 py-1 text-profit">
            Password set. Every browser of yours is signed out: sign in with the new password.
          </p>
          <Link href="/login" className="block w-full rounded border border-line px-3 py-1.5 text-center font-semibold hover:bg-line/40">
            Sign in
          </Link>
        </>
      ) : (
        <>
          <label className="block">
            <span className="text-muted">New password</span>
            <input type="password" autoComplete="new-password" value={form.password} onChange={set("password")} className={INPUT} aria-describedby="reset-rule" />
          </label>
          <label className="block">
            <span className="text-muted">Confirm new password</span>
            <input type="password" autoComplete="new-password" value={form.confirm} onChange={set("confirm")} className={INPUT} />
          </label>
          <p id="reset-rule" className="text-xs text-muted">
            At least {MIN_PASSWORD} characters. The link works once; afterwards every browser of yours is signed out.
          </p>
          {state.phase === "error" ? (
            <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
              {state.error}
            </p>
          ) : null}
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded border border-line px-3 py-1.5 font-semibold hover:bg-line/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-50"
          >
            {busy ? "Saving…" : "Set password"}
          </button>
        </>
      )}
    </form>
  );
}
