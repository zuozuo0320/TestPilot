/**
 * 提示消息页面对象，封装全局 Toast / Message / Notification 断言能力。
 *
 * 职责：
 * 1. 仅处理提示层通用语义，不承载业务表单校验逻辑。
 * 2. 为成功、失败、警告、信息提示提供统一断言方法。
 * 3. 支持“只校验出现”与“校验指定文本”两种成功提示场景。
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class ToastPage {
  readonly page: Page;

  /** 成功提示消息 */
  readonly successToast: Locator;
  /** 错误提示消息 */
  readonly errorToast: Locator;
  /** 警告提示消息 */
  readonly warningToast: Locator;
  /** 信息提示消息 */
  readonly infoToast: Locator;

  constructor(page: Page) {
    this.page = page;
    this.successToast = page.locator('.el-message--success, .ant-message-success');
    this.errorToast = page.locator('.el-message--error, .ant-message-error');
    this.warningToast = page.locator('.el-message--warning, .ant-message-warning');
    this.infoToast = page.locator('.el-message--info, .ant-message-info');
  }

  /**
   * 断言出现成功提示。
   * @param options 可选文本断言参数
   */
  async expectSuccess(options: { text?: string } = {}): Promise<void> {
    const { text } = options;

    await expect(this.successToast).toBeVisible();

    if (text) {
      await expect(this.successToast).toContainText(text);
    }
  }

  /**
   * 断言出现错误提示，且包含指定消息文本。
   * @param message 期望包含的消息文本
   */
  async expectError(message: string): Promise<void> {
    await expect(this.errorToast.filter({ hasText: message })).toBeVisible({ timeout: 5000 });
  }

  /**
   * 断言出现警告提示，且包含指定消息文本。
   * @param message 期望包含的消息文本
   */
  async expectWarning(message: string): Promise<void> {
    await expect(this.warningToast.filter({ hasText: message })).toBeVisible({ timeout: 5000 });
  }

  /**
   * 断言出现信息提示，且包含指定消息文本。
   * @param message 期望包含的消息文本
   */
  async expectInfo(message: string): Promise<void> {
    await expect(this.infoToast.filter({ hasText: message })).toBeVisible({ timeout: 5000 });
  }

  /**
   * 等待所有提示消息消失。
   */
  async waitForToastDismiss(): Promise<void> {
    await expect(this.successToast).toBeHidden({ timeout: 10000 });
  }
}
