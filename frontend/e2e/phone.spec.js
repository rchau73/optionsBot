// The phone-width check (#309): measures the monitor on demo data at 360,
// 390 and 768 px and its dialogs at 360 × 640 / 640 × 360, writes
// docs/responsive-measured.json and screenshots under test-results/phone/
// (both git-ignored), and fails only when something is worse than
// e2e/phone-baseline.json. UPDATE_PHONE_BASELINE=1 (re)records the baseline.
import fs from "node:fs";
import path from "node:path";
import { demoLoaded, expect, test } from "./fixtures.js";
import { measureDialog, measurePage } from "./measure.js";
import { DIALOG_VIEWPORTS, WIDTHS, baselineOf, compareToBaseline, summariseDialog, summariseWidth } from "./phone.js";

const FRONTEND = process.env.PHONE_FRONTEND_DIR;
const REPORT = path.join(FRONTEND, "docs", "responsive-measured.json");
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

const DIALOGS = {
  close: async (page) => {
    await enabled(page, "Close").click();
  },
  rebalance: async (page) => {
    await enabled(page, "Rebalance").click();
    await expect(page.getByRole("dialog").getByText(/Would send|Nothing to send/)).toBeVisible();
  },
  change_password: async (page) => {
    await page.getByRole("button", { name: /^Account/ }).click();
    await page.getByRole("menuitem", { name: "Change password" }).click();
  },
};

test("phone widths: measured on demo data, no worse than the baseline", async ({ page }) => {
  fs.mkdirSync(SHOTS, { recursive: true });
  await enableOrderDialogs(page);
  await page.reload();
  await demoLoaded(page);

  const report = { generated_at: new Date().toISOString(), source: "cmd/monitor-demo (BTC, ETH), simulated data", widths: {}, dialogs: {} };
  for (const vp of WIDTHS) {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    await page.waitForTimeout(800);
    report.widths[vp.name] = summariseWidth(vp, await page.evaluate(measurePage));
    await page.screenshot({ path: path.join(SHOTS, `monitor-${vp.name}.png`), fullPage: true });
  }

  for (const [name, open] of Object.entries(DIALOGS)) {
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

  // Every overflow is explained: the report names what causes it.
  for (const [name, w] of Object.entries(report.widths)) {
    if (w.overflow.overflow_px > 0) expect(w.overflow.causes, `${name} px overflows by ${w.overflow.overflow_px} px with no cause named`).not.toHaveLength(0);
  }

  if (process.env.UPDATE_PHONE_BASELINE === "1" || !fs.existsSync(BASELINE)) {
    fs.writeFileSync(BASELINE, `${JSON.stringify(baselineOf(report), null, 2)}\n`);
    test.info().annotations.push({ type: "baseline", description: `recorded ${path.relative(FRONTEND, BASELINE)}: review and commit it` });
    return;
  }
  const problems = compareToBaseline(JSON.parse(fs.readFileSync(BASELINE, "utf8")), report);
  expect(problems, `worse than e2e/phone-baseline.json:\n${problems.join("\n")}`).toEqual([]);
});
