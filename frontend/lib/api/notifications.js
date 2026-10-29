// The signed-in person's bell (/api/notifications, #447).

import { getJSON, sendJSON } from "./http.js";

const notificationsUrl = (bot) => `/api/notifications${bot ? `?bot=${encodeURIComponent(bot)}` : ""}`;

/** The bell for the page's bot: { items, needs_you, settings, errors }; throws with the server's reason. */
export async function fetchNotifications(bot, signal) {
  return getJSON(notificationsUrl(bot), signal);
}

/**
 * A change of the person's bell: { action: "read", ids } |
 * { action: "read_all" } | { action: "setting", kind, enabled } → the new
 * view; throws with the server's reason.
 */
export async function sendNotificationAction(bot, body) {
  return sendJSON(notificationsUrl(bot), "POST", body);
}
