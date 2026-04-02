import unittest

from raw_locator_guard import (
    build_step_model_from_recording,
    validate_v1_critical_locator_coverage,
    validate_v1_locator_preservation,
    validate_v1_url_semantics_preservation,
)


class RawLocatorGuardTestCase(unittest.TestCase):
    """校验录制脚本解析与原始定位器守卫的通用行为。"""

    def test_build_step_model_supports_wrapped_codegen_script(self) -> None:
        """应当正确拆分被 test(...) 包裹的录制脚本，并保留 getByRole 定位器。"""
        raw_script = """import { test, expect } from '@playwright/test';

test.use({
  ignoreHTTPSErrors: true,
  storageState: './auth_states\\\\10.10.10.189.json'
});

test('test', async ({ page }) => {
  await page.goto('https://10.10.10.189/workbench');
  await page.locator('span').filter({ hasText: '任务管理' }).first().click();
  await page.getByText('资产探知').click();
  await page.getByText('查看任务').first().click();
  await page.getByRole('button', { name: '新建任务' }).click();
  await page.getByRole('textbox', { name: '请输入企业名称进行搜索' }).click();
  await page.getByRole('textbox', { name: '请输入企业名称进行搜索' }).fill('北京华顺信安');
  await page.getByText('北京华顺信安科技有限公司').click();
  await page.getByRole('button', { name: '企业关系查询' }).click();
  await page.getByRole('button', { name: '开始测绘' }).click();
});"""

        result = build_step_model_from_recording(raw_script)

        self.assertEqual(result["total_steps"], 10)
        self.assertEqual(result["steps"][0]["action_type"], "NAVIGATE")
        self.assertEqual(
            result["steps"][4]["locator"],
            "getByRole('button', { name: '新建任务' })",
        )
        self.assertEqual(
            result["steps"][5]["locator"],
            "getByRole('textbox', { name: '请输入企业名称进行搜索' })",
        )
        self.assertEqual(result["steps"][6]["action_type"], "INPUT")
        self.assertEqual(result["steps"][6]["input_value"], "北京华顺信安")

    def test_build_step_model_collapses_duplicate_opener_click_for_generation(self) -> None:
        """生成阶段应折叠重复的按钮型 opener 点击，但原始步骤仍需完整保留。"""
        raw_script = """test('test', async ({ page }) => {
  await page.getByRole('button', { name: '新建任务' }).click();
  await page.getByRole('button', { name: '新建任务' }).click();
  await page.locator('textarea').fill('10.10.10.200');
});"""

        result = build_step_model_from_recording(raw_script)

        self.assertEqual(result["total_steps"], 3)
        self.assertEqual(result["generation_total_steps"], 2)
        self.assertEqual(len(result["normalization_items"]), 1)
        self.assertEqual(
            result["normalization_items"][0]["type"],
            "collapse_duplicate_opener_click",
        )
        self.assertEqual(result["generation_steps"][0]["raw_step_no"], 1)
        self.assertEqual(result["generation_steps"][1]["raw_step_no"], 3)
        self.assertEqual(
            result["generation_script"].count("getByRole('button', { name: '新建任务' }).click()"),
            1,
        )

    def test_build_step_model_keeps_duplicate_textbox_clicks(self) -> None:
        """文本框等非按钮控件的重复点击不应被误判为 opener 去重。"""
        raw_script = """test('test', async ({ page }) => {
  await page.getByRole('textbox', { name: '请输入企业名称进行搜索' }).click();
  await page.getByRole('textbox', { name: '请输入企业名称进行搜索' }).click();
  await page.getByRole('textbox', { name: '请输入企业名称进行搜索' }).fill('北京华顺信安');
});"""

        result = build_step_model_from_recording(raw_script)

        self.assertEqual(result["total_steps"], 3)
        self.assertEqual(result["generation_total_steps"], 3)
        self.assertEqual(result["normalization_items"], [])

    def test_validate_locator_preservation_blocks_rewritten_role_locator(self) -> None:
        """应当识别 LLM 把录制稿中的 getByRole 文本改写为错误值的情况。"""
        raw_script = """test('test', async ({ page }) => {
  await page.getByRole('textbox', { name: '请输入企业名称进行搜索' }).click();
  await page.getByRole('button', { name: '企业关系查询' }).click();
});"""

        llm_result = {
            "page_updates": [
                {
                    "page_name": "AssetDiscoveryTaskPage",
                    "new_locators": [
                        {
                            "name": "enterpriseSearchTextbox",
                            "definition": "page.getByRole('textbox', { name: '请输入关键字检索' })",
                        }
                    ],
                    "new_actions": [],
                    "extend_actions": [],
                }
            ]
        }

        result = validate_v1_locator_preservation(raw_script=raw_script, llm_result=llm_result)

        self.assertEqual(len(result["invalid_locators"]), 1)
        self.assertIn("enterpriseSearchTextbox", result["manual_review_items"][0])

    def test_validate_url_semantics_blocks_invented_fixed_route_assertion(self) -> None:
        """当 raw_script 不包含 URL 等待/断言时，应当拦截 LLM 凭空发明的固定路由判断。"""
        raw_script = """test('test', async ({ page }) => {
  await page.goto('https://10.10.10.189/workbench');
  await page.getByText('查看任务').nth(3).click();
  await page.getByRole('button', { name: '新建任务' }).click();
});"""

        llm_result = {
            "page_creates": [
                {
                    "path": "pages/AssetDiscoveryTaskPage.ts",
                    "class_name": "AssetDiscoveryTaskPage",
                    "content": """export class AssetDiscoveryTaskPage {
  async waitForTaskWorkspaceReady(): Promise<void> {
    await expect(this.page).toHaveURL(/assetsScan/, { timeout: 15000 });
  }
}""",
                }
            ]
        }

        result = validate_v1_url_semantics_preservation(raw_script=raw_script, llm_result=llm_result)

        self.assertEqual(result["raw_url_check_count"], 0)
        self.assertEqual(len(result["invalid_url_checks"]), 1)
        self.assertIn("toHaveURL", result["manual_review_items"][0])

    def test_validate_url_semantics_allows_recorded_url_wait(self) -> None:
        """当 raw_script 已明确包含 URL 等待时，相同语义的输出不应被误拦截。"""
        raw_script = """test('test', async ({ page }) => {
  await page.goto('https://10.10.10.189/login');
  await page.getByRole('button', { name: '登录' }).click();
  await page.waitForURL(/workbench/);
});"""

        llm_result = {
            "page_updates": [
                {
                    "page_name": "LoginPage",
                    "new_locators": [],
                    "new_actions": [
                        {
                            "name": "expectLoginSuccess",
                            "content": """async expectLoginSuccess(): Promise<void> {
  await this.page.waitForURL(/workbench/);
}""",
                        }
                    ],
                    "extend_actions": [],
                }
            ]
        }

        result = validate_v1_url_semantics_preservation(raw_script=raw_script, llm_result=llm_result)

        self.assertEqual(result["raw_url_check_count"], 1)
        self.assertEqual(result["invalid_url_checks"], [])
        self.assertEqual(result["manual_review_items"], [])

    def test_validate_critical_locator_coverage_flags_missing_complex_entry_locator(self) -> None:
        """复杂入口定位器如果被泛化成索引 helper，应当给出人工审核提示。"""
        raw_script = """test('test', async ({ page }) => {
  await page.getByRole('list').filter({ hasText: '云端资产推荐支持自定义线索进行影子资产的发现，帮助用户快速、精准的获取资产数据，适用于不同场景资产盘点。查看任务' }).getByRole('paragraph').click();
});"""

        llm_result = {
            "spec_file": {
                "path": "tests/asset-discovery/asset-discovery-recommendation-workflow.spec.ts",
                "content": """import { test } from '../../fixtures/auth.fixture';

test.describe('资产探知', () => {
  test('执行云端资产推荐', async ({ assetDiscoveryTaskPage }) => {
    await assetDiscoveryTaskPage.openTaskListByIndex(0);
  });
});""",
            }
        }

        result = validate_v1_critical_locator_coverage(raw_script=raw_script, llm_result=llm_result)

        self.assertEqual(result["critical_raw_locator_count"], 1)
        self.assertEqual(len(result["missing_critical_locators"]), 1)
        self.assertIn("复杂原始定位器", result["manual_review_items"][0])

    def test_validate_critical_locator_coverage_allows_explicit_complex_locator_reuse(self) -> None:
        """当 LLM 输出显式复用复杂定位器时，不应误报为泛化缺失。"""
        raw_script = """test('test', async ({ page }) => {
  await page.getByRole('list').filter({ hasText: '云端资产推荐支持自定义线索进行影子资产的发现，帮助用户快速、精准的获取资产数据，适用于不同场景资产盘点。查看任务' }).getByRole('paragraph').click();
});"""

        llm_result = {
            "page_updates": [
                {
                    "page_name": "AssetDiscoveryTaskPage",
                    "new_locators": [
                        {
                            "name": "cloudRecommendationTaskEntry",
                            "definition": "page.getByRole('list').filter({ hasText: '云端资产推荐支持自定义线索进行影子资产的发现，帮助用户快速、精准的获取资产数据，适用于不同场景资产盘点。查看任务' }).getByRole('paragraph')",
                        }
                    ],
                    "new_actions": [],
                    "extend_actions": [],
                }
            ]
        }

        result = validate_v1_critical_locator_coverage(raw_script=raw_script, llm_result=llm_result)

        self.assertEqual(result["critical_raw_locator_count"], 1)
        self.assertEqual(result["missing_critical_locators"], [])
        self.assertEqual(result["manual_review_items"], [])


if __name__ == "__main__":
    unittest.main()
