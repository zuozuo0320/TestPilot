/**
 * 提示消息页面对象 — 公共模板
 *
 * 职责：封装全局 Toast / Message / Notification 提示框的断言操作。
 *       仅包含通用语义方法，禁止添加具体业务判断逻辑。
 *
 * Shared 规则：
 *   - V1 固定白名单成员之一
 *   - 模板复制到项目后，AI 可追加 locator / action（append_only）
 *   - 仅关注提示框层面，不涉及表单校验提示
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class ToastPage {
  readonly page: Page;

  // ── 通用提示定位器 ──
  /** 成功提示消息（兼容 Element Plus / Ant Design） */
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
     * ���Գ��ֳɹ���ʾ��
     * ������ text ʱУ��ָ���ı���δ����ʱ��У��ɹ�������ʾ���֡�
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
