// The analytics allow-list (docs/product/analytics/event-plan.md §2 and §4,
// #192): the only events and properties the collector stores. Pure — the
// route (app/api/events) and lib/track.js share it. A new event or property
// goes into the plan first; __tests__/events-plan.test.js keeps this file
// equal to the plan's tables.

const enumOf = (...values) => ({ type: "enum", values });
const duration = (capMs) => ({ type: "duration", capMs });
const BOOL = { type: "bool" };

const HOUR_MS = 3600 * 1000;

// E3's four investor answers, plus "none of these" (surveys.md §2).
const FOUR_ANSWERS = ["holding", "income", "worst_case", "last_action", "none"];

// A catalogue key: "<prefix>.<words>" of lower-case letters and "_" only — no
// digit, so a number from the page can never be smuggled in as an id.
const catalogue = (prefix) => ({ type: "catalogue id", pattern: new RegExp(`^${prefix}\\.[a-z]+(_[a-z]+)*$`) });

/** §4: property → its rule. */
export const PROPERTIES = {
  entry_screen: enumOf("monitor", "login"),
  screen: enumOf("monitor", "login"),
  viewport: enumOf("phone", "tablet", "desktop"),
  panel: enumOf("account", "held", "others", "trades"),
  open_ms: duration(HOUR_MS),
  filter: enumOf("bot", "strategy", "type", "moneyness", "group_by"),
  level: enumOf("strategy", "slot", "structure", "leg", "orders"),
  tip_id: catalogue("tip"),
  video_id: catalogue("video"),
  watched_ms: duration(10 * 60 * 1000),
  helped: BOOL,
  elapsed_ms: duration(HOUR_MS),
  side: enumOf("call", "put"),
  allowed: BOOL,
  last_leg: BOOL,
  outcome: enumOf("ok", "refused", "error", "timeout"),
  source: enumOf("bot_api", "account", "pnl_history"),
  reason: enumOf("user", "expired", "refresh_failed"),
  // The in-product surveys (docs/product/analytics/surveys.md, #297): codes
  // only — a survey's free text is kept beside the user, never in an event.
  survey_id: enumOf("nps", "answers", "exit"),
  score: enumOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"),
  reason_code: enumOf("value_unclear", "losses", "too_complex", "price", "trust", "switched", "steady_income", "easy_to_follow", "saves_time", "other"),
  came_for: enumOf(...FOUR_ANSWERS),
  hardest_to_find: enumOf(...FOUR_ANSWERS),
};

/** §2: event → the properties it may carry (each optional). */
export const EVENTS = {
  session_started: ["entry_screen", "viewport"],
  screen_viewed: ["screen"],
  panel_expanded: ["panel"],
  panel_collapsed: ["panel", "open_ms"],
  filter_changed: ["filter"],
  drill_opened: ["level"],
  tip_opened: ["tip_id", "screen"],
  video_played: ["video_id"],
  video_completed: ["video_id", "watched_ms"],
  help_resolved: ["tip_id", "video_id", "helped"],
  first_answers_seen: ["elapsed_ms"],
  manual_close_opened: ["side", "allowed"],
  manual_close_cancelled: ["side"],
  manual_close_confirmed: ["side", "last_leg"],
  manual_close_result: ["side", "outcome"],
  rebalance_previewed: ["outcome"],
  rebalance_confirmed: ["outcome"],
  data_stale_shown: ["source"],
  signed_out: ["reason"],
  survey_shown: ["survey_id"],
  survey_answered: ["survey_id", "score", "reason_code", "came_for", "hardest_to_find"],
  survey_dismissed: ["survey_id"],
};

// The browser's random per-session id (lib/track.js): 32 hex characters.
const SESSION_ID = /^[0-9a-f]{32}$/;

/** Why `value` breaks `rule`, or null. */
function valueProblem(rule, value) {
  switch (rule.type) {
    case "enum":
      return rule.values.includes(value) ? null : `one of ${rule.values.join(", ")}`;
    case "bool":
      return typeof value === "boolean" ? null : "true or false";
    case "duration":
      return Number.isInteger(value) && value >= 0 && value <= rule.capMs ? null : `whole milliseconds, 0 to ${rule.capMs}`;
    case "catalogue id":
      return typeof value === "string" && value.length <= 64 && rule.pattern.test(value) ? null : "a help catalogue key, letters and _ only";
    default:
      return "unknown rule";
  }
}

/**
 * Checks a browser's event against the plan: a known event, a session id,
 * only that event's properties, each with an allowed value. Anything else is
 * refused whole — never stored in part.
 * → { ok: true, event, session_id, props } | { ok: false, error }
 */
export function validateEvent(body) {
  if (!body || typeof body !== "object" || Array.isArray(body)) return { ok: false, error: "an event object required" };
  for (const key of Object.keys(body)) {
    if (!["event", "session_id", "props"].includes(key)) return { ok: false, error: `field ${key} is not accepted` };
  }
  const { event, session_id: sessionId, props = {} } = body;
  if (typeof event !== "string" || !Object.hasOwn(EVENTS, event)) return { ok: false, error: "event: not in the event plan" };
  if (typeof sessionId !== "string" || !SESSION_ID.test(sessionId)) return { ok: false, error: "session_id: 32 hex characters" };
  if (!props || typeof props !== "object" || Array.isArray(props)) return { ok: false, error: "props: an object" };

  const allowed = EVENTS[event];
  for (const [name, value] of Object.entries(props)) {
    if (!allowed.includes(name)) return { ok: false, error: `property ${name} is not in the plan for ${event}` };
    const problem = valueProblem(PROPERTIES[name], value);
    if (problem) return { ok: false, error: `property ${name}: ${problem}` };
  }
  return { ok: true, event, session_id: sessionId, props: { ...props } };
}
