// The phone-width check (#309): `npm run test:phone`. Starts two
// cmd/monitor-demo bots (BTC, ETH: simulated data, no exchange) and a
// production build of the monitor wired to them only, signs in with a
// throwaway admin on an in-memory user database, and measures the page at
// 360, 390 and 768 px (e2e/phone.spec.js). Chromium only; not part of
// `npm test` (Jest) nor of CI — it needs `npx playwright install chromium` once.
import crypto from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

const frontend = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repo = path.resolve(frontend, "..");

// Ports apart from the environments' (monitor 3000/3100, bots 8081/8082).
const NEXT_PORT = 3290;
const DEMO_PORTS = { btc: 18191, eth: 18192 };

// Fresh per run, never a real credential: the config is evaluated again in
// the test worker, which inherits these from the runner.
process.env.PHONE_ADMIN_USERNAME ??= "phonecheck";
process.env.PHONE_ADMIN_PASSWORD ??= crypto.randomBytes(18).toString("base64url");
process.env.PHONE_AUTH_SECRET ??= crypto.randomBytes(48).toString("base64url");
process.env.PHONE_FRONTEND_DIR ??= frontend;

const demo = (name) => ({
  command: `go run ./cmd/monitor-demo -underlying ${name.toUpperCase()} -addr 127.0.0.1:${DEMO_PORTS[name]}`,
  cwd: repo,
  url: `http://127.0.0.1:${DEMO_PORTS[name]}/api/health`,
  timeout: 180_000,
  reuseExistingServer: false,
});

export default defineConfig({
  testDir: ".",
  testMatch: "*.spec.js",
  outputDir: path.join(frontend, "test-results", "playwright"),
  workers: 1,
  retries: 0,
  timeout: 300_000,
  reporter: [["list"]],
  use: { baseURL: `http://127.0.0.1:${NEXT_PORT}`, browserName: "chromium", colorScheme: "dark" },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: [
    demo("btc"),
    demo("eth"),
    {
      command: `npx next build && npx next start -H 127.0.0.1 -p ${NEXT_PORT}`,
      cwd: frontend,
      url: `http://127.0.0.1:${NEXT_PORT}/login`,
      timeout: 600_000,
      reuseExistingServer: false,
      // Set explicitly so a frontend/.env.local can point nothing at a real
      // bot, user database, audit trail or analytics file.
      env: {
        BOT_APIS: `btc=http://127.0.0.1:${DEMO_PORTS.btc},eth=http://127.0.0.1:${DEMO_PORTS.eth}`,
        AUTH_SECRET: process.env.PHONE_AUTH_SECRET,
        AUTH_DB_PATH: ":memory:",
        AUDIT_PATH: "",
        ADMIN_USERNAME: process.env.PHONE_ADMIN_USERNAME,
        ADMIN_PASSWORD: process.env.PHONE_ADMIN_PASSWORD,
        ADMIN_EMAIL: "",
        ANALYTICS_SALT: "",
        EVENTS_PATH: "",
        MONITOR_API_TOKEN: "",
        MONITOR_DEPLOYMENT: "phone-check",
      },
    },
  ],
});
