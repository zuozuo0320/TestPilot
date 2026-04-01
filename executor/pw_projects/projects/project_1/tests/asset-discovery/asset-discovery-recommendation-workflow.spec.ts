import { test, expect } from '../../fixtures/auth.fixture';

test.describe('资产探知', () => {
  test('执行云端资产推荐及后续任务操作', async ({ page, navigationPage, assetDiscoveryTaskPage, dialogPage }) => {
    await test.step('进入任务管理下的资产探知页面', async () => {
      await page.goto(process.env.BASE_URL || '');
      await navigationPage.goToMenuPath(['任务管理', '资产探知']);
      await navigationPage.expectMenuActive('资产探知');
      await assetDiscoveryTaskPage.waitForCloudRecommendationTaskEntryReady();
    });

    await test.step('打开云端资产推荐任务工作台', async () => {
      await assetDiscoveryTaskPage.openCloudRecommendationTaskWorkbench();
      await expect(assetDiscoveryTaskPage.cloudRecommendationCheckboxFirst).toBeVisible();
    });

    await test.step('执行云端资产推荐并确认', async () => {
      await assetDiscoveryTaskPage.runCloudAssetRecommendation();
      await dialogPage.expectVisible();
      await dialogPage.confirm();
    });

    await test.step('继续执行资产信任度评估与资产入账扫描', async () => {
      await assetDiscoveryTaskPage.runAssetCredibilityAssessment();
      await assetDiscoveryTaskPage.runAssetAccountingScan();
    });
  });
});
