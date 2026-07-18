import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results/graph",
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: [
    ["line"],
    ["html", { outputFolder: "playwright-report", open: "never" }],
  ],
  use: {
    baseURL: "http://127.0.0.1:4173",
    browserName: "chromium",
    colorScheme: "dark",
    locale: "en-US",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "pnpm exec vite --host 127.0.0.1 --port 4173",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    stdout: "ignore",
    stderr: "pipe",
  },
  projects: [
    { name: "mobile-360x800", use: { viewport: { width: 360, height: 800 }, hasTouch: true, isMobile: true } },
    { name: "mobile-412x915", use: { viewport: { width: 412, height: 915 }, hasTouch: true, isMobile: true } },
    { name: "tablet-768x1024", use: { viewport: { width: 768, height: 1024 }, hasTouch: true } },
    { name: "desktop-1366x768", use: { viewport: { width: 1366, height: 768 } } },
    { name: "desktop-1920x1080", use: { viewport: { width: 1920, height: 1080 } } },
  ],
});
