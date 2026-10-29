"use client";

import clsx from "clsx";
import { useState } from "react";
import { MAX_SURVEY_TEXT } from "@/lib/analytics/surveys";
import { NPS_MARK, NPS_QUESTION, NPS_THANKS, REASON_LABELS, npsStep2 } from "@/lib/analytics/nps";

// The monthly NPS card at the top of the assistant panel (#446, surveys.md §1,
// prototype approved on #164): a 0–10 score, then one list by score (what we
// do best for 9–10, what to improve first for 0–8) and an optional private
// comment. Every step is optional; the assistant works the same around it.

const BUTTON = "min-h-11 rounded-md px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";
const SCORES = Array.from({ length: 11 }, (_, i) => i);

export default function NpsCard({ assistant }) {
  const { npsCard: stage, npsAnswer, npsDismiss, npsOptOut, npsClose } = assistant;
  const [score, setScore] = useState(null);
  const [reason, setReason] = useState(null);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState(null);
  if (!stage) return null;

  const done = stage !== "question";
  const run = async (fn) => {
    setSending(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      setError(e.message);
    } finally {
      setSending(false);
    }
  };
  const step2 = score == null ? null : npsStep2(score);

  return (
    <section aria-label="Optional question from the team" className="space-y-2 border-b border-line bg-slate-900 px-3 py-3 text-sm">
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted">{NPS_MARK}</span>
        <button
          type="button"
          aria-label="Close the question"
          onClick={done ? npsClose : npsDismiss}
          className={clsx(BUTTON, "ml-auto min-w-11 text-lg text-muted hover:text-white")}
        >
          ×
        </button>
      </div>
      {stage === "thanks" ? <p role="status">{NPS_THANKS}</p> : null}
      {stage === "opted_out" ? (
        <p role="status">We won&apos;t ask again. You can turn it back on in Account › Privacy.</p>
      ) : null}
      {done ? null : (
        <>
          <p id="nps-question" className="font-semibold">
            {NPS_QUESTION}
          </p>
          <div role="group" aria-labelledby="nps-question" className="flex flex-wrap gap-1">
            {SCORES.map((n) => (
              <button
                key={n}
                type="button"
                aria-pressed={score === n}
                onClick={() => {
                  // A new band shows its own list: a chip of the other list is cleared.
                  if (score == null || npsStep2(score).question !== npsStep2(n).question) setReason(null);
                  setScore(n);
                }}
                className={clsx(BUTTON, "min-w-11 border border-line px-0", score === n ? "bg-sky-900 text-white" : "hover:border-muted")}
              >
                {n}
              </button>
            ))}
          </div>
          <div className="flex justify-between text-xs text-muted" aria-hidden="true">
            <span>Not likely</span>
            <span>Very likely</span>
          </div>
          {step2 ? (
            <>
              <p id="nps-why" className="font-semibold">
                {step2.question} <span className="font-normal text-muted">(optional)</span>
              </p>
              <div role="group" aria-labelledby="nps-why" className="flex flex-wrap gap-1">
                {step2.codes.map((c) => (
                  <button
                    key={c}
                    type="button"
                    aria-pressed={reason === c}
                    onClick={() => setReason(reason === c ? null : c)}
                    className={clsx(BUTTON, "rounded-full border border-line text-xs", reason === c ? "bg-sky-900 text-white" : "hover:border-muted")}
                  >
                    {REASON_LABELS[c]}
                  </button>
                ))}
              </div>
              <label htmlFor="nps-text" className="block text-xs text-muted">
                Anything you want to add? (optional, private: kept with your account, never in analytics)
              </label>
              <textarea
                id="nps-text"
                value={text}
                maxLength={MAX_SURVEY_TEXT}
                rows={2}
                onChange={(e) => setText(e.target.value)}
                className="w-full resize-none rounded-md border border-line bg-slate-950 px-2 py-1 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet"
              />
            </>
          ) : null}
          {error ? (
            <p role="alert" className="text-xs text-warn">
              Not sent: {error}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              disabled={score == null || sending}
              onClick={() => run(() => npsAnswer({ score, reason, text }))}
              className={clsx(BUTTON, "bg-slate-700 font-semibold text-white disabled:opacity-50")}
            >
              Send
            </button>
            {score != null ? (
              <button type="button" disabled={sending} onClick={() => run(() => npsAnswer({ score }))} className={clsx(BUTTON, "border border-line")}>
                Send the score only
              </button>
            ) : null}
            <button type="button" disabled={sending} onClick={npsDismiss} className={clsx(BUTTON, "border border-line")}>
              Not now
            </button>
            <button type="button" disabled={sending} onClick={() => run(npsOptOut)} className={clsx(BUTTON, "text-muted underline hover:text-white")}>
              Don&apos;t ask me again
            </button>
          </div>
        </>
      )}
    </section>
  );
}
