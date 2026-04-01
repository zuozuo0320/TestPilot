import { test, expect } from '../../fixtures/auth.fixture';

test.describe('资产探知', () => {
  test('新建下发全端口扫描任务', async ({ page, navigationPage, assetDiscoveryTaskPage }) => {
    await test.step('进入资产探知任务工作台', async () => {
      await page.goto(process.env.BASE_URL || '');
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await navigationPage.expectMenuActive('资产探知');
      await assetDiscoveryTaskPage.openThirdViewTaskEntry();
      await assetDiscoveryTaskPage.expectTaskWorkbenchReady();
    });

    await test.step('创建下发全端口扫描任务', async () => {
      await assetDiscoveryTaskPage.createFullPortScanTask({
        taskName: '下发全端口扫描',
        target: '10.10.10.200'
      });
    });
  });
});