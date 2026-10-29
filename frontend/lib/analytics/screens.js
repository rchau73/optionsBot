// What the monitor's pages and the assistant panel send (#424, event plan §2
// and §4). Pure: components/ScreenTracker.jsx and components/AssistantProvider.jsx
// call them; __tests__/analytics-screens.test.js keeps every page of the menu
// in the plan's `screen` list.

import { PROPERTIES } from "@/lib/analytics/plan";
import { menuItems } from "@/lib/menu";

/** A menu item's key as a `screen` value: "worst-case" → "worst_case". */
export const screenOfKey = (key) => String(key).replace(/-/g, "_");

/** The labels of the menu's pages whose `screen` is not in the plan's list (empty when every page is counted). */
export function pagesWithoutScreen(items = menuItems("/"), screens = PROPERTIES.screen.values) {
  return items.filter((i) => !screens.includes(screenOfKey(i.key))).map((i) => i.label);
}

/** Pages outside the menu that are counted too: the admin Analytics page (#445: does the admin open it on Mondays?). */
const OTHER_SCREENS = { "/admin/analytics": "analytics" };

/** The `screen` of the page at pathname: a menu page or one of OTHER_SCREENS; null for any other page (Users, Audit, sign-in). */
export function screenOf(pathname) {
  if (Object.hasOwn(OTHER_SCREENS, pathname)) return OTHER_SCREENS[pathname];
  const item = menuItems(pathname).find((i) => i.current);
  return item ? screenOfKey(item.key) : null;
}

/**
 * The screen_viewed properties of a page, or null: { screen }, and on the
 * admin Analytics page also its `topic` from ?topic= (#473: usage unless
 * delivery). `search` is the page's URLSearchParams (or null).
 */
export function screenViewOf(pathname, search = null) {
  const screen = screenOf(pathname);
  if (!screen) return null;
  if (screen !== "analytics") return { screen };
  return { screen, topic: search?.get("topic") === "delivery" ? "delivery" : "usage" };
}

// The assistant route's outcomes (lib/assistant/service.js) → the plan's
// `answer` kinds: what the person was shown, never why the guard chose it.
const ANSWER_OF_OUTCOME = {
  answered: "answered",
  dont_know: "dont_know",
  no_citation: "dont_know", // replaced by "I don't know"
  refused: "refused",
  rejected: "fallback", // replaced by the safe fallback reply
};

/** The `answer` kind of a reply's outcome, or null for an outcome that is no reply shown. */
export function answerOf(outcome) {
  return Object.hasOwn(ANSWER_OF_OUTCOME, outcome) ? ANSWER_OF_OUTCOME[outcome] : null;
}
