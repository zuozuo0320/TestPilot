/**
 * 登录页面对象，封装登录页的通用交互操作。
 *
 * 职责：
 * 1. 提供账号密码登录、验证码输入与登录结果断言能力。
 * 2. 作为平台内置 shared 页面，供项目级 fixture 统一注入。
 * 3. 仅承载登录语义，不负责登录后的业务流程判断。
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class LoginPage {
  readonly page: Page;

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
   * 断言登录成功。
   * @param expectedUrlPattern 登录后期望的 URL 模式
   */
  async expectLoginSuccess(expectedUrlPattern?: string | RegExp): Promise<void> {
    if (expectedUrlPattern) {
      await this.page.waitForURL(expectedUrlPattern);
    } else {
      // 未显式传入 URL 模式时，使用“登录按钮消失”作为离开登录页的兜底判断。
      await expect(this.loginButton).toBeHidden({ timeout: 10000 });
    }
  }

  /**
   * 断言登录失败。
   * @param message 可选的错误提示文本
   */
  async expectLoginFailed(message?: string): Promise<void> {
    if (message) {
      await expect(this.errorMessage.filter({ hasText: message })).toBeVisible();
    } else {
      await expect(this.errorMessage).toBeVisible();
    }
  }

  /**
   * 打开登录页面。
   */
  async openLoginPage(): Promise<void> {
    await this.page.goto(process.env.BASE_URL || '/login');
    await expect(this.usernameInput).toBeVisible();
  }
}
