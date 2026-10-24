"use client";

import { useState } from "react";
import { createUser } from "@/lib/api";
import { MIN_PASSWORD, newUserProblem, ROLES } from "@/lib/users";

const INPUT =
  "mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const EMPTY = { username: "", email: "", role: "user", password: "", confirm: "" };

/** "Add user": username, email (optional), role and a first password (twice). */
export default function AddUserForm({ onCreated }) {
  const [form, setForm] = useState(EMPTY);
  const [state, setState] = useState({ phase: "form" }); // form | sending | error
  const busy = state.phase === "sending";
  const set = (field) => (e) => setForm({ ...form, [field]: e.target.value });

  async function submit(e) {
    e.preventDefault();
    const problem = newUserProblem(form);
    if (problem) {
      setState({ phase: "error", error: problem });
      return;
    }
    setState({ phase: "sending" });
    try {
      const { username, email, role, password } = form;
      const user = await createUser({ username: username.trim(), email: email.trim() || null, role, password });
      setForm(EMPTY);
      setState({ phase: "form" });
      onCreated(user);
    } catch (err) {
      setState({ phase: "error", error: err.message });
    }
  }

  return (
    <form onSubmit={submit} aria-label="Add user" className="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-5">
      <label className="block">
        <span className="text-muted">Username</span>
        <input autoComplete="off" value={form.username} onChange={set("username")} className={INPUT} />
      </label>
      <label className="block">
        <span className="text-muted">Email (optional)</span>
        <input type="email" autoComplete="off" value={form.email} onChange={set("email")} className={INPUT} />
      </label>
      <label className="block">
        <span className="text-muted">Role</span>
        <select value={form.role} onChange={set("role")} className={`${INPUT} bg-panel`}>
          {ROLES.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
      </label>
      <label className="block">
        <span className="text-muted">Password</span>
        <input type="password" autoComplete="new-password" value={form.password} onChange={set("password")} className={INPUT} />
      </label>
      <label className="block">
        <span className="text-muted">Confirm password</span>
        <input type="password" autoComplete="new-password" value={form.confirm} onChange={set("confirm")} className={INPUT} />
      </label>
      <div className="flex flex-wrap items-center gap-3 sm:col-span-2 lg:col-span-5">
        <button type="submit" disabled={busy} className="rounded bg-action px-3 py-1 font-semibold text-white disabled:opacity-60">
          {busy ? "Adding…" : "Add user"}
        </button>
        <span className="text-xs text-muted">
          Username: 3 to 64 of a-z 0-9 . _ - · password at least {MIN_PASSWORD} characters, given to the person by a safe channel.
        </span>
        {state.phase === "error" ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            {state.error}
          </p>
        ) : null}
      </div>
    </form>
  );
}
