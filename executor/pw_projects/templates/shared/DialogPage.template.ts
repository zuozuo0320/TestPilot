/**
 * 弹窗页面对象，封装通用 Modal / Dialog / Drawer 的交互能力。
 *
 * 职责：
 * 1. 仅处理平台级共性弹窗行为，不承载具体业务表单流程。
 * 2. 提供确认、取消、关闭、可见性与标题断言等标准方法。
 * 3. 优先关注当前可见的弹窗实例，减少命中预渲染容器导致的误判。
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class DialogPage {
  readonly page: Page;

  /** 弹窗遮罩层 */
  readonly overlay: Locator;
  /** 当前可见的弹窗容器 */
  readonly dialog: Locator;
  /** 弹窗标题 */
  readonly dialogTitle: Locator;
  /** 确认按钮 */
  readonly confirmButton: Locator;
  /** 取消按钮 */
  readonly cancelButton: Locator;
  /** 右上角关闭按钮 */
  readonly closeButton: Locator;

  constructor(page: Page) {
    this.page = page;
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
   * 断言弹窗已关闭。
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
