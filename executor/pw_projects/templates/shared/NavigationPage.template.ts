/**
 * 导航页面对象，负责封装侧边栏与公共菜单导航能力。
 *
 * 职责：
 * 1. 提供按文本导航到菜单、按多级路径导航的通用方法。
 * 2. 提供菜单高亮与页面文本存在性断言，避免业务页面重复写导航判断。
 * 3. 不承载任何具体业务流程，只保留共享导航语义。
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class NavigationPage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  /**
   * 确保页面已加载到目标站点，避免在 about:blank 上查找菜单元素。
   * 如果当前页面是 about:blank，则自动导航到 baseURL（由 playwright.config.ts 配置）。
   */
  private async ensurePageLoaded(): Promise<void> {
    const url = this.page.url();
    if (url === 'about:blank' || url === '') {
      await this.page.goto('/');
      await this.page.waitForLoadState('networkidle');
    }
  }

  /**
   * 获取当前页面中第一个可见的精确文本节点，避免命中折叠菜单里的隐藏副本。
   * @param text 需要精确匹配的文本
   */
  async getVisibleExactText(text: string): Promise<Locator> {
    const exactCandidates = this.page.getByText(text, { exact: true });
    const exactCount = await exactCandidates.count();

    for (let index = 0; index < exactCount; index += 1) {
      const candidate = exactCandidates.nth(index);
      if (await candidate.isVisible()) {
        return candidate;
      }
    }

    const menuCandidates = this.page
      .locator('.el-menu-item, .el-submenu__title, [role="menuitem"], [class*="menu"]')
      .filter({ hasText: text });
    const menuCount = await menuCandidates.count();

    for (let index = 0; index < menuCount; index += 1) {
      const candidate = menuCandidates.nth(index);
      if (await candidate.isVisible()) {
        return candidate;
      }
    }

    return exactCandidates.first();
  }

  /**
   * 通过菜单名称导航到指定页面。
   * @param menuName 菜单项文本
   */
  async goToMenu(menuName: string): Promise<void> {
    await this.ensurePageLoaded();
    const menuItem = await this.getVisibleExactText(menuName);
    await expect(menuItem).toBeVisible();
    await menuItem.click();
    await this.page.waitForLoadState('networkidle');
  }

  /**
   * 通过多级菜单路径逐级导航。
   * @param menuPath 菜单路径数组，例如 ['任务管理', '资产探知']
   */
  async goToMenuPath(menuPath: string[]): Promise<void> {
    await this.ensurePageLoaded();
    for (const menuName of menuPath) {
      const menuItem = await this.getVisibleExactText(menuName);
      await expect(menuItem).toBeVisible();
      await menuItem.click();
      await this.page.waitForLoadState('networkidle');
    }
  }

  /**
   * 断言侧边栏菜单中指定文本处于激活状态。
   * @param menuText 期望激活的菜单文本
   */
  async expectMenuActive(menuText: string): Promise<void> {
    const activeItem = this.page
      .locator('.el-menu-item.is-active, .el-submenu.is-active')
      .filter({ hasText: menuText });
    await expect(activeItem.first()).toBeVisible({ timeout: 5000 });
  }

  /**
   * 断言页面包含指定文本，用于验证导航成功。
   * @param text 期望页面中包含的文本
   */
  async expectPageContainsText(text: string): Promise<void> {
    const textLocator = await this.getVisibleExactText(text);
    await expect(textLocator).toBeVisible({ timeout: 5000 });
  }
}
