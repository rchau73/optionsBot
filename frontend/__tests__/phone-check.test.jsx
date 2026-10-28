// The viewport check (#309, #425): the pages it measures (every menu page and
// the assistant), the in-browser measurement (e2e/measure.js) on a page with a
// faked layout, the comparison with the per-page baseline (e2e/phone.js), and
// that the check reads demo data only and commits nothing. The browser run itself is `npm run test:phone` (Playwright).
import fs from "node:fs";
import path from "node:path";
import { render } from "@testing-library/react";
import Panel from "@/components/Panel";
import PendingOrders from "@/components/PendingOrders";
import { measureDialog, measurePage } from "@/e2e/measure";
import { baselineNotes, baselineOf, compareToBaseline, normaliseLabel, summariseDialog, summariseWidth, viewportPages, WIDTHS } from "@/e2e/phone";
import { menuItems } from "@/lib/menu";
import { bot } from "@/test/fixtures";

const ROOT = path.join(__dirname, "..");
const read = (f) => fs.readFileSync(path.join(ROOT, f), "utf8");

// jsdom has no layout: fake one where the Working orders table is 540 px
// wide in a 360 px page, buttons are 50 × 20 px and everything else 100 × 30.
const shown = (el) => {
  for (let e = el; e; e = e.parentElement) if (e.style?.display === "none") return false;
  return true;
};
function fakeLayout() {
  const wideTable = () => [...document.querySelectorAll("section")].find((s) => s.textContent.includes("Working orders"))?.querySelector("table");
  const de = document.documentElement;
  Object.defineProperty(de, "clientWidth", { configurable: true, get: () => 360 });
  Object.defineProperty(de, "scrollWidth", { configurable: true, get: () => (wideTable() && shown(wideTable()) ? 360 + 230 : 360) });
  jest.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function rect() {
    const [w, h] = this.tagName === "BUTTON" ? [50, 20] : this.tagName === "TABLE" ? [540, 60] : [100, 30];
    return { width: w, height: h, top: 0, left: 0, right: w, bottom: h, x: 0, y: 0 };
  });
}

function MonitorLike() {
  return (
    <>
      <header>
        <button type="button">Account ▾</button>
      </header>
      <main>
        <Panel title="Open positions">
          <span title="Gamma: how fast delta moves">Γ</span>
        </Panel>
        <Panel title="Working orders (placed, not filled yet)">
          <PendingOrders bots={[bot()]} />
        </Panel>
      </main>
    </>
  );
}

afterEach(() => jest.restoreAllMocks());

