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

/** The `screen` of the menu page at pathname, or null for a page outside the menu (admin, sign-in). */
export function screenOf(pathname) {
  const item = menuItems(pathname).find((i) => i.current);
  return item ? screenOfKey(item.key) : null;
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
