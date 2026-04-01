import { test, expect } from '../../fixtures/auth.fixture';

test.describe('资产探知任务管理', () => {
  test('从任务工作台新建任务', async ({ page, navigationPage, assetDiscoveryTaskPage }) => {
    await test.step('进入工作台并打开指定任务', async () => {
      await page.goto(process.env.BASE_URL || '');
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await assetDiscoveryTaskPage.openTaskListByIndex(2);
      await expect(assetDiscoveryTaskPage.createTaskButton).toBeVisible();
    });

    await test.step('新建资产探知任务', async () => {
      await assetDiscoveryTaskPage.createTask({ target: '10.10.10.200' });
    });
  });
});