/**
 * 项目级 Playwright 配置模板
 *
 * 使用方式：项目初始化时复制到 projects/<project_key>/playwright.config.ts
 * 占位符说明：
 *   project_1  — 项目标识（如 foradar）
 *   AiSight Demo — 项目中文名
 */
import { defineConfig, devices } from '@playwright/test';
import dotenv from 'dotenv';
import path from 'path';

// 加载项目级环境变量
dotenv.config({ path: path.resolve(__dirname, '.env') });

export default defineConfig({
  testDir: './tests',
  timeout: 60_000,
  retries: 0,
  fullyParallel: false,
  forbidOnly: true,

  reporter: [
    ['json', { outputFile: 'test-results.json' }],
    ['html', { open: 'never', outputFolder: 'playwright-report' }],
  ],

  use: {
    headless: true,
    screenshot: 'on',
    locale: 'zh-CN',
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
    storageState: 'D:/ai_project/TestPilot/executor/auth_states/10.10.10.189.json',
    baseURL: process.env.BASE_URL || '',
  },

  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: {
          args: ['--disable-blink-features=AutomationControlled'],
        },
      },
    },
  ],
});
