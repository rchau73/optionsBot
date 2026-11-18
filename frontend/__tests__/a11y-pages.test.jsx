// Page-level accessibility baseline (#840, epic E12): each of the eight menu
// pages and the phone overview rendered whole — the menu bar above it, as the
// layout puts it — on the shared fixtures, and checked by axe for the
// screen-reader structure only: landmarks, regions, heading order, the names
// of buttons, links and fields, table headers and dialog names. Today's
// violations are kept in `a11y-pages-baseline.json` (page → rule id → nodes),
// every page with an entry even when it has none; a new rule or a higher count
// fails, a lower count asks for the baseline to be lowered with
// `UPDATE_A11Y=1 npm test` — never edit it by hand. The panels one by one are
// `a11y.test.jsx` (#310); colour contrast is `a11y-tokens.test.js` (#763).
import fs from "node:fs";
import path from "node:path";
import { cloneElement } from "react";
import { render, screen } from "@testing-library/react";
import { AccountView } from "@/components/AccountPage";
import { ActivityView } from "@/components/ActivityPage";
import DetailsPage from "@/components/DetailsPage";
import { HowItWorksScreen } from "@/components/HowItWorksView";
import { IncomeView } from "@/components/IncomePage";
import MenuBar from "@/components/MenuBar";
import Monitor from "@/components/Monitor";
import { PositionsScreen } from "@/components/PositionsPage";
import { WorstCaseScreen } from "@/components/WorstCaseView";
import { useMonitor } from "@/hooks/useMonitor";
import { usePathname, useSearchParams } from "next/navigation";
import { menuItems } from "@/lib/menu";
import { compareToBaseline, pageAxeCounts, UPDATE_COMMAND } from "@/test/a11y";
import DEMO from "@/test/demoScenarios.json";
import { incomeFor, payoffFor, tradesFor } from "@/test/resultFixtures";

jest.mock("@/hooks/useMonitor", () => ({ useMonitor: jest.fn() }));
jest.mock("@/lib/track", () => ({ track: jest.fn(() => true) }));
jest.mock("next/navigation", () => ({ usePathname: jest.fn(), useSearchParams: jest.fn(), useRouter: jest.fn(() => ({ push: jest.fn(), replace: jest.fn() })) }));
// The bell (#447) stays loading: notifications.test.jsx covers it.
jest.mock("@/lib/api/notifications", () => ({ fetchNotifications: jest.fn(() => new Promise(() => {})) }));
// The two charts that load their own history are left out; the others draw at a fixed size (jsdom has no layout).
jest.mock("@/components/OverviewPnl", () => () => null);
jest.mock("@/components/PnlChart", () => () => null);
jest.mock("recharts", () => {
  const actual = jest.requireActual("recharts");
  return { ...actual, ResponsiveContainer: ({ children }) => cloneElement(children, { width: 800, height: 400 }) };
});

const BASELINE_FILE = path.join(__dirname, "a11y-pages-baseline.json");
const UPDATE = process.env.UPDATE_A11Y === "1";
const baseline = fs.existsSync(BASELINE_FILE) ? JSON.parse(fs.readFileSync(BASELINE_FILE, "utf8")) : null;
const recorded = {};

/** The rules in scope (#840): page structure for a screen reader. Contrast is not one of them. */
const PAGE_RULES = [
  "aria-dialog-name",
  "button-name",
  "heading-order",
  "label",
  "landmark-banner-is-top-level",
  "landmark-complementary-is-top-level",
  "landmark-contentinfo-is-top-level",
  "landmark-main-is-top-level",
  "landmark-no-duplicate-banner",
  "landmark-no-duplicate-contentinfo",
  "landmark-no-duplicate-main",
  "landmark-one-main",
  "landmark-unique",
  "link-name",
  "region",
  "scope-attr-valid",
  "th-has-data-cells",
];

jest.setTimeout(30_000); // axe walks the whole page

// --- the shared fixtures: the demo's default scenario, as useMonitor serves it ---

