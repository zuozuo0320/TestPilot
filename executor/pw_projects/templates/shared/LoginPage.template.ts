/**
 * 登录页面对象 — 公共模板
 *
 * 职责：封装登录页面的交互操作，包括账号密码登录和验证码处理。
 *       作为 Shared 白名单成员之一，但物理文件放在 pages/LoginPage.ts（非 shared 子目录）。
 *
 * Shared 规则：
 *   - V1 固定白名单成员之一
 *   - 模板复制到项目后，AI 可追加 locator / action（append_only）
 *   - 仅包含登录流程的通用语义，不涉及登录后的业务逻辑
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class LoginPage {
  readonly page: Page;

  // ── 登录表单定位器 ──
  /** 用户名输入框 */
  readonly usernameInput: Locator;
  /** 密码输入框 */
  readonly passwordInput: Locator;
  /** 验证码输入框 */
  readonly captchaInput: Locator;
  /** 登录按钮 */
  readonly loginButton: Locator;
  /** 登录错误提示 */
  readonly errorMessage: Locator;

  constructor(page: Page) {
    this.page = page;
    this.usernameInput = page.getByPlaceholder(/用户名|账号|Username/i);
    this.passwordInput = page.getByPlaceholder(/密码|Password/i);
    this.captchaInput = page.getByPlaceholder(/验证码|Captcha/i);
    this.loginButton = page.getByRole('button', { name: /登录|Login|Sign in/i });
    this.errorMessage = page.locator('.login-error, .el-message--error');
  }

  /**
   * 执行登录操作。
   * @param options 登录凭据
   */
  async login(options: {
    username: string;
    password: string;
    captcha?: string;
  }): Promise<void> {
    await this.usernameInput.fill(options.username);
    await this.passwordInput.fill(options.password);
    if (options.captcha) {
      await this.captchaInput.fill(options.captcha);
    }
    await this.loginButton.click();
    await this.page.waitForLoadState('networkidle');
  }

  /**
   * 断言登录成功（页面跳转离开登录页）。
   * @param expectedUrlPattern 登录后期望的 URL 模式
   */
  async expectLoginSuccess(expectedUrlPattern?: string | RegExp): Promise<void> {
    if (expectedUrlPattern) {
      await this.page.waitForURL(expectedUrlPattern);
    } else {
      // 默认断言：登录按钮不再可见（已离开登录页）
      await expect(this.loginButton).toBeHidden({ timeout: 10000 });
    }
  }

  /**
   * 断言登录失败，显示错误提示。
   * @param message 期望的错误提示文本
   */
  async expectLoginFailed(message?: string): Promise<void> {
    if (message) {
      await expect(this.errorMessage.filter({ hasText: message })).toBeVisible();
    } else {
      await expect(this.errorMessage).toBeVisible();
    }
  }
}
