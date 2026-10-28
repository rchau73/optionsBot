// The menu bar on every signed-in page (#385, E3 prototype screens 4 and 7):
// one link per view, the current one marked, the chosen bot kept from page to
// page through ?bot=; the signed-in person's menu ends it (#394).

/** Pages without the menu: signed out (sign-in, set a new password with a reset link). */
const NO_MENU = ["/login", "/reset"];

/** Whether pathname shows the menu. */
export function showMenu(pathname) {
  return !NO_MENU.some((p) => pathname === p || pathname?.startsWith(`${p}/`));
}

/** The old ?view= value of the overview's detail panels; /?view=details now redirects to /details (#405). */
export const DETAILS_VIEW = "details";

/** A bot name as the pages accept it (letters, digits, _ and -), lower-case; null otherwise. */
export function botParam(value) {
  return typeof value === "string" && /^[a-z0-9_-]+$/i.test(value) ? value.toLowerCase() : null;
}

/** Where `/?view=details` goes now: "/details" (keeping a valid ?bot=), or null for any other overview URL. */
export function detailsRedirect({ bot, view } = {}) {
  if (view !== DETAILS_VIEW) return null;
  const b = botParam(bot);
  return b ? `/details?bot=${encodeURIComponent(b)}` : "/details";
}

/**
 * The menu's items for the page at pathname, each { key, label, href,
 * current }. bot: the chosen bot (from ?bot=, else the first bot), carried by
 * every link; null leaves the links without it.
 */
export function menuItems(pathname, { bot = null } = {}) {
  const q = bot ? `?bot=${encodeURIComponent(bot)}` : "";
  return [
    { key: "overview", label: "Overview", href: `/${q}`, current: pathname === "/" },
    { key: "income", label: "Income", href: `/income${q}`, current: pathname === "/income" },
    { key: "worst-case", label: "Worst case", href: `/worst-case${q}`, current: pathname === "/worst-case" },
    { key: "positions", label: "Positions", href: `/positions${q}`, current: pathname === "/positions" },
    { key: "how-it-works", label: "How it works", href: `/how-it-works${q}`, current: pathname === "/how-it-works" },
    { key: "activity", label: "Activity", href: `/activity${q}`, current: pathname === "/activity" },
    { key: "account", label: "Account", href: `/account${q}`, current: pathname === "/account" },
    { key: "details", label: "Details (pro)", href: `/details${q}`, current: pathname === "/details" },
  ];
}