describe("Scenario: the known overflow is measured", () => {
  test("at 360 px the report names the Working orders table as causing page overflow", () => {
    render(<MonitorLike />);
    fakeLayout();
    const w = summariseWidth(WIDTHS[0], measurePage());
    expect(w.overflow.overflow_px).toBe(230);
    expect(w.overflow.causes).toEqual([{ panel: "Working orders (placed, not filled yet)", element: "table", width_px: 540 }]);
    // Hiding to measure is undone: the table is shown again.
    expect(shown(document.querySelector("table"))).toBe(true);
  });

  test("no overflow, no cause; small controls and hover-only titles are counted", () => {
    render(<MonitorLike />);
    fakeLayout();
    Object.defineProperty(document.documentElement, "scrollWidth", { configurable: true, get: () => 360 });
    const w = summariseWidth(WIDTHS[0], measurePage());
    expect(w.overflow).toMatchObject({ overflow_px: 0, causes: [] });
    expect(w.controls.under_24_kinds).toEqual([{ key: 'Header › button "Account ▾"', width: 50, height: 20, count: 1 }]);
    expect(w.controls.under_44).toBe(1);
    // The Timeout header explains itself only by title; so does Γ.
    expect(w.title_only.kinds).toEqual(["Open positions › Γ", "Working orders (placed, not filled yet) › Timeout"]);
  });

  test("the assistant panel's controls are named by the panel, and its header is not the page's", () => {
    render(
      <>
        <header>
          <button type="button">Account ▾</button>
        </header>
        <aside aria-label="Assistant">
          <header>
            <h2>Assistant</h2>
            <button type="button" aria-label="Close the assistant">
              ×
            </button>
          </header>
        </aside>
      </>,
    );
    fakeLayout();
    const w = summariseWidth(WIDTHS[0], measurePage());
    expect(w.controls.under_24_kinds.map((k) => k.key)).toEqual(['Header › button "Account ▾"', 'Panel: Assistant › button "Close the assistant"']);
  });

  test("an overflow outside any panel is named by its block of <main>", () => {
    render(
      <main>
        <div className="wide">wide</div>
      </main>,
    );
    const de = document.documentElement;
    Object.defineProperty(de, "clientWidth", { configurable: true, get: () => 360 });
    Object.defineProperty(de, "scrollWidth", { configurable: true, get: () => (shown(document.querySelector(".wide")) ? 500 : 360) });
    const w = summariseWidth(WIDTHS[0], measurePage());
    expect(w.overflow.causes).toEqual([{ panel: "Page", element: "div", width_px: 0 }]);
  });

  test("a dialog taller than the screen does not fit", () => {
    render(
      <div role="dialog">
        <div>box</div>
      </div>,
    );
    jest.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({ width: 328, height: 450, top: -45, bottom: 405, left: 16, right: 344 });
    expect(summariseDialog({ width: 640, height: 360 }, measureDialog())).toMatchObject({ opened: true, fits: false, height_px: 450 });
    expect(summariseDialog({ width: 360, height: 640 }, { width: 328, height: 450, top: 95, bottom: 545, left: 16, right: 344 }).fits).toBe(true);
    expect(summariseDialog({ width: 360, height: 640 }, null)).toEqual({ opened: false, fits: false });
  });
});

// One width's measurement as summariseWidth builds it, for the comparison tests.
const WORKING_ORDERS = { panel: "Working orders (placed, not filled yet)", element: "table", width_px: 540 };
function width({ under24 = [], causes = [WORKING_ORDERS], overflow = 230, header = 420, titles = [] } = {}) {
  return {
    overflow: { overflow_px: overflow, causes },
    controls: {
      under_24_kinds: [{ key: 'Activity › button[tab] "All"', width: 30, height: 20, count: 4 }, ...under24],
      under_44_kinds: [{ key: 'Activity › button[tab] "All"', width: 30, height: 20, count: 4 }, ...under24],
    },
    title_only: { kinds: titles },
    header_height_px: header,
  };
}
const CLEAN = { causes: [], overflow: 0 };

// A report: the overview with today's overflow, the income page clean, at every width.
function report({ fits = true, ...overview } = {}) {
  const widths = (opts) => ({ 360: width(opts), 768: width(opts), 1280: width(opts) });
  return {
    pages: { overview: { widths: widths(overview) }, income: { widths: widths(CLEAN) } },
    dialogs: { close: { portrait: { fits }, landscape: { fits: false } } },
  };
}

describe("Scenario: a page listed in menu.js is checked at 360 px", () => {
  const baseline = baselineOf(report());

  test("a page whose document width equals the viewport passes", () => {
    expect(baseline.pages.income.widths[360]).toMatchObject({ overflow_px: 0, overflow_causes: [] });
    expect(compareToBaseline(baseline, report())).toEqual([]);
  });

  test("an overflow in the baseline passes; the same overflow on a page where it is not fails naming page, viewport and element", () => {
    const now = report();
    now.pages.income.widths[360] = width();
    expect(compareToBaseline(baseline, now)).toEqual([
      "income 360 px: page overflow now caused by Working orders (placed, not filled yet) › table (540 px wide)",
      "income 360 px: page overflows by 230 px, baseline 0 px",
    ]);
  });

  test("the widths measured are 360, 768 and 1280 px", () => {
    expect(WIDTHS.map((w) => w.width)).toEqual([360, 768, 1280]);
  });
});

