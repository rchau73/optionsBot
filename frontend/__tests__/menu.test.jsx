import { render, screen, waitFor, within } from "@testing-library/react";
import MenuBar, { MenuNav } from "@/components/MenuBar";
import { botParam, menuItems, showMenu } from "@/lib/menu";
import { usePathname, useSearchParams } from "next/navigation";
import { fetchMonitorConfig } from "@/lib/api";

// The menu bar on every signed-in page (#385).

jest.mock("next/navigation", () => ({ usePathname: jest.fn(), useSearchParams: jest.fn() }));
jest.mock("@/lib/api", () => ({ fetchMonitorConfig: jest.fn() }));

const LABELS = ["Overview", "Income", "Worst case", "How it works", "Activity", "Details (pro)"];

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
    ["/how-it-works", "bot=eth", "How it works"],
    ["/activity", "bot=eth", "Activity"],
    ["/", "bot=eth&view=details", "Details (pro)"],
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
    expect(href("How it works")).toBe("/how-it-works?bot=eth");
    expect(href("Activity")).toBe("/activity?bot=eth");
    expect(href("Details (pro)")).toBe("/?bot=eth&view=details#details");
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
    expect(menuItems("/", {}).map((i) => i.href)).toEqual(["/", "/income", "/worst-case", "/how-it-works", "/activity", "/?view=details#details"]);
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
