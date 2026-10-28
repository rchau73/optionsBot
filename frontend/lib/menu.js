// The menu bar on every signed-in page (#385, E3 prototype screens 4 and 7):
// Overview · Details (pro) · Operational ▾ · Help ▾ (#450), the current page
// marked, the chosen bot kept from page to page through ?bot=; the signed-in
// person's menu ends it (#394).

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
 * Every page of the menu in menu order, each { key, label, href, current,
 * group } (group: the dropdown it sits in, null on the bar itself). bot: the
 * chosen bot (from ?bot=, else the first bot), carried by every link; null
 * leaves the links without it.
 */
export function menuItems(pathname, { bot = null } = {}) {
  const q = bot ? `?bot=${encodeURIComponent(bot)}` : "";
  const item = (key, label, path, group = null) => ({ key, label, href: `${path}${q}`, current: pathname === path, group });
  return [
    item("overview", "Overview", "/"),
    item("details", "Details (pro)", "/details"),
    item("positions", "Positions", "/positions", "operational"),
    item("income", "Income", "/income", "operational"),
    item("worst-case", "Worst case", "/worst-case", "operational"),
    item("activity", "Activity", "/activity", "operational"),
    item("account", "Account", "/account", "operational"),
    item("how-it-works", "How it works", "/how-it-works", "help"),
  ];
}

/** The dropdowns of the menu bar, in order (#450). */
const GROUPS = [
  { key: "operational", label: "Operational" },
  { key: "help", label: "Help" },
];

/**
 * The menu bar's entries in order: a page { key, label, href, current } or a
 * dropdown { key, label, current, items } (current: one of its pages is).
 */
export function menuGroups(pathname, { bot = null } = {}) {
  const items = menuItems(pathname, { bot });
  const entries = items.filter((i) => !i.group);
  for (const g of GROUPS) {
    const children = items.filter((i) => i.group === g.key);
    entries.push({ ...g, current: children.some((c) => c.current), items: children });
  }
  return entries;
}