const served = DEMO.default;
const NOW = Date.parse(served["/api/status"].as_of);
const account = {
  snapshot: {
    as_of: served["/api/status"].as_of, margin_model: "cross_pm", portfolio_margining: true, cross_collateral: true,
    totals: { equity_usd: 150000, margin_balance_usd: 150000, initial_margin_usd: 30000, maintenance_margin_usd: 21000, im_pct: 20, mm_pct: 14 },
    assets: [
      { currency: "BTC", balance: 1.2, equity: 1.25, margin_balance: 1.25, available_funds: 0.95, available_withdrawal_funds: 0.9,
        initial_margin: 0.3, maintenance_margin: 0.2, projected_initial_margin: 0.25, projected_maintenance_margin: 0.18, im_pct: 24, mm_pct: 16, spot_reserve: 0 },
    ],
  },
  error: null,
};
const bot = {
  name: "btc", stale: false, status: served["/api/status"], summary: served["/api/summary"], positions: served["/api/positions"],
  payoff: payoffFor("profit"), account,
};
const trades = tradesFor("profit");
const events = [
  { seq: 10, event: "filled", at: "2026-11-09T14:30:00Z", data: { instrument: "BTC-5DEC26-80000-P", option_type: "put", trigger_reason: "entry", qty: 1, fill_price: 0.0094, premium_received: 0.0094 }, plain: "Sold the put to open a new strangle and collect its premium (entry)." },
  { seq: 21, event: "skipped", at: "2026-11-10T02:05:00Z", data: { skip_reason: "gex_wait: no snapshot" }, plain: "No new risk yet: the GEX data is out of date, so the bot waits for a fresh reading." },
];

const monitorData = () => ({
  names: ["btc"], bots: [bot], feed: [], history: [], trades: { btc: trades }, tradesComplete: true, hedgeEvents: {}, error: null, pollMs: 1000, polledAt: NOW,
});

/** The menu bar above the page, as app/layout.jsx renders it, then the page. */
function page(pathname, content) {
  usePathname.mockReturnValue(pathname);
  useSearchParams.mockReturnValue(new URLSearchParams("bot=btc"));
  return render(
    <>
      <MenuBar person={{ username: "ana", role: "viewer" }} />
      {content}
    </>,
  );
}

/**
 * The overview at one width: jsdom applies no media query, so the layout the
 * width hides (sm:hidden on a desktop, hidden sm:block on a phone) is hidden
 * here as the browser would, and axe skips it.
 */
function overview(width) {
  const r = page("/", <Monitor bot="btc" />);
  const desktop = screen.getByTestId("desktop-monitor");
  const phone = desktop.previousElementSibling;
  (width === "phone" ? desktop : phone).style.display = "none";
  return r;
}

// --- the eight menu pages (lib/menu.js) and the phone overview ---

const PAGES = {
  Overview: () => overview("desktop"),
  "Phone overview": () => overview("phone"),
  "Details (pro)": () => page("/details", <DetailsPage />),
  Positions: () => page("/positions", <PositionsScreen names={["btc"]} bot="btc" payoff={bot.payoff} />),
  Income: () => page("/income", <IncomeView bot="btc" income={incomeFor("profit")} />),
  "Worst case": () => page("/worst-case", <WorstCaseScreen bot="btc" names={["btc"]} summary={bot.summary} />),
  Activity: () => page("/activity", <ActivityView bot="btc" events={events} trades={[]} now={NOW} />),
  Account: () => page("/account", <AccountView bots={[bot]} polledAt={NOW} />),
  "How it works": () => page("/how-it-works", <HowItWorksScreen bot="btc" names={["btc"]} summary={bot.summary} />),
};

const reply = (body) => ({ ok: true, status: 200, json: async () => body });

describe("page-level accessibility baseline (axe: screen-reader structure)", () => {
  const realFetch = global.fetch;
  beforeEach(() => {
    useMonitor.mockReturnValue(monitorData());
    // The Account page's own loads (hedge, consent, privacy) answer with nothing to show.
    global.fetch = jest.fn().mockResolvedValue(reply({}));
    window.localStorage.clear();
  });
  afterEach(() => {
    global.fetch = realFetch;
  });
  afterAll(() => {
    if (UPDATE) fs.writeFileSync(BASELINE_FILE, `${JSON.stringify(recorded, null, 2)}\n`);
  });

  // Scenario: every menu page is measured — and a new violation fails, a fixed one asks for the baseline to be lowered.
  test.each(Object.keys(PAGES))("%s has no violation beyond the baseline", async (name) => {
    PAGES[name]();
    await screen.findByRole("navigation", { name: "Main" });
    expect(screen.getAllByRole("heading", { level: 1 }).length).toBeGreaterThan(0); // the page itself rendered, not only the menu
    const counts = await pageAxeCounts(PAGE_RULES);
    recorded[name] = Object.fromEntries(Object.entries(counts).sort(([a], [b]) => a.localeCompare(b)));
    if (UPDATE) return;
    if (!baseline) throw new Error(`a11y-pages-baseline.json is missing: run ${UPDATE_COMMAND} and commit it`);
    expect(Object.keys(baseline)).toContain(name);
    expect(compareToBaseline(name, counts, baseline[name])).toEqual([]);
  });
});

