import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  timeout: 60000,
  retries: 0,
  reporter: [['json', { outputFile: 'test-results.json' }]],
  use: {
    headless: true,
    screenshot: 'on',
    locale: 'zh-CN',
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
    storageState: 'D:/ai_project/TestPilot/executor/auth_states/10.10.10.189.json',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
        launchOptions: {
          args: ['--disable-blink-features=AutomationControlled'],
        },
      },
    },
  ],
});