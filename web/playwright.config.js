import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.js',
  timeout: 30_000,
  workers: 1,
  retries: 0,
  reporter: [['line']],
  use: {
    baseURL: process.env.NLI_BASE_URL || 'http://127.0.0.1:12382',
    // The Rocky two-node gate proves the generated private CA separately with
    // curl --cacert before Chromium runs. New Chromium builds use their own
    // root store, so only that gate may opt out of repeating certificate trust.
    ignoreHTTPSErrors: process.env.NLI_E2E_IGNORE_HTTPS_ERRORS === '1',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    ...(process.env.NLI_BROWSER_CHANNEL ? { channel: process.env.NLI_BROWSER_CHANNEL } : {}),
  },
  outputDir: process.env.NLI_E2E_OUTPUT || '../build/browser-test-results',
});
