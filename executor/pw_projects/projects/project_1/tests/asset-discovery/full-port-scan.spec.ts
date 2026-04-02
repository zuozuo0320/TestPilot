import { test, expect } from '../../fixtures/auth.fixture';

test.describe('资产探知', () => {
  test('全端口扫描', async ({ navigationPage, assetDiscoveryTaskPage, toastPage }) => {
    await test.step('进入资产探知查看任务工作台', async () => {
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await navigationPage.expectMenuActive('资产探知');
      await assetDiscoveryTaskPage.openViewTaskWorkbench();
      await assetDiscoveryTaskPage.expectWorkbenchReady();
    });

    await test.step('创建全端口扫描任务并提交', async () => {
      await assetDiscoveryTaskPage.createFullPortScanTask({
        target: '10.10.10.200',
        templateName: '-65535'
      });
    });

    await test.step('校验任务提交结果', async () => {
      await toastPage.expectSuccess();
    });
  });
});