import { test, expect } from '../../fixtures/auth.fixture';

test.describe('任务管理', () => {
  test('任务下发123', async ({ navigationPage, assetDiscoveryTaskPage, dialogPage }) => {
    await test.step('进入资产探知任务页面', async () => {
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await assetDiscoveryTaskPage.openTaskListByIndex(2);
      await expect(assetDiscoveryTaskPage.createTaskButton).toBeVisible();
    });

    await test.step('新建资产探知任务', async () => {
      await assetDiscoveryTaskPage.createTask({ target: '10.10.10.200' });
      await dialogPage.expectHidden();
    });
  });
});