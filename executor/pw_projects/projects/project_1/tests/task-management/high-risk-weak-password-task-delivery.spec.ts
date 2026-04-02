import { test, expect } from '../../fixtures/auth.fixture';

test.describe('任务管理-资产探知', () => {
  test('两高一弱任务下发', async ({ navigationPage, assetDiscoveryTaskPage }) => {
    await test.step('进入资产探知任务工作台', async () => {
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await assetDiscoveryTaskPage.openViewTaskWorkbench();
      await assetDiscoveryTaskPage.expectWorkbenchReady();
    });

    await test.step('创建两高一弱端口任务', async () => {
      await assetDiscoveryTaskPage.createHighRiskWeakPasswordTask({
        target: '10.10.10.200',
        templateName: '两高一弱-端口'
      });
    });
  });
});