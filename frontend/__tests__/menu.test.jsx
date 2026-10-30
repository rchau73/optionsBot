import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import MenuBar, { MenuNav } from "@/components/MenuBar";
import { ADMIN_PAGES, botParam, detailsRedirect, menuGroups, menuItems, showMenu } from "@/lib/menu";
import { usePathname, useSearchParams } from "next/navigation";
import { fetchMonitorConfig } from "@/lib/api/bots";

// The menu bar on every signed-in page (#385): Overview · Details (pro) ·
// Operational ▾ · Help ▾ (#450).

jest.mock("next/navigation", () => ({ usePathname: jest.fn(), useSearchParams: jest.fn() }));
jest.mock("@/lib/api/bots", () => ({ fetchMonitorConfig: jest.fn() }));
// The bell (#447): left loading here; __tests__/notifications.test.jsx covers it.
jest.mock("@/lib/api/notifications", () => ({ fetchNotifications: jest.fn(() => new Promise(() => {})) }));

const BAR = ["Overview", "Details (pro)", "Operational ▾", "Help ▾"];
const GROUP = { "Operational ▾": ["Positions", "Income", "Worst case", "Activity", "Account"], "Help ▾": ["How it works"] };

const at = (pathname, query = "") => {
  usePathname.mockReturnValue(pathname);
  useSearchParams.mockReturnValue(new URLSearchParams(query));
};

beforeEach(() => {
  fetchMonitorConfig.mockReset();
  fetchMonitorConfig.mockResolvedValue({ names: ["btc", "eth"], pollMs: 1000 });
});

