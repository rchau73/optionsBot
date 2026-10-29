// The analytics allow-list (docs/product/analytics/event-plan.md §2 and §4,
// #192): the only events and properties the collector stores. Pure — the
// route (app/api/events) and lib/track.js share it. A new event or property
// goes into the plan first; __tests__/events-plan.test.js keeps this file
// equal to the plan's tables.

import glossary from "@/content/glossary.json";
import videos from "@/content/videos.json";

const enumOf =(...values) => ({ type: "enum", values });
const duration = (capMs) => ({ type: "duration", capMs });
const BOOL = { type: "bool" };

const HOUR_MS = 3600 * 1000;

// E3's four investor answers, plus "none of these" (surveys.md §2).
const FOUR_ANSWERS = ["holding", "income", "worst_case", "last_action", "none"];

// A catalogue key: "<prefix>.<words>" of lower-case letters and "_" only — no
// digit, so a number from the page can never be smuggled in as an id.
// `known` is the set of ids that exist in the content catalogue (#300): a
// well-formed id that is not in it is refused too.
const catalogue = (prefix, known) => ({ type: "catalogue id", pattern: new RegExp(`^${prefix}\\.[a-z]+(_[a-z]+)*$`), known });

/** A glossary key as a tip id: lower case, every run of other characters one "_" ("stop-loss" → "tip.stop_loss"). */
export const tipIdOf = (term) => `tip.${String(term).toLowerCase().replace(/[^a-z]+/g, "_").replace(/^_+|_+$/g, "")}`;

export const VIDEO_IDS = new Set(Object.keys(videos));
export const TIP_IDS = new Set(Object.keys(glossary).map(tipIdOf));

// Both lists are kept in alphabetical order (#465), so two PRs that each add
// one land on different lines: __tests__/events-plan.test.js fails otherwise.

/** §4: property → its rule. */
export const PROPERTIES = {
  allowed: BOOL,
  // The kind of the assistant's reply, never its text (#424).
  answer: enumOf("answered", "dont_know", "refused", "fallback"),
  came_for: enumOf(...FOUR_ANSWERS),
  elapsed_ms: duration(HOUR_MS),
  // The assistant panel's opener (#408).
  entry: enumOf("menu", "why"),
  entry_screen: enumOf("monitor", "login"),
  filter: enumOf("bot", "strategy", "type", "moneyness", "group_by"),
  hardest_to_find: enumOf(...FOUR_ANSWERS),
  helped: BOOL,
  // The kind of the bell's item followed (#447, lib/notifications.js KINDS), never its text.
  kind: enumOf("survey", "request_needs_info", "bot_event", "request_status", "whats_new"),
  last_leg: BOOL,
  // The overview's switches (#404): layer and unit.
  layer: enumOf("sma", "sma21", "swings", "flip", "strikes"),
  level: enumOf("strategy", "slot", "structure", "leg", "orders"),
  open_ms: duration(HOUR_MS),
  outcome: enumOf("ok", "refused", "error", "timeout"),
  panel: enumOf("account", "positions", "held", "others", "trades", "activity", "pnl", "pnl_coin", "orders"),
  reason: enumOf("user", "expired", "refresh_failed"),
  reason_code: enumOf("value_unclear", "losses", "too_complex", "price", "trust", "switched", "steady_income", "easy_to_follow", "saves_time", "clear_risk", "explains_trades", "trust_numbers", "helpful_answers", "hard_to_find", "missing_feature", "other"),
  score: enumOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"),
  // One per page of the menu (lib/menu.js, lib/analytics/screens.js, #424), the sign-in page and the admin Analytics page (#445).
  screen: enumOf("overview", "income", "worst_case", "positions", "how_it_works", "activity", "account", "details", "login", "analytics"),
  section: enumOf("positions", "held", "trades", "orders", "others"),
  side: enumOf("call", "put"),
  source: enumOf("bot_api", "account", "pnl_history"),
  // The in-product surveys (docs/product/analytics/surveys.md, #297; also
  // score, reason_code, came_for, hardest_to_find): codes only — a survey's
  // free text is kept beside the user, never in an event.
  survey_id: enumOf("nps", "answers", "exit"),
  tip_id: catalogue("tip", TIP_IDS),
  // The admin Analytics page's topic (#473).
  topic: enumOf("usage", "delivery"),
  unit: enumOf("coin", "usd"),
  video_id: catalogue("video", VIDEO_IDS),
  viewport: enumOf("phone", "tablet", "desktop"),
  watched_ms: duration(10 * 60 * 1000),
};

/** §2: event → the properties it may carry (each optional). */
export const EVENTS = {
  assistant_answered: ["answer"],
  assistant_ask: ["entry"],
  assistant_flag: [],
  assistant_open: ["entry"],
  data_stale_shown: ["source"],
  details_section_jump: ["section"],
  details_trade_expand: [],
  drill_opened: ["level"],
  filter_changed: ["filter"],
  first_answers_seen: ["elapsed_ms"],
  help_resolved: ["tip_id", "video_id", "helped"],
  manual_close_cancelled: ["side"],
  manual_close_confirmed: ["side", "last_leg"],
  manual_close_opened: ["side", "allowed"],
  manual_close_result: ["side", "outcome"],
  notification_followed: ["kind"],
  notifications_opened: [],
  overview_layer_toggle: ["layer"],
  overview_unit_toggle: ["unit"],
  panel_collapsed: ["panel", "open_ms"],
  panel_expanded: ["panel"],
  rebalance_confirmed: ["outcome"],
  rebalance_previewed: ["outcome"],
  screen_viewed: ["screen", "topic"],
  session_started: ["entry_screen", "viewport"],
  signed_out: ["reason"],
  survey_answered: ["survey_id", "score", "reason_code", "came_for", "hardest_to_find"],
  survey_dismissed: ["survey_id"],
  survey_shown: ["survey_id"],
  tip_opened: ["tip_id", "screen"],
  video_completed: ["video_id", "watched_ms"],
  video_played: ["video_id"],
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
      if (typeof value !== "string" || value.length > 64 || !rule.pattern.test(value)) return "a help catalogue key, letters and _ only";
      return rule.known.has(value) ? null : "an id that is in the content catalogue";
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
