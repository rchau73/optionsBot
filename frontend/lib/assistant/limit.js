// The assistant's rate limit per user (#191): each question is a paid model
// call, so a person may ask QUESTIONS_PER_HOUR an hour. In this process's
// memory, like the per-IP sign-in count (a restart forgets it). Takes `now`
// (ms) so tests can move the clock.

export const QUESTIONS_PER_HOUR = 30;
export const WINDOW_MS = 60 * 60 * 1000;

export class QuestionLimiter {
  constructor(max = QUESTIONS_PER_HOUR, windowMs = WINDOW_MS) {
    this.max = max;
    this.windowMs = windowMs;
    this.asked = new Map(); // user id → question times
  }

  /** Counts a question; 0 when allowed, else the seconds to wait (not counted). */
  take(userId, now) {
    const times = (this.asked.get(userId) ?? []).filter((t) => now - t < this.windowMs);
    if (times.length >= this.max) {
      this.asked.set(userId, times);
      return Math.max(1, Math.ceil((times[0] + this.windowMs - now) / 1000));
    }
    this.asked.set(userId, [...times, now]);
    return 0;
  }

  /** Questions the person may still ask in the hour ending at now (the panel's "n of 30 left"). */
  remaining(userId, now) {
    const times = (this.asked.get(userId) ?? []).filter((t) => now - t < this.windowMs);
    return Math.max(0, this.max - times.length);
  }
}