describe("Scenario: a new page added to menu.js is checked without editing the test", () => {
  test("the pages measured are the menu's, then the assistant panel opened", () => {
    expect(viewportPages()).toEqual([...menuItems("/").map((m) => ({ name: m.key, path: m.href, assistant: false })), { name: "assistant", path: "/", assistant: true }]);
    expect(viewportPages().map((p) => p.path)).toEqual(expect.arrayContaining(["/", "/income", "/details"]));
  });

  test("a page added to the menu is in the list", () => {
    jest.isolateModules(() => {
      jest.doMock("@/lib/menu", () => ({ menuItems: () => [{ key: "overview", href: "/" }, { key: "news", href: "/news" }] }));
      const { viewportPages: pages } = require("@/e2e/phone");
      expect(pages().map((p) => p.name)).toEqual(["overview", "news", "assistant"]);
    });
    jest.dontMock("@/lib/menu");
  });

  test("the spec measures viewportPages(), not a typed list", () => {
    const spec = read("e2e/phone.spec.js");
    expect(spec).toMatch(/for \(const target of viewportPages\(\)\)/);
    expect(spec).not.toMatch(/"\/income"|"\/details"|"\/positions"/);
  });

  test("a new page not in the baseline: clean passes, held to what the recorded pages accept", () => {
    const baseline = baselineOf(report());
    const now = report();
    now.pages.news = { widths: { 360: width(CLEAN), 768: width(CLEAN), 1280: width(CLEAN) } };
    // Its shared controls (the overview's known small tab) are not new.
    expect(compareToBaseline(baseline, now)).toEqual([]);
    now.pages.news.widths[360] = width({ causes: [{ panel: "Headlines", element: "table", width_px: 700 }], overflow: 340, under24: [{ key: 'Headlines › a "More"', width: 30, height: 16, count: 1 }] });
    expect(compareToBaseline(baseline, now)).toEqual([
      "news 360 px: page overflow now caused by Headlines › table (700 px wide)",
      "news 360 px: page overflows by 340 px, baseline 230 px",
      'news 360 px: new control under 24 px: Headlines › a "More" (30×16 px)',
      'news 360 px: new control under 44 px: Headlines › a "More" (30×16 px)',
    ]);
  });
});

