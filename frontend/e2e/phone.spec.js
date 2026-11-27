// The viewport check (#309, every page since #425): measures every page of the
// menu (lib/menu.js), the assistant panel opened, the admin pages, Analytics'
// Delivery topic and, signed out, sign-in (#883) on demo data at 360, 768
// and 1280 px, and the overview's dialogs at 360 × 640 / 640 × 360, writes
// docs/responsive-measured.json and screenshots under test-results/phone/
// (both git-ignored) and, report-only, e2e/a11y-report.json (small text and
// tap targets at 360 px, #763), and fails only when something is worse than
// e2e/phone-baseline.json, naming page, viewport and element.
// UPDATE_PHONE_BASELINE=1 (re)records the baseline, locally only: CI refuses
// to (recordRefusal, #739) and prints what the baseline could drop.
import fs from "node:fs";
import path from "node:path";
import { demoLoaded, expect, test } from "./fixtures.js";
import { measureDialog, measurePage, measureResize } from "./measure.js";
import { A11Y_WIDTH, DIALOG_VIEWPORTS, RESIZE_PASSES, WIDTHS, a11yOfPage, a11yReport, baselineNotes, baselineOf, baselineSlack, compareToBaseline, pagePathname, recordRefusal, summariseDialog, summariseResize, summariseWidth, viewportPages } from "./phone.js";

const FRONTEND = process.env.PHONE_FRONTEND_DIR;
const REPORT = path.join(FRONTEND, "docs", "responsive-measured.json");
// Small text and small tap targets per page at 360 px (#763): report-only, fails nothing.
const A11Y_REPORT = path.join(FRONTEND, "e2e", "a11y-report.json");
const BASELINE = path.join(FRONTEND, "e2e", "phone-baseline.json");
const SHOTS = path.join(FRONTEND, "test-results", "phone");

// What the Rebalance dialog's preview shows: a canned dry run, so the
// dialog has its usual length (the demo serves no rebalance).
const DRY_RUN = {
  dry_run: true,
  done: true,
  limit_im_pct: 20,
  im_pct: 12.4,
  share_per_slot: 0.0124,
  margin_balance: 1.27,
  unit: "BTC",
  actions: [
    { strangle_id: "st-1", slot: "25d/0.16Δ", action: "top_up", call: "BTC-DEMO-110000-C", put: "BTC-DEMO-90000-P", qty: 0.1, target_qty: 0.4, detail: "slot short of its share by 0.1 (demo)" },
    { strangle_id: "st-2", slot: "45d/0.16Δ", action: "top_up", call: "BTC-DEMO-114000-C", put: "BTC-DEMO-87000-P", qty: 0.1, target_qty: 0.4, detail: "slot short of its share by 0.1 (demo)" },
    { strangle_id: "st-3", slot: "60d/0.18Δ", action: "kept", qty: 0, target_qty: 0.3, detail: "within its share (demo)" },
  ],
};

// The demo never enables Close (no regime-side block) nor Rebalance (its
// margin policy is frozen). To measure their dialogs, the browser's copy of
// the demo's answers is changed to enable them, and the dialogs' own POSTs are
// answered here: nothing reaches a bot. Every dialog is left with Escape.
async function enableOrderDialogs(page) {
  await page.route("**/api/bots/btc/positions", async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    const leg = body.strangles?.[0]?.legs?.[0];
    if (leg) leg.manual_close = { allowed: true, reason: "phone check: enabled to measure the dialog", after: "The slot keeps its other leg (demo).", rebuilt: false };
    await route.fulfill({ response: res, json: body });
  });
  await page.route("**/api/bots/btc/status", async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    if (body.risk?.status) Object.assign(body.risk.status, { frozen: false, freeze_reason: "" });
    await route.fulfill({ response: res, json: body });
  });
  await page.route("**/api/bots/*/rebalance", (route) => route.fulfill({ json: DRY_RUN }));
  await page.route("**/api/bots/*/close", (route) => route.fulfill({ status: 403, json: { error: "phone check: nothing is sent" } }));
}

