/**
 * base.fixture.ts — 自动生成，请勿手动修改
 * 由 fixture_builder.py 根据 page-registry.json 生成
 */
import { test as base, expect } from "@playwright/test";
import { AssetDiscoveryTaskPage } from "../pages/AssetDiscoveryTaskPage";
import { DialogPage } from "../pages/shared/DialogPage";
import { LoginPage } from "../pages/LoginPage";
import { NavigationPage } from "../pages/shared/NavigationPage";
import { ToastPage } from "../pages/shared/ToastPage";

export const test = base.extend<{
  assetDiscoveryTaskPage: AssetDiscoveryTaskPage;
  dialogPage: DialogPage;
  loginPage: LoginPage;
  navigationPage: NavigationPage;
  toastPage: ToastPage;
}>({
  assetDiscoveryTaskPage: async ({ page }, use) => {
    await use(new AssetDiscoveryTaskPage(page));
  },
  dialogPage: async ({ page }, use) => {
    await use(new DialogPage(page));
  },
  loginPage: async ({ page }, use) => {
    await use(new LoginPage(page));
  },
  navigationPage: async ({ page }, use) => {
    await use(new NavigationPage(page));
  },
  toastPage: async ({ page }, use) => {
    await use(new ToastPage(page));
  },
});

export { expect };
