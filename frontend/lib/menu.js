// The menu bar on every signed-in page (#385, E3 prototype screens 4 and 7):
// one link per view, the current one marked, the chosen bot kept from page to
// page through ?bot=.

/** Pages without the menu: signed out (sign-in, set a new password with a reset link). */
const NO_MENU = ["/login", "/reset"];

/** Whether pathname shows the menu. */
export function showMenu(pathname) {
  return !NO_MENU.some((p) => pathname === p || pathname?.startsWith(`${p}/`));
}

/** ?view= value that opens the overview's detail panels (Details (pro)). */
export const DETAILS_VIEW = "details";

/** A bot name as the pages accept it (letters, digits, _ and -), lower-case; null otherwise. */
export function botParam(value) {
  return typeof value === "string" && /^[a-z0-9_-]+$/i.test(value) ? value.toLowerCase() : null;
}

/**
 * The menu's items for the page at pathname (with ?view=), each
 * { key, label, href, current }. bot: the chosen bot (from ?bot=, else the
 * first bot), carried by every link; null leaves the links without it.
 */
export function menuItems(pathname, { bot = null, view = null } = {}) {
  const q = bot ? `?bot=${encodeURIComponent(bot)}` : "";
  const onOverview = pathname === "/";
  const details = onOverview && view === DETAILS_VIEW;
  return [
    { key: "overview", label: "Overview", href: `/${q}`, current: onOverview && !details },
    { key: "income", label: "Income", href: `/income${q}`, current: pathname === "/income" },
    { key: "worst-case", label: "Worst case", href: `/worst-case${q}`, current: pathname === "/worst-case" },
    { key: "positions", label: "Positions", href: `/positions${q}`, current: pathname === "/positions" },
    { key: "how-it-works", label: "How it works", href: `/how-it-works${q}`, current: pathname === "/how-it-works" },
    { key: "activity", label: "Activity", href: `/activity${q}`, current: pathname === "/activity" },
    {
      key: "details",
      label: "Details (pro)",
      href: `/${q ? `${q}&` : "?"}view=${DETAILS_VIEW}#details`,
      current: details,
    },
  ];
}
