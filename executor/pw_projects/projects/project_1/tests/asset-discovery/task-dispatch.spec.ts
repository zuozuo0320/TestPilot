import { test, expect } from '../../fixtures/auth.fixture';

test.describe('资产探知任务管理', () => {
  test('任务下发111', async ({ page, navigationPage, assetDiscoveryTaskPage, dialogPage }) => {
    await test.step('进入资产探知任务页面', async () => {
      await page.goto(process.env.BASE_URL || '');
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await assetDiscoveryTaskPage.openTaskCreateDialog();
    });

    await test.step('填写目标并提交任务', async () => {
      await assetDiscoveryTaskPage.createTask({
        target: '10.10.10.200'
      });
      await dialogPage.expectHidden();
    });
  });
});