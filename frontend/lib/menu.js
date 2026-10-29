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
  const item = ({ key, label, path, group = null }) => ({ key, label, href: `${path}${q}`, current: pathname === path, group });
  return [
    { key: "overview", label: "Overview", path: "/" },
    { key: "details", label: "Details (pro)", path: "/details" },
    { key: "positions", label: "Positions", path: "/positions", group: "operational" },
    { key: "income", label: "Income", path: "/income", group: "operational" },
    { key: "worst-case", label: "Worst case", path: "/worst-case", group: "operational" },
    { key: "activity", label: "Activity", path: "/activity", group: "operational" },
    { key: "account", label: "Account", path: "/account", group: "operational" },
    { key: "how-it-works", label: "How it works", path: "/how-it-works", group: "help" },
  ].map(item);
}

/**
 * The admin pages of the person's menu (UserMenu.jsx), in order: shown to
 * admins only, and only as links — each page's route re-checks the role.
 * Users (#306), Audit (#308), Analytics (#445). href first: the monitor
 * guide's test reads `{ key, label }` objects as menu-bar pages (a ## section
 * each), and these as admin pages (a `<!-- guide: page:<href> -->` marker in
 * frontend/content/monitor-guide.md, #483).
 */
export const ADMIN_PAGES = [
  { href: "/admin/users", key: "users", label: "Users" },
  { href: "/admin/audit", key: "audit", label: "Audit" },
  { href: "/admin/analytics", key: "analytics", label: "Analytics" },
];

/** The dropdowns of the menu bar, in order (#450): group key → label. Not pages, so not in the monitor guide's page list. */
const GROUPS = [
  ["operational", "Operational"],
  ["help", "Help"],
];

/**
 * The menu bar's entries in order: a page { key, label, href, current } or a
 * dropdown { key, label, current, items } (current: one of its pages is).
 */
export function menuGroups(pathname, { bot = null } = {}) {
  const items = menuItems(pathname, { bot });
  const entries = items.filter((i) => !i.group);
  for (const [key, label] of GROUPS) {
    const children = items.filter((i) => i.group === key);
    entries.push({ key, label, current: children.some((c) => c.current), items: children });
  }
  return entries;
}
