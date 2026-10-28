import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import MenuBar, { MenuNav } from "@/components/MenuBar";
import { botParam, detailsRedirect, menuItems, showMenu } from "@/lib/menu";
import { usePathname, useSearchParams } from "next/navigation";
import { fetchMonitorConfig } from "@/lib/api";

// The menu bar on every signed-in page (#385).

jest.mock("next/navigation", () => ({ usePathname: jest.fn(), useSearchParams: jest.fn() }));
jest.mock("@/lib/api", () => ({ fetchMonitorConfig: jest.fn() }));

const LABELS = ["Overview", "Income", "Worst case", "Positions", "How it works", "Activity", "Account", "Details (pro)"];

const at = (pathname, query = "") => {
  usePathname.mockReturnValue(pathname);
  useSearchParams.mockReturnValue(new URLSearchParams(query));
};

beforeEach(() => {
  fetchMonitorConfig.mockReset();
  fetchMonitorConfig.mockResolvedValue({ names: ["btc", "eth"], pollMs: 1000 });
});

describe("Scenario: every view is one click from any page", () => {
  test.each([
    ["/", "bot=btc", "Overview"],
    ["/income", "bot=eth", "Income"],
    ["/worst-case", "bot=eth", "Worst case"],
    ["/positions", "bot=eth", "Positions"],
    ["/how-it-works", "bot=eth", "How it works"],
    ["/activity", "bot=eth", "Activity"],
    ["/account", "bot=eth", "Account"],
    ["/details", "bot=eth", "Details (pro)"],
  ])("on %s?%s the menu shows every view and marks %s", (pathname, query, current) => {
    at(pathname, query);
    render(<MenuBar />);
    const nav = screen.getByRole("navigation", { name: "Main" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((l) => l.textContent)).toEqual(LABELS);
    expect(links.filter((l) => l.getAttribute("aria-current") === "page").map((l) => l.textContent)).toEqual([current]);
    expect(within(nav).getByRole("link", { name: current })).toHaveClass("rounded-full", "bg-slate-700");
  });

  test("the chosen bot is kept from page to page", () => {
    at("/income", "bot=eth");
    render(<MenuBar />);
    const href = (name) => screen.getByRole("link", { name }).getAttribute("href");
    expect(href("Overview")).toBe("/?bot=eth");
    expect(href("Worst case")).toBe("/worst-case?bot=eth");
    expect(href("Positions")).toBe("/positions?bot=eth");
    expect(href("How it works")).toBe("/how-it-works?bot=eth");
    expect(href("Activity")).toBe("/activity?bot=eth");
    expect(href("Account")).toBe("/account?bot=eth");
    expect(href("Details (pro)")).toBe("/details?bot=eth");
    expect(fetchMonitorConfig).not.toHaveBeenCalled();
  });

  test("without ?bot= the links take the first bot", async () => {
    at("/");
    render(<MenuBar />);
    await waitFor(() => expect(screen.getByRole("link", { name: "Income" })).toHaveAttribute("href", "/income?bot=btc"));
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

  test("the bar ends with my name; its menu holds Change password and Sign out", () => {
    at("/", "bot=btc");
    render(<MenuBar person={{ username: "bob", role: "user" }} />);
    const nav = screen.getByRole("navigation", { name: "Main" });
    const last = nav.querySelector("ul").lastElementChild;
    expect(within(last).getByRole("button", { name: "bob ▾" })).toHaveClass("min-h-11");
    open("bob ▾");
    expect(items()).toEqual(["Change password", "Sign out"]);
  });

  test("for an admin it also holds Users and Audit", () => {
    at("/", "bot=btc");
    render(<MenuBar person={{ username: "ana", role: "admin" }} />);
    open("ana ▾");
    expect(items()).toEqual(["Users", "Audit", "Change password", "Sign out"]);
    expect(screen.getByRole("menuitem", { name: "Users" })).toHaveAttribute("href", "/admin/users");
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
  });
});

describe("lib/menu", () => {
  test("menuItems without a bot has no ?bot=", () => {
    expect(menuItems("/", {}).map((i) => i.href)).toEqual([
      "/",
      "/income",
      "/worst-case",
      "/positions",
      "/how-it-works",
      "/activity",
      "/account",
      "/details",
    ]);
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
  };

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
