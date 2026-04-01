import { test, expect } from '../../fixtures/auth.fixture';

test.describe('资产探知', () => {
  test('单位资产测绘', async ({ page, navigationPage, assetDiscoveryTaskPage }) => {
    await test.step('进入资产探知任务页面', async () => {
      await page.goto(process.env.BASE_URL || '');
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await assetDiscoveryTaskPage.openTaskListByIndex(0);
      await expect(assetDiscoveryTaskPage.createTaskButton).toBeVisible();
      await navigationPage.expectMenuActive('任务管理');
    });

    await test.step('执行单位资产测绘', async () => {
      await assetDiscoveryTaskPage.startEnterpriseMapping({
        enterpriseName: '北京华顺信安',
        enterpriseFullName: '北京华顺信安科技有限公司'
      });
    });
  });
});