describe("Scenario: a new overflow not in the baseline fails naming page, viewport and element", () => {
  const baseline = baselineOf(report());

  test("today's failures pass", () => {
    expect(compareToBaseline(baseline, report())).toEqual([]);
  });

  test("a control added under 24 px at 768 px fails naming the page and the control", () => {
    const now = report();
    now.pages.overview.widths[768].controls.under_24_kinds.push({ key: 'Open positions › button "Hide"', width: 40, height: 18, count: 1 });
    expect(compareToBaseline(baseline, now)).toEqual(['overview 768 px: new control under 24 px: Open positions › button "Hide" (40×18 px)']);
  });

  test("more rows of a known control are not a regression (demo data varies)", () => {
    const now = report();
    now.pages.overview.widths[768].controls.under_24_kinds[0].count = 40;
    expect(compareToBaseline(baseline, now)).toEqual([]);
  });

  test("a new overflow cause, more overflow, a taller header, a new title, a dialog that stops fitting all fail", () => {
    const now = report({
      causes: [WORKING_ORDERS, { panel: "Activity", element: "panel", width_px: 500 }],
      overflow: 230 + 17,
      header: 420 + 25,
      titles: ["Header › LOOP STALLED"],
      fits: false,
    });
    const problems = compareToBaseline(baseline, now);
    expect(problems).toContain("overview 360 px: page overflow now caused by Activity › panel (500 px wide)");
    expect(problems).toContain("overview 360 px: page overflows by 247 px, baseline 230 px");
    expect(problems).toContain("overview 768 px: header 445 px tall, baseline 420 px");
    expect(problems).toContain("overview 1280 px: new hover-only explanation (title): Header › LOOP STALLED");
    expect(problems).toContain("close dialog no longer fits portrait");
  });

  test("within the tolerances, and better than the baseline, passes", () => {
    expect(compareToBaseline(baseline, report({ overflow: 230 + 16, header: 420 + 24, causes: [], fits: true }))).toEqual([]);
  });

  test("a width missing from a recorded page fails", () => {
    const partial = { pages: { overview: { widths: { 360: baseline.pages.overview.widths[360] } }, income: baseline.pages.income }, dialogs: {} };
    expect(compareToBaseline(partial, report())).toEqual([
      "overview 768 px: not in the baseline (record it with UPDATE_PHONE_BASELINE=1)",
      "overview 1280 px: not in the baseline (record it with UPDATE_PHONE_BASELINE=1)",
    ]);
  });

  test("the baseline's entries become follow-up notes: page, viewport, element", () => {
    const notes = baselineNotes(baselineOf(report()));
    expect(notes).toContain("overview 360 px: page overflows by 230 px, caused by Working orders (placed, not filled yet) › table");
    expect(notes).toContain('income 1280 px: control under 24 px: Activity › button[tab] "All"');
    expect(notes).toHaveLength(9); // overview: 3 × (overflow + control); income: 3 × control
  });

  test("labels are stable across demo runs: numbers become #", () => {
    expect(normaliseLabel("  Show 50\n more ")).toBe("Show # more");
    expect(normaliseLabel("IM 12.4 % at 10:42")).toBe("IM # % at #");
    expect(normaliseLabel("x".repeat(60))).toHaveLength(40);
  });
});

describe("Scenario: nothing private", () => {
  const config = read("e2e/playwright.config.mjs");

  test("the monitor reads the two demo bots only, with a throwaway in-memory sign-in", () => {
    const bots = config.match(/BOT_APIS: `([^`]+)`/)[1];
    expect(bots).toBe("btc=http://127.0.0.1:${DEMO_PORTS.btc},eth=http://127.0.0.1:${DEMO_PORTS.eth}");
    expect(config).toMatch(/go run \.\/cmd\/monitor-demo -underlying/);
    expect(config).toMatch(/AUTH_DB_PATH: ":memory:"/);
    expect(config).toMatch(/PHONE_ADMIN_PASSWORD \?\?= crypto\.randomBytes/);
    expect(config).toMatch(/PHONE_AUTH_SECRET \?\?= crypto\.randomBytes/);
    for (const off of ["AUDIT_PATH", "ANALYTICS_SALT", "EVENTS_PATH", "MONITOR_API_TOKEN"]) expect(config).toMatch(new RegExp(`${off}: ""`));
  });

  test("screenshots and the report are git-ignored and kept out of the image", () => {
    const ignore = read(".gitignore").split("\n");
    expect(ignore).toEqual(expect.arrayContaining(["test-results/", "docs/responsive-measured.json"]));
    expect(read(".dockerignore").split("\n")).toEqual(expect.arrayContaining(["e2e", "test-results"]));
    const spec = read("e2e/phone.spec.js");
    expect(spec).toMatch(/SHOTS = path\.join\(FRONTEND, "test-results", "phone"\)/);
    expect(spec).toMatch(/REPORT = path\.join\(FRONTEND, "docs", "responsive-measured\.json"\)/);
    expect(spec).not.toMatch(/fullPage[^\n]*path:(?![^\n]*SHOTS)/);
  });

  test("npm test stays Jest only", () => {
    const pkg = JSON.parse(read("package.json"));
    expect(pkg.scripts.test).toBe("jest");
    expect(pkg.scripts["test:phone"]).toBe("playwright test --config e2e/playwright.config.mjs");
    expect(read("jest.config.mjs")).toMatch(/testMatch: \["<rootDir>\/__tests__\/\*\*\/\*\.test\.\{js,jsx\}"\]/);
  });
});