const enabled = (page, name) => page.getByRole("button", { name, exact: true }).and(page.locator(":enabled")).first();

// A menu page's path by its key (lib/menu.js), never typed here.
const pathOf = (key) => viewportPages().find((p) => p.name === key && !p.assistant).path;

// Each dialog with the menu page it opens from: Close… on Details (pro) (#405), Rebalance on Account.
const DIALOGS = {
  close: {
    page: "details",
    ready: "Close…",
    open: async (page) => {
      await enabled(page, "Close…").click();
    },
  },
  rebalance: {
    page: "account",
    ready: "Rebalance",
    open: async (page) => {
      await enabled(page, "Rebalance").click();
      await expect(page.getByRole("dialog").getByText(/Would send|Nothing to send/)).toBeVisible();
    },
  },
  change_password: {
    page: "details",
    ready: "Close…",
    open: async (page) => {
      // The account menu is the signed-in name ▾ (UserMenu).
      await page.getByRole("button", { name: `${process.env.PHONE_ADMIN_USERNAME} ▾`, exact: true }).click();
      await page.getByRole("menuitem", { name: "Change password" }).click();
    },
  },
};

// A page is rendered: the URL stayed on it (not sent to sign-in, nor an admin
// page sent away) and its <main> is shown. A query (?topic=delivery) is kept.
async function openPage(page, target) {
  await page.goto(target.path);
  await expect(page, `${target.name}: did not stay on ${target.path}`).toHaveURL((url) => url.pathname === pagePathname(target));
  await expect(page.locator("main").first(), `${target.name}: no <main> rendered`).toBeVisible({ timeout: 30_000 });
  await page.waitForTimeout(2000); // the first poll of both demo bots arrives
  if (target.assistant) {
    await page.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(page.getByLabel("Assistant", { exact: true }).first()).toBeVisible();
  }
}

// One page at every width: its part of the report, at 360 px its accessibility
// findings, then the text resize and reflow passes (#887, report-only).
async function measureTarget(page, target, report, a11yPages, resizePages) {
  await page.setViewportSize({ width: WIDTHS[0].width, height: WIDTHS[0].height });
  await openPage(page, target);
  report.pages[target.name] = { path: target.path, signed_out: target.signedOut, widths: {} };
  for (const vp of WIDTHS) {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    await page.waitForTimeout(800);
    const raw = await page.evaluate(measurePage);
    report.pages[target.name].widths[vp.name] = summariseWidth(vp, raw);
    if (vp.name === A11Y_WIDTH) a11yPages[target.name] = a11yOfPage(raw);
    await page.screenshot({ path: path.join(SHOTS, `${target.name}-${vp.name}.png`), fullPage: true });
  }
  // Text resize and reflow (#887), report-only: 200 % text size, then 320 px.
  resizePages[target.name] = {};
  for (const pass of RESIZE_PASSES) {
    await page.setViewportSize({ width: pass.width, height: pass.height });
    await page.evaluate((scale) => { document.documentElement.style.fontSize = `${scale * 100}%`; }, pass.fontScale);
    await page.waitForTimeout(500);
    resizePages[target.name][pass.name] = summariseResize(await page.evaluate(measureResize));
    await page.evaluate(() => { document.documentElement.style.fontSize = ""; });
  }
}

// The measure itself (#887): a fixed-height box whose text outgrows it at 200 % is one clipped text.
test("resize measure: a clipped label is counted once", async ({ page }) => {
  await page.setContent(`<html style="font-size:200%"><body style="margin:0">
    <div id="box" style="height:20px;overflow:hidden;width:200px">A long label that wraps onto several lines at double size</div>
    <p>Fits.</p></body></html>`);
  const raw = await page.evaluate(measureResize);
  expect(summariseResize(raw).clipped_text).toBe(1);
  expect(summariseResize(raw).offscreen_controls).toBe(0);
});

