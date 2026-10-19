// Sign-in throttling, in memory (a restart forgets it): a username with 5
// failures within 15 minutes is locked until 15 minutes after its last
// failure, and an IP may try 20 sign-ins a minute. Every method takes `now`
// (ms) so tests can move the clock.

export const LOCK_FAILURES = 5;
export const LOCK_WINDOW_MS = 15 * 60 * 1000;
export const IP_ATTEMPTS = 20;
export const IP_WINDOW_MS = 60 * 1000;
// Past this many keys, stale ones are swept before adding another.
const SWEEP_AT = 10000;

function recent(map, key, windowMs, now) {
  const kept = (map.get(key) ?? []).filter((t) => now - t < windowMs);
  if (kept.length === 0) map.delete(key);
  else map.set(key, kept);
  return kept;
}

function sweep(map, windowMs, now) {
  if (map.size < SWEEP_AT) return;
  for (const key of [...map.keys()]) recent(map, key, windowMs, now);
}

export class LoginLimiter {
  constructor() {
    this.failures = new Map(); // username → failure times
    this.locks = new Map(); // username → locked until (ms)
    this.attempts = new Map(); // ip → attempt times
  }

  /** Counts a sign-in from this IP; returns 0 when allowed, else the seconds to wait (not counted). */
  takeIp(ip, now) {
    const times = recent(this.attempts, ip, IP_WINDOW_MS, now);
    if (times.length >= IP_ATTEMPTS) return Math.max(1, Math.ceil((times[0] + IP_WINDOW_MS - now) / 1000));
    sweep(this.attempts, IP_WINDOW_MS, now);
    this.attempts.set(ip, [...times, now]);
    return 0;
  }

  /** The time (ms) the username stays locked until, or 0 when it is not locked. */
  lockedUntil(username, now) {
    const until = this.locks.get(username) ?? 0;
    if (until > now) return until;
    this.locks.delete(username);
    return 0;
  }

  /**
   * Records a failed sign-in; true when this failure locks the username (for
   * 15 minutes from now). Attempts during a lock are not failures: they
   * neither extend it nor count afterwards.
   */
  recordFailure(username, now) {
    const times = recent(this.failures, username, LOCK_WINDOW_MS, now);
    sweep(this.failures, LOCK_WINDOW_MS, now);
    if (times.length + 1 < LOCK_FAILURES) {
      this.failures.set(username, [...times, now]);
      return false;
    }
    this.failures.delete(username);
    for (const [name, until] of this.locks) if (until <= now) this.locks.delete(name);
    this.locks.set(username, now + LOCK_WINDOW_MS);
    return true;
  }

  /** Forgets the failures of a username (after a successful sign-in). */
  clear(username) {
    this.failures.delete(username);
  }
}
