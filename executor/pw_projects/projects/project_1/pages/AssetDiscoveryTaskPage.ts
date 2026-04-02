import { expect, Locator, Page } from '@playwright/test';

/**
 * 资产探知任务页面对象，负责封装资产探知任务工作台进入、就绪校验及任务下发相关业务操作。
 */
export class AssetDiscoveryTaskPage {
  readonly page: Page;
  readonly viewTaskEntry: Locator;
  readonly newTaskButton: Locator;
  readonly targetTextarea: Locator;
  readonly templateSelectTextbox: Locator;
  readonly highRiskWeakPasswordPortOption: Locator;
  readonly confirmButton: Locator;
    /** 全端口扫描模板选项 */
    readonly fullPortTemplateOption: Locator;

  constructor(page: Page) {
    this.page = page;
    this.viewTaskEntry = page.getByText('查看任务').nth(2);
    this.newTaskButton = page.getByRole('button', { name: '新建任务' });
    this.targetTextarea = page.locator('textarea');
    this.templateSelectTextbox = page.getByRole('textbox', { name: '请选择' }).nth(3);
    this.highRiskWeakPasswordPortOption = page.getByText('两高一弱-端口');
    this.confirmButton = page.getByRole('button', { name: '确定' });
      this.fullPortTemplateOption = page.getByText('-65535');
  }

  /**
   * 打开资产探知的查看任务工作台。
   */
  async openViewTaskWorkbench(): Promise<void> {
    await this.viewTaskEntry.click();
  }

  /**
   * 断言任务工作台已就绪。
   */
  async expectWorkbenchReady(): Promise<void> {
    await expect(this.newTaskButton).toBeVisible();
  }

  /**
   * 创建两高一弱任务并提交下发。
   */
  async createHighRiskWeakPasswordTask(options: { target: string; templateName: string }): Promise<void> {
    await this.newTaskButton.click();

    await expect(this.targetTextarea).toBeVisible();
    await this.targetTextarea.click();
    await this.targetTextarea.fill(options.target);

    await this.templateSelectTextbox.click();
    await this.highRiskWeakPasswordPortOption.click();
    await this.confirmButton.click();
  }

      /**
       * 创建并下发全端口扫描任务
       */
      async createFullPortScanTask(options: { target: string; templateName: string }): Promise<void> {
        await this.newTaskButton.click();
        await expect(this.targetTextarea).toBeVisible();
        await this.targetTextarea.click();
        await this.targetTextarea.fill(options.target);
        await this.templateSelectTextbox.click();
        await this.fullPortTemplateOption.click();
        await this.confirmButton.click();
      }
}