describe("Scenario: every view is at most two clicks from any page", () => {
  // The last two items are the bell (#447) and the person's menu.
  const bar = (nav) => [...nav.querySelector("ul").children].slice(0, -2).map((li) => li.firstElementChild.textContent);
  test.each([
    ["/", "bot=btc", "Overview", null],
    ["/details", "bot=eth", "Details (pro)", null],
    ["/positions", "bot=eth", "Positions", "Operational ▾"],
    ["/income", "bot=eth", "Income", "Operational ▾"],
    ["/worst-case", "bot=eth", "Worst case", "Operational ▾"],
    ["/activity", "bot=eth", "Activity", "Operational ▾"],
    ["/account", "bot=eth", "Account", "Operational ▾"],
    ["/how-it-works", "bot=eth", "How it works", "Help ▾"],
  ])("on %s?%s the bar is Overview · Details (pro) · Operational ▾ · Help ▾ and marks %s", (pathname, query, current, group) => {
    at(pathname, query);
    render(<MenuBar />);
    const nav = screen.getByRole("navigation", { name: "Main" });
    expect(bar(nav)).toEqual(BAR);
    if (!group) {
      expect(within(nav).getByRole("link", { name: current })).toHaveAttribute("aria-current", "page");
      expect(within(nav).getByRole("link", { name: current })).toHaveClass("rounded-full", "bg-slate-700");
      return;
    }
    const button = within(nav).getByRole("button", { name: group });
    expect(button).toHaveClass("bg-slate-700");
    fireEvent.click(button);
    const menu = within(nav).getByRole("menu");
    expect(within(menu).getAllByRole("menuitem").map((i) => i.textContent)).toEqual(GROUP[group]);
    expect(within(menu).getByRole("menuitem", { name: current })).toHaveAttribute("aria-current", "page");
  });

  test("the chosen bot is kept from page to page", () => {
    at("/income", "bot=eth");
    render(<MenuBar />);
    const href = (name) => screen.getByRole("link", { name }).getAttribute("href");
    expect(href("Overview")).toBe("/?bot=eth");
    expect(href("Details (pro)")).toBe("/details?bot=eth");
    fireEvent.click(screen.getByRole("button", { name: "Operational ▾" }));
    const item = (name) => screen.getByRole("menuitem", { name }).getAttribute("href");
    expect(item("Positions")).toBe("/positions?bot=eth");
    expect(item("Worst case")).toBe("/worst-case?bot=eth");
    expect(item("Activity")).toBe("/activity?bot=eth");
    expect(item("Account")).toBe("/account?bot=eth");
    expect(fetchMonitorConfig).not.toHaveBeenCalled();
  });

  test("a dropdown closes on Escape, on a click outside and after choosing a page", () => {
    at("/", "bot=btc");
    render(<MenuBar />);
    const open = () => fireEvent.click(screen.getByRole("button", { name: "Help ▾" }));
    open();
    expect(screen.getByRole("button", { name: "Help ▾" })).toHaveAttribute("aria-expanded", "true");
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    open();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
    open();
    fireEvent.click(screen.getByRole("menuitem", { name: "How it works" }));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  test("without ?bot= the links take the first bot", async () => {
    at("/");
    render(<MenuBar />);
    await waitFor(() => expect(screen.getByRole("link", { name: "Details (pro)" })).toHaveAttribute("href", "/details?bot=btc"));
  });

  test("an admin page shows the menu with no view marked", () => {
    at("/admin/users", "bot=btc");
    render(<MenuBar />);
    expect(screen.getAllByRole("link").filter((l) => l.getAttribute("aria-current"))).toEqual([]);
  });

  test.each(["/login", "/reset"])("no menu on %s (signed out)", (pathname) => {
    at(pathname);
    const { container } = render(<MenuBar />);
    expect(container).toBeEmptyDOMElement();
    expect(fetchMonitorConfig).not.toHaveBeenCalled();
  });
});

// #394 Scenario: the person menu lives in the menu bar.
describe("Scenario: the person menu lives in the menu bar", () => {
  const open = (name) => fireEvent.click(screen.getByRole("button", { name }));
  const items = () => within(screen.getByRole("menu")).getAllByRole("menuitem").map((i) => i.textContent);

  test("the bar ends with my name; its menu holds Change password, Delete my account (#543) and Sign out", () => {
    at("/", "bot=btc");
    render(<MenuBar person={{ username: "bob", role: "user" }} />);
    const nav = screen.getByRole("navigation", { name: "Main" });
    const last = nav.querySelector("ul").lastElementChild;
    expect(within(last).getByRole("button", { name: "bob ▾" })).toHaveClass("min-h-11");
    open("bob ▾");
    expect(items()).toEqual(["Change password", "Delete my account", "Sign out"]);
  });

  test("for an admin it also holds Users, Audit and Analytics (#445)", () => {
    at("/", "bot=btc");
    render(<MenuBar person={{ username: "ana", role: "admin" }} />);
    open("ana ▾");
    expect(items()).toEqual(["Users", "Audit", "Analytics", "Change password", "Delete my account", "Sign out"]);
    expect(screen.getByRole("menuitem", { name: "Users" })).toHaveAttribute("href", "/admin/users");
    expect(screen.getByRole("menuitem", { name: "Analytics" })).toHaveAttribute("href", "/admin/analytics");
  });

  // #445 Scenario: not an admin — the menu has no Analytics item.
  test.each(["manager", "user"])("a %s's menu has no Analytics item", (role) => {
    at("/", "bot=btc");
    render(<MenuBar person={{ username: "bob", role }} />);
    open("bob ▾");
    expect(items()).not.toContain("Analytics");
    expect(items()).toEqual(["Change password", "Delete my account", "Sign out"]);
  });

  test("an unreadable session still offers the menu, as Account ▾", () => {
    at("/", "bot=btc");
    render(<MenuBar />);
    expect(screen.getByRole("button", { name: "Account ▾" })).toBeInTheDocument();
  });

  test("Account is a menu item placed after Activity", () => {
    const keys = menuItems("/", {}).map((i) => i.key);
    expect(keys.indexOf("account")).toBe(keys.indexOf("activity") + 1);
  });
});

describe("Scenario: phone", () => {
  test("the items wrap, each a 44 px target, so the menu fits 390 px without sideways scroll", () => {
    render(<MenuNav pathname="/activity" bot="btc" />);
    const nav = screen.getByRole("navigation", { name: "Main" });
    expect(nav.querySelector("ul")).toHaveClass("flex-wrap");
    expect(nav.querySelector("ul")).not.toHaveClass("overflow-x-auto");
    for (const l of within(nav).getAllByRole("link")) expect(l).toHaveClass("min-h-11");
    for (const b of within(nav).getAllByRole("button")) expect(b).toHaveClass("min-h-11");
    fireEvent.click(within(nav).getByRole("button", { name: "Operational ▾" }));
    for (const i of within(nav).getAllByRole("menuitem")) expect(i).toHaveClass("min-h-11");
  });
});

describe("lib/menu", () => {
  test("menuItems without a bot has no ?bot=", () => {
    expect(menuItems("/", {}).map((i) => i.href)).toEqual([
      "/",
      "/details",
      "/positions",
      "/income",
      "/worst-case",
      "/activity",
      "/account",
      "/how-it-works",
    ]);
  });

  test("menuGroups: two pages, then Operational and Help with their pages", () => {
    const g = menuGroups("/income", {});
    expect(g.map((e) => e.key)).toEqual(["overview", "details", "operational", "help"]);
    expect(g[2].items.map((i) => i.key)).toEqual(["positions", "income", "worst-case", "activity", "account"]);
    expect(g[3].items.map((i) => i.key)).toEqual(["how-it-works"]);
    expect(g.filter((e) => e.current).map((e) => e.key)).toEqual(["operational"]);
  });

  // #405 Scenario: Details (pro) is its own page; the old link redirects there.
  test("/?view=details redirects to /details, keeping a valid ?bot=", () => {
    expect(detailsRedirect({ view: "details" })).toBe("/details");
    expect(detailsRedirect({ view: "details", bot: "ETH" })).toBe("/details?bot=eth");
    expect(detailsRedirect({ view: "details", bot: "eth&x=1" })).toBe("/details");
    expect(detailsRedirect({ bot: "eth" })).toBeNull();
    expect(detailsRedirect({ view: "other" })).toBeNull();
    expect(detailsRedirect()).toBeNull();
  });

  test("the overview page redirects /?view=details", async () => {
    const redirect = jest.fn(() => {
      throw new Error("NEXT_REDIRECT");
    });
    jest.resetModules();
    jest.doMock("next/navigation", () => ({ redirect }));
    jest.doMock("@/components/Monitor", () => () => null);
    const { default: Page } = require("@/app/page");
    await expect(Page({ searchParams: Promise.resolve({ view: "details", bot: "btc" }) })).rejects.toThrow("NEXT_REDIRECT");
    expect(redirect).toHaveBeenCalledWith("/details?bot=btc");
    redirect.mockClear();
    const page = await Page({ searchParams: Promise.resolve({ bot: "btc" }) });
    expect(redirect).not.toHaveBeenCalled();
    expect(page.props.bot).toBe("btc");
  });

  test("Details (pro) is marked current on /details only", () => {
    expect(menuItems("/details", {}).filter((i) => i.current).map((i) => i.key)).toEqual(["details"]);
    expect(menuItems("/", {}).filter((i) => i.current).map((i) => i.key)).toEqual(["overview"]);
  });

  // Product owner, 2026-10-27: a page is in the menu, or here with its reason.
  const NOT_IN_MENU = {
    "/login": "signed out: no menu",
    "/reset": "signed out (a reset link): no menu",
    "/admin/users": "admins only, from the person's menu (the menu bar's last item)",
    "/admin/audit": "admins only, from the person's menu (the menu bar's last item)",
    "/admin/analytics": "admins only, from the person's menu (the menu bar's last item)",
    "/notifications": "the bell's list as a page (#447): the bell before the person's menu opens it on a phone, its dropdown links it",
  };

  test("every admin page of the person's menu is on the allow-list", () => {
    for (const p of ADMIN_PAGES) expect(NOT_IN_MENU[p.href]).toMatch(/^admins only/);
  });

  test("every page is in the menu or on the allow-list with its reason", () => {
    const fs = jest.requireActual("node:fs");
    const path = jest.requireActual("node:path");
    const app = path.join(__dirname, "..", "app");
    const pages = [];
    const walk = (dir) => {
      for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
        const full = path.join(dir, e.name);
        if (e.isDirectory() && e.name !== "api") walk(full);
        else if (e.name === "page.jsx") pages.push(`/${path.relative(app, dir).split(path.sep).join("/")}`.replace(/\/$/, "") || "/");
      }
    };
    walk(app);
    const inMenu = menuItems("/", {}).map((i) => i.href.split(/[?#]/)[0]);
    expect(pages).toEqual(expect.arrayContaining(["/", "/positions"]));
    expect(pages.filter((p) => !inMenu.includes(p) && !NOT_IN_MENU[p])).toEqual([]);
  });

  test("botParam accepts a bot name only", () => {
    expect(botParam("BTC")).toBe("btc");
    expect(botParam("btc&x=1")).toBeNull();
    expect(botParam(undefined)).toBeNull();
  });

  test("showMenu hides only the signed-out pages", () => {
    expect(showMenu("/")).toBe(true);
    expect(showMenu("/admin/audit")).toBe(true);
    expect(showMenu("/login")).toBe(false);
    expect(showMenu("/reset")).toBe(false);
  });
});
