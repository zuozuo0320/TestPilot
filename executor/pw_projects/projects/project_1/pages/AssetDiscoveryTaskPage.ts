/**
 * 资产探知任务页面对象。
 *
 * 职责：
 * 1. 封装资产探知模块入口、任务工作台与新建任务表单的通用操作。
 * 2. 严格复用录制脚本中的原始定位器，只做 POM 结构化组织，不改写定位语义。
 * 3. 兼容资产探知下不同任务工作台路由与卡片入口，避免把页面就绪条件硬编码到固定 URL。
 */
import { type Locator, type Page, expect } from '@playwright/test';

export class AssetDiscoveryTaskPage {
  readonly page: Page;

  /** 任务入口页中的“查看任务”按钮集合。 */
  readonly viewTaskButtons: Locator;
  /** 任务工作台中的“新建任务”按钮。 */
  readonly createTaskButton: Locator;
  /** 新建任务表单中的目标输入框。 */
  readonly targetTextarea: Locator;
  /** 新建任务表单中的任务名称输入框。 */
  readonly taskNameTextbox: Locator;
  /** 单位资产测绘表单中的企业搜索输入框。 */
  readonly enterpriseSearchTextbox: Locator;
  /** 企业关系查询按钮。 */
  readonly enterpriseRelationQueryButton: Locator;
  /** 开始测绘按钮。 */
  readonly startMappingButton: Locator;
  /** 通用确认按钮。 */
  readonly confirmButton: Locator;
  /** 云端资产推荐卡片中的“查看任务”入口。 */
  readonly cloudRecommendationTaskEntry: Locator;
  /** 云端资产推荐工作台中的首个勾选框。 */
  readonly cloudRecommendationCheckboxFirst: Locator;
  /** 云端资产推荐按钮。 */
  readonly cloudAssetRecommendationButton: Locator;
  /** 资产信任度评估按钮。 */
  readonly assetCredibilityAssessmentButton: Locator;
  /** 资产入账扫描按钮。 */
  readonly assetAccountingScanButton: Locator;
  /** 第三个“查看任务”入口。 */
  readonly thirdViewTaskButton: Locator;
  /** 端口范围选择输入框。 */
  readonly portRangeSelectTextbox: Locator;
  /** 全端口范围选项。 */
  readonly minus65535ListItem: Locator;

  constructor(page: Page) {
    this.page = page;
    this.viewTaskButtons = page.getByText('查看任务');
    this.createTaskButton = page.getByRole('button', { name: '新建任务' });
    this.targetTextarea = page.locator('textarea').first();
    this.taskNameTextbox = page.getByRole('textbox', { name: '请输入任务名称', exact: true });
    this.enterpriseSearchTextbox = page.getByRole('textbox', { name: '请输入企业名称进行搜索' });
    this.enterpriseRelationQueryButton = page.getByRole('button', { name: '企业关系查询' });
    this.startMappingButton = page.getByRole('button', { name: '开始测绘' });
    this.confirmButton = page.getByRole('button', { name: '确定' });
    this.cloudRecommendationTaskEntry = page
      .getByRole('list')
      .filter({
        hasText:
          '云端资产推荐支持自定义线索进行影子资产的发现，帮助用户快速、精准的获取资产数据，适用于不同场景资产盘点。查看任务',
      })
      .getByRole('paragraph');
    this.cloudRecommendationCheckboxFirst = page
      .locator('td > .cell > .el-checkbox > .el-checkbox__input > .el-checkbox__inner')
      .first();
    this.cloudAssetRecommendationButton = page.getByRole('button', { name: '云端资产推荐' });
    this.assetCredibilityAssessmentButton = page.getByRole('button', { name: '资产信任度评估' });
    this.assetAccountingScanButton = page.getByRole('button', { name: '资产入账扫描' });
    this.thirdViewTaskButton = this.viewTaskButtons.nth(2);
    this.portRangeSelectTextbox = this.page.getByRole('textbox', { name: '请选择' }).nth(3);
    this.minus65535ListItem = this.page.getByRole('listitem').filter({ hasText: '-65535' });
  }

  /**
   * 等待资产探知任务入口页加载完成。
   *
   * 这里不依赖固定业务 URL，而是以录制脚本中真实会出现的“查看任务”入口为准，
   * 这样可以兼容同一模块下不同索引跳转到不同任务工作台的情况。
   */
  async waitForEntryReady(): Promise<void> {
    await expect(this.viewTaskButtons.first()).toBeVisible({ timeout: 15000 });
  }

