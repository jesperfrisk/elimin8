import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  timeout: 60000,
  workers: 1,
  use: {
    baseURL: process.env.E2E_BASE_URL || "http://localhost:3000",
    channel:
      process.env.PLAYWRIGHT_CHANNEL ||
      (process.platform === "win32" ? "msedge" : undefined),
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
});
