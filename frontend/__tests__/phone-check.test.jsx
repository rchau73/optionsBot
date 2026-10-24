// The phone-width check (#309): the in-browser measurement (e2e/measure.js)
// on a page with a faked layout, the comparison with the baseline
// (e2e/phone.js), and that the check reads demo data only and commits
// nothing. The browser run itself is `npm run test:phone` (Playwright).
import fs from "node:fs";
import path from "node:path";
import { render } from "@testing-library/react";
import Panel from "@/components/Panel";
import PendingOrders from "@/components/PendingOrders";
import { measureDialog, measurePage } from "@/e2e/measure";
import { baselineOf, compareToBaseline, normaliseLabel, summariseDialog, summariseWidth, WIDTHS } from "@/e2e/phone";
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

// A report as summariseWidth builds it, for the comparison tests.
function report({ under24 = [], causes = [{ panel: "Working orders (placed, not filled yet)", element: "table", width_px: 540 }], overflow = 230, header = 420, titles = [], fits = true } = {}) {
  const width = () => ({
    overflow: { overflow_px: overflow, causes },
    controls: {
      under_24_kinds: [{ key: 'Activity › button[tab] "All"', width: 30, height: 20, count: 4 }, ...under24],
      under_44_kinds: [{ key: 'Activity › button[tab] "All"', width: 30, height: 20, count: 4 }, ...under24],
    },
    title_only: { kinds: titles },
    header_height_px: header,
  });
  return {
    widths: { 360: width(), 390: width(), 768: width() },
    dialogs: { close: { portrait: { fits }, landscape: { fits: false } } },
  };
}

describe("Scenario: no regression", () => {
  const baseline = baselineOf(report());

  test("today's failures pass", () => {
    expect(compareToBaseline(baseline, report())).toEqual([]);
  });

  test("a control added under 24 px at 390 px fails naming the control", () => {
    const now = report();
    now.widths[390].controls.under_24_kinds.push({ key: 'Open positions › button "Hide"', width: 40, height: 18, count: 1 });
    expect(compareToBaseline(baseline, now)).toEqual(['390 px: new control under 24 px: Open positions › button "Hide" (40×18 px)']);
  });

  test("more rows of a known control are not a regression (demo data varies)", () => {
    const now = report();
    now.widths[390].controls.under_24_kinds[0].count = 40;
    expect(compareToBaseline(baseline, now)).toEqual([]);
  });

  test("a new overflow cause, more overflow, a taller header, a new title, a dialog that stops fitting all fail", () => {
    const now = report({
      causes: [...report().widths[360].overflow.causes, { panel: "Activity", element: "panel", width_px: 500 }],
      overflow: 230 + 17,
      header: 420 + 25,
      titles: ["Header › LOOP STALLED"],
      fits: false,
    });
    const problems = compareToBaseline(baseline, now);
    expect(problems).toContain("360 px: page overflow now caused by Activity › panel (500 px wide)");
    expect(problems).toContain("360 px: page overflows by 247 px, baseline 230 px");
    expect(problems).toContain("768 px: header 445 px tall, baseline 420 px");
    expect(problems).toContain("390 px: new hover-only explanation (title): Header › LOOP STALLED");
    expect(problems).toContain("close dialog no longer fits portrait");
  });

  test("within the tolerances, and better than the baseline, passes", () => {
    expect(compareToBaseline(baseline, report({ overflow: 230 + 16, header: 420 + 24, causes: [], fits: true }))).toEqual([]);
  });

  test("a width missing from the baseline fails", () => {
    const partial = { widths: { 360: baseline.widths[360] }, dialogs: {} };
    expect(compareToBaseline(partial, report())).toEqual([
      "390 px: not in the baseline (record it with UPDATE_PHONE_BASELINE=1)",
      "768 px: not in the baseline (record it with UPDATE_PHONE_BASELINE=1)",
    ]);
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