test("every page a person reaches and the assistant: measured on demo data, no worse than the baseline", async ({ page, browser }) => {
  fs.mkdirSync(SHOTS, { recursive: true });
  await enableOrderDialogs(page);
  await demoLoaded(page);

  const report = { generated_at: new Date().toISOString(), source: "cmd/monitor-demo (BTC, ETH), simulated data", pages: {}, dialogs: {} };
  const a11yPages = {};
  const resizePages = {};
  for (const target of viewportPages().filter((t) => !t.signedOut)) {
    await measureTarget(page, target, report, a11yPages, resizePages);
  }
  // Sign-in is what a signed-out person sees (#883): a fresh context, no session cookie.
  const { baseURL, colorScheme } = test.info().project.use;
  const signedOut = await browser.newContext({ baseURL, colorScheme });
  try {
    const fresh = await signedOut.newPage();
    for (const target of viewportPages().filter((t) => t.signedOut)) {
      await measureTarget(fresh, target, report, a11yPages, resizePages);
    }
  } finally {
    await signedOut.close();
  }

  for (const [name, { page: pageKey, ready, open }] of Object.entries(DIALOGS)) {
    await page.setViewportSize({ width: WIDTHS[0].width, height: WIDTHS[0].height });
    await page.goto(pathOf(pageKey));
    await expect(page.getByRole("button", { name: ready, exact: true }).first()).toBeAttached({ timeout: 30_000 });
    report.dialogs[name] = {};
    for (const vp of DIALOG_VIEWPORTS) {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await page.waitForTimeout(300);
      await open(page);
      await expect(page.getByRole("dialog")).toBeVisible();
      report.dialogs[name][vp.name] = summariseDialog(vp, await page.evaluate(measureDialog));
      await page.screenshot({ path: path.join(SHOTS, `dialog-${name}-${vp.name}.png`) });
      await page.keyboard.press("Escape");
      await expect(page.getByRole("dialog")).toHaveCount(0);
    }
  }

  fs.writeFileSync(REPORT, `${JSON.stringify(report, null, 2)}\n`);
  fs.writeFileSync(A11Y_REPORT, `${JSON.stringify(a11yReport(a11yPages, report.generated_at, resizePages), null, 2)}\n`);

  // Every overflow is explained: the report names what causes it.
  for (const [pageName, p] of Object.entries(report.pages)) {
    for (const [name, w] of Object.entries(p.widths)) {
      if (w.overflow.overflow_px > 0) expect(w.overflow.causes, `${pageName} ${name} px overflows by ${w.overflow.overflow_px} px with no cause named`).not.toHaveLength(0);
    }
  }

  const refusal = recordRefusal(process.env, fs.existsSync(BASELINE));
  expect(refusal, refusal).toBe("");
  if (process.env.UPDATE_PHONE_BASELINE === "1" || !fs.existsSync(BASELINE)) {
    const baseline = baselineOf(report);
    fs.writeFileSync(BASELINE, `${JSON.stringify(baseline, null, 2)}\n`);
    test.info().annotations.push({ type: "baseline", description: `recorded ${path.relative(FRONTEND, BASELINE)}: review and commit it` });
    // The known problems, one per line: the follow-up notes of the PR that commits the baseline.
    console.log(`Baseline entries (follow-ups):\n${baselineNotes(baseline).join("\n")}`);
    return;
  }
  const baseline = JSON.parse(fs.readFileSync(BASELINE, "utf8"));
  const slack = baselineSlack(baseline, report);
  if (slack.length) console.log(`Better than e2e/phone-baseline.json (${slack.length}): the baseline can be tightened with UPDATE_PHONE_BASELINE=1, locally:\n${slack.join("\n")}`);
  const problems = compareToBaseline(baseline, report);
  expect(problems, `worse than e2e/phone-baseline.json:\n${problems.join("\n")}`).toEqual([]);
});
