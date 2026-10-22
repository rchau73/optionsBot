// The monitor's one analytics sender (#193, docs/product/analytics/event-plan.md):
// track(event, props) posts to the monitor's own /api/events — never a third
// party. Nothing is sent until the person said yes (setAnalyticsConsent(true));
// "no" or not asked yet means nothing at all is sent. Events outside the plan
// are dropped here too, so a mistake in a component never reaches the server.
import { validateEvent } from "@/lib/analytics/plan";

export const SESSION_IDLE_MS = 30 * 60 * 1000;

const state = { consent: null, sessionId: null, lastAt: 0 };

/** The person's answer: true (yes), false (no) or null (not asked yet). Only true sends. */
export function setAnalyticsConsent(consent) {
  state.consent = consent === true ? true : consent === false ? false : null;
  if (state.consent !== true) state.sessionId = null;
}

function newSessionId() {
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** The browser session's random id, new after 30 min without an event. */
function sessionId(nowMs) {
  if (!state.sessionId || nowMs - state.lastAt > SESSION_IDLE_MS) state.sessionId = newSessionId();
  state.lastAt = nowMs;
  return state.sessionId;
}

/**
 * Sends one event; true when it was handed to the browser. Never throws and
 * never waits: analytics must not break or slow the page.
 */
export function track(event, props = {}, nowMs = Date.now()) {
  if (state.consent !== true) return false;
  const body = { event, session_id: sessionId(nowMs), props };
  if (!validateEvent(body).ok) return false;
  try {
    fetch("/api/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      credentials: "same-origin",
      keepalive: true,
    }).catch(() => {});
    return true;
  } catch {
    return false;
  }
}

/** Forgets consent and session (tests, sign-out). */
export function resetTracking() {
  state.consent = null;
  state.sessionId = null;
  state.lastAt = 0;
}
