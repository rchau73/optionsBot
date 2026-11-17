// Sign-in awareness (#803): the person's previous sign-in and the wrong
// passwords typed for their username since, in the Active sessions dialog and
// as one Needs you item of the bell. Pure: the server (AuthService) builds the
// summary, the dialog and the bell word it with these functions.

import { formatDateTime } from "./format";

/** The advice under the line: what to do when a sign-in or an attempt was not the person. */
export const NOT_YOU = "Not you? Change your password and sign out the other browsers.";

/** "0 wrong-password attempts since." / "1 wrong-password attempt since." */
export function attemptsSentence(n) {
  return `${n} wrong-password ${n === 1 ? "attempt" : "attempts"} since.`;
}

/**
 * The dialog's first line for a summary ({ first_sign_in, previous_sign_in,
 * failed_since, locked_until? }): the previous sign-in and the count, or
 * "This is your first sign-in." with no count.
 */
export function summaryLine(s) {
  if (s.first_sign_in) return "This is your first sign-in.";
  const when = s.previous_sign_in ? `Last sign-in: ${formatDateTime(s.previous_sign_in)}.` : "Your sign-in before this one was not recorded.";
  return `${when} ${attemptsSentence(s.failed_since)}`;
}

/** The lock's sentence when the username is locked now, else null. */
export function lockedLine(s) {
  return s.locked_until ? `Your username is locked until ${formatDateTime(s.locked_until)} after too many wrong passwords.` : null;
}

/** The analytics bucket of a count (event plan `failed_since`): never the number itself above 1. */
export function failedBucket(n) {
  if (!(n > 0)) return "0";
  if (n === 1) return "1";
  return n <= 4 ? "2_4" : "5_plus";
}

/**
 * The bell item's id for a summary, or null when there is nothing to tell:
 * a first sign-in or no wrong password. Keyed by the last sign-in and the
 * count, so a new attempt brings the item back after it was read.
 */
export function noticeId(lastSignInSec, failedSince, firstSignIn) {
  if (firstSignIn || !(failedSince > 0)) return null;
  return `security:${lastSignInSec ?? "never"}:${failedSince}`;
}

/**
 * The bell's Needs you item for a summary with a notice_id, or null. Listed
 * until it is read: opening Active sessions marks it read.
 */
export function securityItem(s, nowMs) {
  if (!s?.notice_id) return null;
  const n = s.failed_since;
  return {
    id: s.notice_id,
    kind: "security",
    ts: new Date(nowMs).toISOString(),
    title: `${n} wrong-password ${n === 1 ? "attempt" : "attempts"} on your account`,
    text: `They were typed since your previous sign-in. Open Active sessions in your account menu to see when you last signed in. ${NOT_YOU}`,
  };
}
