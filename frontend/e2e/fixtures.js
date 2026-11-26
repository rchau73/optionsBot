// Playwright fixture of the phone-width check: a page signed in as the
// throwaway admin playwright.config.mjs creates (in-memory user database), on
// the monitor once both demo bots' data has arrived.
import { test as base, expect } from "@playwright/test";
import { viewportPages } from "./phone.js";

export const test = base.extend({
  // `provide`, not Playwright's usual `use`: ESLint's React rules read `use` as a hook.
  page: async ({ page }, provide) => {
    await page.goto("/login");
    await page.getByLabel("Username").fill(process.env.PHONE_ADMIN_USERNAME);
    await page.getByLabel("Password", { exact: true }).fill(process.env.PHONE_ADMIN_PASSWORD);
    await page.getByRole("button", { name: "Sign in" }).click();
    await page.waitForURL((url) => url.pathname === "/");
    await provide(page);
  },
});

/**
 * Opens Details (pro) and waits until the demo's positions and its working
 * order are on it (the Overview has shown neither since #405).
 */
export async function demoLoaded(page) {
  await page.goto(viewportPages().find((p) => p.name === "details").path);
  await expect(page.getByRole("button", { name: "Close…", exact: true }).first()).toBeAttached({ timeout: 30_000 });
  await expect(page.getByText("(repair)").first()).toBeVisible({ timeout: 30_000 });
  await page.waitForTimeout(1500); // charts and the first events settle
}

export { expect };