  /**
   * 等待通用任务工作台加载完成。
   *
   * 同一模块下可能跳到 `/assetsScan`、`/unitIndex`、`/domainTask` 等不同地址，
   * 因此统一使用真实可见的业务控件作为 ready 条件，而不是写死 URL。
   */
  async waitForTaskWorkspaceReady(): Promise<void> {
    await expect(this.createTaskButton.first()).toBeVisible({ timeout: 15000 });
  }

  /**
   * 等待云端资产推荐卡片入口可见。
   *
   * 这里必须复用录制脚本里的完整卡片定位器，避免把特定任务入口错误抽象成通用索引点击。
   */
  async waitForCloudRecommendationTaskEntryReady(): Promise<void> {
    await this.waitForEntryReady();
    await expect(this.cloudRecommendationTaskEntry).toBeVisible({ timeout: 15000 });
  }

  /**
   * 判断新建任务表单是否已经打开。
   *
   * 这里优先检查录制脚本中真实操作过的表单控件，
   * 避免因为弹窗容器实现差异导致“弹窗已开但判断失败”。
   */
  async isTaskCreateDialogOpen(): Promise<boolean> {
    if (await this.targetTextarea.isVisible()) {
      return true;
    }

    if (await this.taskNameTextbox.isVisible()) {
      return true;
    }

    if (await this.enterpriseSearchTextbox.isVisible()) {
      return true;
    }

    return false;
  }

  /**
   * 等待新建任务表单出现。
   *
   * 资产探知不同任务类型的表单首屏控件不同：
   * 有的先出现目标 textarea，有的先出现企业搜索输入框。
   * 因此这里用“任一关键表单控件可见”作为统一表单就绪条件。
   */
  async waitForTaskFormReady(): Promise<void> {
    try {
      await Promise.any([
        this.targetTextarea.waitFor({ state: 'visible', timeout: 15000 }),
        this.taskNameTextbox.waitFor({ state: 'visible', timeout: 15000 }),
        this.enterpriseSearchTextbox.waitFor({ state: 'visible', timeout: 15000 }),
      ]);
    } catch {
      throw new Error('新建任务表单未出现：未检测到 textarea、任务名称输入框或企业搜索输入框。');
    }
  }

  /**
   * 打开指定索引的任务工作台。
   * @param index “查看任务”按钮索引
   */
  async openTaskListByIndex(index: number): Promise<void> {
    await this.waitForEntryReady();
    await expect(this.viewTaskButtons.nth(index)).toBeVisible({ timeout: 15000 });
    await this.viewTaskButtons.nth(index).click();
    await this.waitForTaskWorkspaceReady();
  }

  /**
   * 打开云端资产推荐任务工作台。
   *
   * 该入口来自录制脚本中的特定卡片文案，不允许退化成通用的第 N 个“查看任务”按钮。
   * 如果点击后直接进入任务详情页且页面状态已经变化，需要明确提示这是业务数据态漂移，而不是代码结构问题。
   */
  async openCloudRecommendationTaskWorkbench(): Promise<void> {
    await this.waitForCloudRecommendationTaskEntryReady();
    await this.cloudRecommendationTaskEntry.click();

    try {
      await expect(this.cloudRecommendationCheckboxFirst).toBeVisible({ timeout: 5000 });
    } catch (error) {
      const recommendationTaskTitle = this.page.getByText('云端推荐任务');
      const isRecommendationTaskDetailVisible = await recommendationTaskTitle.isVisible().catch(() => false);

      if (isRecommendationTaskDetailVisible) {
        throw new Error(
          '点击云端资产推荐卡片后，当前系统直接进入了“云端推荐任务”详情页，且页面已处于后续流程状态；录制脚本中原本存在的勾选框工作台未出现。请确认任务数据是否已变化，或重新录制该业务入口。'
        );
      }

      throw error;
    }
  }

  /**
   * 打开新建任务表单。
   *
   * 如果当前还停留在任务入口页，则优先复用第一个“查看任务”入口进入默认工作台；
   * 如果已经在工作台内，则直接点击“新建任务”。
   */
  async openTaskCreateDialog(): Promise<void> {
    const createTaskButton = this.createTaskButton.first();
    const isCreateTaskButtonVisible = await createTaskButton.isVisible().catch(() => false);

    if (!isCreateTaskButtonVisible) {
      await this.waitForEntryReady();
      await this.viewTaskButtons.first().click();
      await this.waitForTaskWorkspaceReady();
    }

    await createTaskButton.click();
    await this.waitForTaskFormReady();
  }

