"use client";

import clsx from "clsx";
import { useState } from "react";
import { ANSWERS_CODES, ANSWERS_LABELS, ANSWERS_MARK, ANSWERS_QUESTIONS, ANSWERS_THANKS } from "@/lib/analytics/answers";

// The four answers card at the top of the assistant panel (#498, surveys.md
// §2, E13's prototype): what the person came to check and what was hardest to
// find, each one tap among the same five options. Both questions are
// optional; Send needs one. No free text. Same place and style as NpsCard.

const BUTTON = "min-h-11 rounded-md px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-testnet";

export default function AnswersCard({ assistant }) {
  const { answersCard: stage, answersAnswer, answersDismiss, answersClose } = assistant;
  const [picked, setPicked] = useState({});
  const [sending, setSending] = useState(false);
  const [error, setError] = useState(null);
  if (!stage) return null;

  const done = stage !== "question";
  const answered = Object.keys(picked).length > 0;
  const send = async () => {
    setSending(true);
    setError(null);
    try {
      await answersAnswer(picked);
    } catch (e) {
      setError(e.message);
    } finally {
      setSending(false);
    }
  };
  // A second tap on the chosen option clears it: each question may stay blank.
  const pick = (field, code) =>
    setPicked(({ [field]: current, ...rest }) => (current === code ? rest : { ...rest, [field]: code }));

  return (
    <section aria-label="Optional questions from the team" className="space-y-2 border-b border-line bg-slate-900 px-3 py-3 text-sm">
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted">{ANSWERS_MARK}</span>
        <button
          type="button"
          aria-label="Close the questions"
          onClick={done ? answersClose : answersDismiss}
          className={clsx(BUTTON, "ml-auto min-w-11 text-lg text-muted hover:text-white")}
        >
          ×
        </button>
      </div>
      {stage === "thanks" ? <p role="status">{ANSWERS_THANKS}</p> : null}
      {done ? null : (
        <>
          {ANSWERS_QUESTIONS.map(({ field, question }) => (
            <div key={field} className="space-y-1">
              <p id={`answers-${field}`} className="font-semibold">
                {question} <span className="font-normal text-muted">(optional)</span>
              </p>
              <div role="group" aria-labelledby={`answers-${field}`} className="flex flex-wrap gap-1">
                {ANSWERS_CODES.map((c) => (
                  <button
                    key={c}
                    type="button"
                    aria-pressed={picked[field] === c}
                    onClick={() => pick(field, c)}
                    className={clsx(BUTTON, "rounded-full border border-line text-xs", picked[field] === c ? "bg-sky-900 text-white" : "hover:border-muted")}
                  >
                    {ANSWERS_LABELS[c]}
                  </button>
                ))}
              </div>
            </div>
          ))}
          {error ? (
            <p role="alert" className="text-xs text-warn">
              Not sent: {error}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <button type="button" disabled={!answered || sending} onClick={send} className={clsx(BUTTON, "bg-slate-700 font-semibold text-white disabled:opacity-50")}>
              Send
            </button>
            <button type="button" disabled={sending} onClick={answersDismiss} className={clsx(BUTTON, "border border-line")}>
              Not now
            </button>
          </div>
        </>
      )}
    </section>
  );
}
