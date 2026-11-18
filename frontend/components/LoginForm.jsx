"use client";

import { useEffect, useState } from "react";
import { loginNext, signedOutMessage, signIn, tryRefresh } from "@/lib/session";
import { idleSignOutMessage } from "@/lib/auth/idle";

/** Username + password form. A live refresh cookie (expired access token) skips it. */
export default function LoginForm() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);
  // Why the last session ended, when it was inactivity (#821); nothing for an ordinary expiry or a sign-out.
  const [notice, setNotice] = useState(null);

  useEffect(() => {
    let cancelled = false;
    tryRefresh().then(({ ok, idle }) => {
      if (cancelled) return;
      if (ok) window.location.assign(loginNext(window.location.search));
      // This refresh ended it, or the page was sent here with ?idle= by apiFetch.
      else setNotice(idle ? idleSignOutMessage(idle) : signedOutMessage(window.location.search));
    });
    return () => {
      cancelled = true;
    };
  }, []);

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await signIn(username, password);
      window.location.assign(loginNext(window.location.search));
    } catch (err) {
      setError(err.message);
      setPassword("");
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="w-full max-w-sm space-y-4 rounded-lg border border-line bg-panel p-6" aria-label="Sign in">
      <h1 className="text-lg font-bold">optionsBot · Sign in</h1>
      {notice ? (
        <p role="status" className="text-sm text-muted">
          {notice}
        </p>
      ) : null}
      <label className="block text-sm">
        <span className="text-muted">Username</span>
        <input
          name="username"
          autoComplete="username"
          required
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          className="mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
        />
      </label>
      <label className="block text-sm">
        <span className="text-muted">Password</span>
        <input
          name="password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
        />
      </label>
      {error ? (
        <p role="alert" className="text-sm text-loss">
          {error}
        </p>
      ) : null}
      <button
        type="submit"
        disabled={busy}
        className="w-full rounded border border-line px-3 py-1.5 font-semibold hover:bg-line/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-50"
      >
        {busy ? "Signing in…" : "Sign in"}
      </button>
    </form>
  );
}