  /**
   * 确保新建任务表单处于可填写状态。
   *
   * 某些 spec 会先手动打开表单，某些 spec 会直接调用业务方法，
   * 因此这里做一次兜底，统一保证后续填写动作有稳定表单上下文。
   */
  async ensureTaskCreateDialogOpen(): Promise<void> {
    if (await this.isTaskCreateDialogOpen()) {
      return;
    }

    await this.openTaskCreateDialog();
  }

  /**
   * 填写目标与可选任务名称，并提交新建任务。
   * @param options 新建任务所需的目标与可选任务名称
   */
  async createTask(options: { target: string; taskName?: string }): Promise<void> {
    await this.ensureTaskCreateDialogOpen();
    await expect(this.targetTextarea).toBeVisible({ timeout: 15000 });
    await this.targetTextarea.click();
    await this.targetTextarea.fill(options.target);

    if (options.taskName) {
      await this.taskNameTextbox.fill(options.taskName);
    }

    await this.confirmButton.click();
    await expect(this.targetTextarea).toBeHidden({ timeout: 15000 });
  }

  /**
   * 发起单位资产测绘任务。
   * @param options 企业搜索与选择参数
   */
  async startEnterpriseMapping(options: { enterpriseName: string; enterpriseFullName: string }): Promise<void> {
    await this.ensureTaskCreateDialogOpen();

    // 这里保留录制脚本中的三次点击，再输入企业名称，避免组件需要先聚焦或展开联想框时行为不一致。
    await expect(this.enterpriseSearchTextbox).toBeVisible({ timeout: 15000 });
    await this.enterpriseSearchTextbox.click();
    await this.enterpriseSearchTextbox.click();
    await this.enterpriseSearchTextbox.click();
    await this.enterpriseSearchTextbox.fill(options.enterpriseName);

    const enterpriseOption = this.page.getByText(options.enterpriseFullName);
    await expect(enterpriseOption).toBeVisible({ timeout: 15000 });
    await enterpriseOption.click();

    await this.enterpriseRelationQueryButton.click();
    await this.startMappingButton.click();
  }

  /**
   * 勾选首条记录后执行云端资产推荐。
   */
  async runCloudAssetRecommendation(): Promise<void> {
    await expect(this.cloudRecommendationCheckboxFirst).toBeVisible({ timeout: 15000 });
    await this.cloudRecommendationCheckboxFirst.click();
    await this.cloudAssetRecommendationButton.click();
  }

  /**
   * 执行资产信任度评估。
   */
  async runAssetCredibilityAssessment(): Promise<void> {
    await this.assetCredibilityAssessmentButton.click();
  }

  /**
   * 执行资产入账扫描。
   */
  async runAssetAccountingScan(): Promise<void> {
    await this.assetAccountingScanButton.click();
  }

  /**
   * 打开第三个“查看任务”入口并进入任务工作台。
   *
   * 该方法对应录制脚本中的 `page.getByText('查看任务').nth(2).click()`，
   * 这里复用通用入口集合，避免重复维护同一类定位器。
   */
  async openThirdViewTaskEntry(): Promise<void> {
    await this.waitForEntryReady();
    await expect(this.thirdViewTaskButton).toBeVisible({ timeout: 15000 });
    await this.thirdViewTaskButton.click();
  }

  /**
   * 断言任务工作台已就绪。
   */
  async expectTaskWorkbenchReady(): Promise<void> {
    await expect(this.createTaskButton).toBeVisible({ timeout: 15000 });
  }

  /**
   * 填写任务名称、目标和端口范围后提交全端口扫描任务。
   * @param options 全端口扫描任务所需的任务名称与目标
   */
  async createFullPortScanTask(options: { taskName: string; target: string }): Promise<void> {
    await this.createTaskButton.click();
    await expect(this.targetTextarea).toBeVisible({ timeout: 15000 });
    await this.targetTextarea.click();

    // 这里保留录制脚本中的任务名称清空与重填逻辑，避免默认值残留影响提交。
    await this.taskNameTextbox.click();
    await this.taskNameTextbox.press('ControlOrMeta+a');
    await this.taskNameTextbox.fill(options.taskName);

    await this.targetTextarea.click();
    await this.targetTextarea.fill(options.target);
    await this.portRangeSelectTextbox.click();
    await this.minus65535ListItem.click();
    await this.confirmButton.click();
  }
}
