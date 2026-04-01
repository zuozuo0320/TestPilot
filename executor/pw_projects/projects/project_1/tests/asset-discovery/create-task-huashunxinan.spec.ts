import { test, expect } from '../../fixtures/auth.fixture';

test.describe('资产探知任务管理', () => {
  test('创建 huashunxinan.net 任务', async ({ page, navigationPage, assetDiscoveryTaskPage }) => {
    await test.step('进入任务管理下的资产探知页面并打开任务列表', async () => {
      await page.goto(process.env.BASE_URL || '');
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await assetDiscoveryTaskPage.openTaskListByIndex(3);
      await expect(assetDiscoveryTaskPage.createTaskButton).toBeVisible();
    });

    await test.step('新建 huashunxinan.net 任务并提交', async () => {
      await assetDiscoveryTaskPage.createTask({
        target: 'huashunxinan.net',
        taskName: 'huashunxinan.net'
      });
    });
  });
});