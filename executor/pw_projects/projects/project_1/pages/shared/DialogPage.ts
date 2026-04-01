/**
 * 弹窗页面对象 — 公共模板
 *
 * 职责：封装全局/通用弹窗（Modal / Dialog / Drawer）的交互操作。
 *       仅包含通用语义方法，禁止添加具体业务弹窗内的表单逻辑。
 *
 * Shared 规则：
 *   - V1 固定白名单成员之一
 *   - 模板复制到项目后，AI 可追加 locator / action（append_only）
 *   - 业务弹窗的内部表单应放在对应的 BusinessPage 中
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class DialogPage {
  readonly page: Page;

  // ── 通用弹窗定位器 ──
  /** 弹窗遮罩层 */
  readonly overlay: Locator;
  /** 弹窗容器（兼容 Element Plus / Ant Design） */
  readonly dialog: Locator;
  /** 弹窗标题 */
  readonly dialogTitle: Locator;
  /** 确认按钮 */
  readonly confirmButton: Locator;
  /** 取消按钮 */
  readonly cancelButton: Locator;
  /** 关闭按钮（右上角 X） */
  readonly closeButton: Locator;

  constructor(page: Page) {
    this.page = page;
    // 仅关注当前可见的弹窗，避免命中页面中预渲染但未展示的对话框容器。
    this.overlay = page.locator('.el-overlay:visible, .ant-modal-mask:visible');
    this.dialog = page.locator('.el-dialog:visible, .el-drawer:visible, .ant-modal:visible');
    this.dialogTitle = this.dialog.locator('.el-dialog__title, .el-drawer__title, .ant-modal-title');
    this.confirmButton = this.dialog.getByRole('button', { name: /确定|确认|提交|保存/i });
    this.cancelButton = this.dialog.getByRole('button', { name: /取消|关闭/i });
    this.closeButton = this.dialog.locator('.el-dialog__close, .el-drawer__close-btn, .ant-modal-close');
  }

  /**
   * 点击确认按钮关闭弹窗。
   */
  async confirm(): Promise<void> {
    await this.confirmButton.first().click();
  }

  /**
   * 点击取消按钮关闭弹窗。
   */
  async cancel(): Promise<void> {
    await this.cancelButton.first().click();
  }

  /**
   * 点击右上角关闭按钮。
   */
  async close(): Promise<void> {
    await this.closeButton.first().click();
  }

  /**
   * 断言弹窗可见。
   */
  async expectVisible(): Promise<void> {
    await expect(this.dialog.first()).toBeVisible();
  }

  /**
   * 断言弹窗已关闭（不可见）。
   */
  async expectHidden(): Promise<void> {
    await expect(this.dialog.first()).toBeHidden();
  }

  /**
   * 断言弹窗标题文本。
   * @param title 期望的标题文本
   */
  async expectTitle(title: string): Promise<void> {
    await expect(this.dialogTitle.first()).toContainText(title);
  }
}