describe("Scenario: every menu page is measured", () => {
  test("the pages are the menu's eight and the phone overview, each with a baseline entry", () => {
    const menu = menuItems("/").map((i) => i.label);
    expect(Object.keys(PAGES).sort()).toEqual([...menu, "Phone overview"].sort());
    if (!UPDATE) expect(Object.keys(baseline ?? {}).sort()).toEqual(Object.keys(PAGES).sort());
  });
});

const DOC = path.join(__dirname, "../../docs/product/responsive-a11y-baseline.md");

/** The rows of the doc's "Screen-reader structure" table: { rule, count, pages, nodes, fix }. */
function docFindings() {
  const section = fs.readFileSync(DOC, "utf8").split("## Screen-reader structure")[1]?.split("\n## ")[0] ?? "";
  return section
    .split("\n")
    .filter((l) => /^\| \d+ \|/.test(l))
    .map((l) => {
      const [, , , rule, affected, nodes, fix] = l.split("|").map((c) => c.trim());
      const [count, pages] = affected.split(/:\s*/);
      return { rule: rule.replace(/`/g, ""), count: Number(count), pages: pages.split(/,\s*/).sort(), nodes: Number(nodes), fix };
    });
}

describe("Scenario: the findings are ranked", () => {
  test("docs/product/responsive-a11y-baseline.md lists every pinned finding, ranked by pages affected, with its fix", () => {
    if (UPDATE) return;
    const pinned = {};
    for (const [page, rules] of Object.entries(baseline)) {
      for (const [rule, n] of Object.entries(rules)) {
        pinned[rule] ??= { rule, count: 0, pages: [], nodes: 0 };
        pinned[rule].count += 1;
        pinned[rule].pages.push(page);
        pinned[rule].nodes += n;
      }
    }
    const rows = docFindings();
    expect(rows.map(({ rule, count, pages, nodes }) => ({ rule, count, pages, nodes })).sort((a, b) => a.rule.localeCompare(b.rule))).toEqual(
      Object.values(pinned)
        .map((f) => ({ ...f, pages: f.pages.sort() }))
        .sort((a, b) => a.rule.localeCompare(b.rule)),
    );
    const counts = rows.map((r) => r.count);
    expect(counts).toEqual([...counts].sort((a, b) => b - a));
    for (const r of rows) expect([r.rule, r.fix.length > 20]).toEqual([r.rule, true]);
  });
});

describe("Scenario: a new violation fails", () => {
  test("an unnamed button added to a page fails naming the page, the rule and the node count", async () => {
    PAGES.Income();
    const today = await pageAxeCounts(PAGE_RULES);
    document.querySelector("main").appendChild(document.createElement("button"));
    const problems = compareToBaseline("Income", await pageAxeCounts(PAGE_RULES), today);
    expect(problems).toEqual(["Income: new violation button-name (1 node)"]);
  });
});

describe("Scenario: a fixed violation asks for the baseline to be lowered", () => {
  test("Positions with its nested aside fixed fails with the UPDATE_A11Y=1 command", async () => {
    PAGES.Positions();
    const pinned = await pageAxeCounts(PAGE_RULES);
    expect(pinned["landmark-complementary-is-top-level"]).toBe(1);
    document.querySelector("main aside").setAttribute("role", "group");
    expect(compareToBaseline("Positions", await pageAxeCounts(PAGE_RULES), pinned)).toEqual([
      `Positions: landmark-complementary-is-top-level fell from 1 to 0 — lower the baseline with ${UPDATE_COMMAND}`,
    ]);
  });
});
