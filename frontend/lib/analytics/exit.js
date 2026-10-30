// The exit question on the "Delete my account" screen (#543,
// docs/product/analytics/surveys.md §3): one optional coded reason, a text box
// only for "other", skippable in one click, never blocking the deletion. Pure
// words and the order of the leaving steps; the dialog (DeleteAccount.jsx)
// only renders them. Each text is in EN and PT-BR as surveys.md has it;
// __tests__/delete-account.test.jsx keeps the two equal.
import { SURVEYS } from "./surveys.js";

/** The exit codes only, never the promoter codes (surveys.md §3 and §4). */
export const EXIT_CODES = SURVEYS.exit.codes.reason_code;

/** surveys.md §3: the question, the text box for "other" and each code's words (§4). */
export const EXIT_COPY = {
  en: {
    question: "Before you go: what is the main reason? (optional)",
    other: "Tell us in a few words (optional)",
    labels: {
      value_unclear: "I don't see the value",
      losses: "risk or losses",
      too_complex: "too hard to understand",
      price: "price",
      trust: "I don't trust what I see",
      switched: "switched to another product",
      other: "other",
    },
  },
  pt: {
    question: "Antes de sair: qual o principal motivo? (opcional)",
    other: "Conte em poucas palavras (opcional)",
    labels: {
      value_unclear: "não vejo o valor",
      losses: "risco ou perdas",
      too_complex: "difícil de entender",
      price: "preço",
      trust: "não confio no que vejo",
      switched: "mudei para outro produto",
      other: "outro",
    },
  },
};

/** What a deletion removes and what it never touches (docs/legal/data-protection.md). */
export const DELETE_COPY = {
  title: "Delete my account",
  removes: "This deletes your sign-in, your analytics events and your survey answers, with any text you wrote. It cannot be undone.",
  untouched: "Your exchange accounts and funds are not touched: the bots keep trading, and deposits and withdrawals stay on the exchange.",
  continue: "Continue",
  confirm: "Delete my account",
  skip: "Skip and delete",
  back: "Back",
  close: "Close and delete my account",
};

/** How long the screen waits for the answer to be stored before deleting anyway. */
export const EXIT_ANSWER_WAIT_MS = 2000;

/**
 * The POST /api/survey body for a chosen reason, or null when none was chosen
 * (Skip, close, or Delete without a choice). Text only with "other".
 */
export function exitAnswerBody(reason, text, sessionId) {
  if (!EXIT_CODES.includes(reason)) return null;
  const body = { survey_id: "exit", session_id: sessionId, codes: { reason_code: reason } };
  if (reason === "other" && typeof text === "string" && text.trim() !== "") body.text = text;
  return body;
}

/**
 * The leaving steps, in order: the reason if one was chosen (or
 * survey_dismissed when none), then the deletion. Storing the reason may fail
 * or answer 403 (no analytics consent, surveys.md §6 C3): that is never shown
 * and never stops the deletion, and it waits at most EXIT_ANSWER_WAIT_MS.
 * The answer goes first so it is removed with the account's other events.
 * deps: { sendAnswer(body), deleteAccount(), track(event, props), sessionId(), wait(ms) }.
 * → what deleteAccount returns; throws only with deleteAccount's reason.
 */
export async function leave({ reason = null, text = "" } = {}, deps) {
  const body = exitAnswerBody(reason, text, deps.sessionId());
  if (body) {
    await Promise.race([deps.sendAnswer(body).catch(() => {}), deps.wait(EXIT_ANSWER_WAIT_MS)]);
  } else {
    deps.track("survey_dismissed", { survey_id: "exit" });
  }
  return deps.deleteAccount();
}
