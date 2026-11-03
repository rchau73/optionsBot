"use client";

import clsx from "clsx";
import { useEffect, useState } from "react";
import { endSession, listSessions } from "@/lib/api/auth";
import { formatDateTime } from "@/lib/format";

// "Active sessions" (#584), from the person's menu: where they are signed in,
// this browser marked, and a Sign out button on every other one. Built like
// Change password: one dialog, Escape closes, 44 px targets, rows that wrap.

const BUTTON = "min-h-11 rounded px-3 font-semibold focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-60";

/** What the dialog says about a sign-out elsewhere: it is not instant for reading. */
export const TAKES_EFFECT =
  "Signing out another browser stops it from changing anything at once; it can still read pages for up to 15 minutes, until its sign-in renews and fails.";

const DEPS = { listSessions, endSession };

function SessionRow({ session, ending, onEnd }) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-2 py-2">
      <div className="min-w-0">
        <p className="font-semibold">
          {session.browser}
          {session.current ? <span className="ml-2 rounded bg-testnet/20 px-1.5 text-xs font-normal">This browser</span> : null}
        </p>
        <p className="text-xs text-muted">
          Signed in {formatDateTime(session.created_at)} · last used {formatDateTime(session.last_used_at)}
        </p>
      </div>
      {session.current ? null : (
        <button
          type="button"
          onClick={() => onEnd(session.id)}
          disabled={ending}
          aria-label={`Sign out ${session.browser}, signed in ${formatDateTime(session.created_at)}`}
          className={clsx(BUTTON, "bg-slate-700")}
        >
          {ending ? "Signing out…" : "Sign out"}
        </button>
      )}
    </li>
  );
}

export default function ActiveSessionsModal({ onClose, deps = DEPS }) {
  const [state, setState] = useState({ phase: "loading" }); // loading | list | error
  const [ending, setEnding] = useState(null); // the id being signed out
  const [notice, setNotice] = useState(null); // { ok, text }

  const [version, setVersion] = useState(0); // bumped to read the list again after a sign-out

  useEffect(() => {
    let live = true;
    deps.listSessions().then(
      (sessions) => live && setState({ phase: "list", sessions }),
      (err) => live && setState({ phase: "error", error: err.message }),
    );
    return () => {
      live = false;
    };
  }, [deps, version]);

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && !ending && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, ending]);

  async function end(id) {
    setEnding(id);
    setNotice(null);
    try {
      await deps.endSession(id);
      setNotice({ ok: true, text: "Signed out. That browser goes to the sign-in page within 15 minutes." });
      setVersion((v) => v + 1);
    } catch (err) {
      setNotice({ ok: false, text: err.message });
    } finally {
      setEnding(null);
    }
  }

  const sessions = state.sessions ?? [];
  const others = sessions.filter((s) => !s.current).length;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="sessions-title">
      <div className="max-h-full w-full max-w-md space-y-3 overflow-y-auto rounded-lg border border-line bg-panel p-4 text-sm shadow-xl">
        <h2 id="sessions-title" className="text-base font-bold">
          Active sessions
        </h2>
        <p className="text-xs text-muted">Every browser where you are signed in. No address is kept or shown.</p>
        {state.phase === "loading" ? <p role="status">Loading your sessions…</p> : null}
        {state.phase === "error" ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            Could not load your sessions: {state.error}
          </p>
        ) : null}
        {state.phase === "list" ? (
          <>
            <ul className="divide-y divide-line" aria-label="Your sessions">
              {sessions.map((s) => (
                <SessionRow key={s.id} session={s} ending={ending === s.id} onEnd={end} />
              ))}
            </ul>
            {others === 0 ? <p className="text-muted">You are signed in only here.</p> : null}
            <p className="text-xs text-muted">{TAKES_EFFECT} To sign out this browser, use Sign out in your menu.</p>
          </>
        ) : null}
        {notice ? (
          <p role={notice.ok ? "status" : "alert"} className={clsx("rounded px-2 py-1", notice.ok ? "bg-profit/10 text-profit" : "bg-loss/15 text-loss")}>
            {notice.text}
          </p>
        ) : null}
        <div className="flex justify-end">
          <button type="button" onClick={onClose} disabled={Boolean(ending)} className={clsx(BUTTON, "bg-slate-700")}>
            Close window
          </button>
        </div>
      </div>
    </div>
  );
}
