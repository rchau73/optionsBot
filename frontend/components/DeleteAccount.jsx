"use client";

import clsx from "clsx";
import { useEffect, useState } from "react";
import { deleteAccount } from "@/lib/api/auth";
import { navigation } from "@/lib/api/http";
import { sendSurveyAnswer } from "@/lib/api/survey";
import { DELETE_COPY, EXIT_CODES, EXIT_COPY, leave } from "@/lib/analytics/exit";
import { MAX_SURVEY_TEXT } from "@/lib/analytics/surveys";
import { currentSessionId, track } from "@/lib/track";

// "Delete my account" (#543, surveys.md §3), from the user menu: a first
// screen says what is deleted and what is not; the final screen has the
// delete button with the optional exit question below it. Skip, close (×)
// and Delete with no choice all delete at once; the question never blocks.

const BUTTON = "min-h-11 rounded px-3 font-semibold focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet disabled:opacity-60";
const COPY = EXIT_COPY.en;

const sentence = (s) => s.charAt(0).toUpperCase() + s.slice(1);

/** A random id for a person without analytics: the route answers 403 before reading it. */
function sessionId() {
  const id = currentSessionId();
  if (id) return id;
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

const DEPS = {
  sendAnswer: sendSurveyAnswer,
  deleteAccount,
  track,
  sessionId,
  wait: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
};

export default function DeleteAccountModal({ onClose, deps = DEPS }) {
  const [step, setStep] = useState("confirm"); // confirm | final
  const [state, setState] = useState({ phase: "idle" }); // idle | deleting | error
  const [reason, setReason] = useState(null);
  const [text, setText] = useState("");
  const busy = state.phase === "deleting";

  useEffect(() => {
    if (step === "final") deps.track("survey_shown", { survey_id: "exit" });
  }, [step, deps]);

  useEffect(() => {
    // Escape never deletes: it closes the first screen and steps back from the final one.
    const onKey = (e) => {
      if (e.key !== "Escape" || busy) return;
      if (step === "final") setStep("confirm");
      else onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, busy, step]);

  async function remove(answer) {
    setState({ phase: "deleting" });
    try {
      await leave(answer, deps);
      navigation.assign("/login");
    } catch (err) {
      setState({ phase: "error", error: err.message });
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" aria-labelledby="delete-account-title">
      <div className="max-h-full w-full max-w-md space-y-3 overflow-y-auto rounded-lg border border-line bg-panel p-4 text-sm shadow-xl">
        <div className="flex items-start gap-2">
          <h2 id="delete-account-title" className="text-base font-bold">
            {DELETE_COPY.title}
          </h2>
          {step === "final" ? (
            <button
              type="button"
              aria-label={DELETE_COPY.close}
              title={DELETE_COPY.close}
              disabled={busy}
              onClick={() => remove({})}
              className={clsx(BUTTON, "ml-auto min-w-11 text-lg text-muted hover:text-white")}
            >
              ×
            </button>
          ) : null}
        </div>
        <p>{DELETE_COPY.removes}</p>
        <p className="text-muted">{DELETE_COPY.untouched}</p>
        {state.phase === "error" ? (
          <p role="alert" className="rounded bg-loss/15 px-2 py-1 text-loss">
            Not deleted: {state.error}
          </p>
        ) : null}
        {busy ? <p role="status">Deleting your account…</p> : null}

        {step === "confirm" ? (
          <div className="flex justify-end gap-2">
            <button type="button" onClick={onClose} className={clsx(BUTTON, "bg-slate-700 font-normal")}>
              Cancel
            </button>
            <button type="button" onClick={() => setStep("final")} className={clsx(BUTTON, "bg-loss text-white")}>
              {DELETE_COPY.continue}
            </button>
          </div>
        ) : (
          <>
            <div className="flex justify-end gap-2">
              <button type="button" disabled={busy} onClick={() => setStep("confirm")} className={clsx(BUTTON, "bg-slate-700 font-normal")}>
                {DELETE_COPY.back}
              </button>
              <button type="button" disabled={busy} onClick={() => remove({ reason, text })} className={clsx(BUTTON, "bg-loss text-white")}>
                {busy ? "Deleting…" : DELETE_COPY.confirm}
              </button>
            </div>
            <fieldset className="space-y-2 border-t border-line pt-3" disabled={busy}>
              <legend className="pt-3 font-semibold">{COPY.question}</legend>
              <div className="grid gap-1 sm:grid-cols-2">
                {EXIT_CODES.map((code) => (
                  <label key={code} className="flex min-h-11 cursor-pointer items-center gap-2 rounded border border-line px-2 hover:border-muted">
                    <input type="radio" name="exit-reason" value={code} checked={reason === code} onChange={() => setReason(code)} />
                    {sentence(COPY.labels[code])}
                  </label>
                ))}
              </div>
              {reason === "other" ? (
                <label className="block">
                  <span className="text-muted">{COPY.other}</span>
                  <textarea
                    value={text}
                    maxLength={MAX_SURVEY_TEXT}
                    onChange={(e) => setText(e.target.value)}
                    rows={3}
                    className="mt-1 w-full rounded border border-line bg-transparent px-2 py-1.5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
                  />
                </label>
              ) : null}
              <button type="button" onClick={() => remove({})} className={clsx(BUTTON, "border border-line font-normal")}>
                {DELETE_COPY.skip}
              </button>
            </fieldset>
          </>
        )}
      </div>
    </div>
  );
}
